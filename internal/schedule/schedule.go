package schedule

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const (
	hedgeDelay  = 800 * time.Millisecond
	latencyHalf = 600.0
)

var (
	ErrAllChannels = errors.New("这个音源的渠道现在都不通")
	errEmpty       = errors.New("这条渠道没给出结果")
)

type stat struct {
	mu      sync.Mutex
	ok      int64
	fail    int64
	latency float64
	lastErr string
	gate    *guard.Gate
}

func newStat(name string) *stat {
	return &stat{gate: guard.NewGate(guard.Options{
		Name: name, RPS: 6, Burst: 12, WaitBudget: 4 * time.Second,
		Failures: 3, Cooldown: 45 * time.Second,
	})}
}

func (s *stat) enter() error { return s.gate.Enter() }

func (s *stat) done(d time.Duration, err error) {
	s.gate.Done(err == nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.fail++
		s.lastErr = err.Error()
		return
	}
	s.ok++
	ms := float64(d.Milliseconds())
	if s.latency == 0 {
		s.latency = ms
		return
	}
	s.latency = s.latency*0.7 + ms*0.3
}

// score 越大越优先: 成功率占七成, 延迟占三成。没跑过的给个中间值, 让它有机会被试一次。
func (s *stat) score() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := s.ok + s.fail
	if total == 0 {
		return 1
	}
	speed := 0.0
	if s.latency > 0 {
		speed = 1 / (1 + s.latency/latencyHalf)
	}
	return float64(s.ok)/float64(total)*2 + speed
}

type ChannelStatus struct {
	ID        string
	Platform  song.Platform
	Label     string
	OK        int64
	Fail      int64
	LatencyMS int
	LastErr   string
	Cooling   bool
}

type Scheduler struct {
	hedge       time.Duration
	mu          sync.Mutex
	stats       map[string]*stat
	order       []song.Platform
	search      *guard.Flight[[]song.Song]
	resolve     *guard.Flight[song.Song]
	probeOnce   sync.Once
	probeCancel context.CancelFunc
}

func New() *Scheduler {
	return &Scheduler{
		hedge:   hedgeDelay,
		stats:   map[string]*stat{},
		search:  guard.NewFlight[[]song.Song](),
		resolve: guard.NewFlight[song.Song](),
	}
}

func (s *Scheduler) SetPlatformOrder(order []song.Platform) {
	s.mu.Lock()
	s.order = append([]song.Platform(nil), order...)
	s.mu.Unlock()
}

func (s *Scheduler) platformOrder() []song.Platform {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]song.Platform(nil), s.order...)
}

func (s *Scheduler) statFor(id string) *stat {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.stats[id]; ok {
		return st
	}
	st := newStat(id)
	s.stats[id] = st
	return st
}

func (s *Scheduler) ranked(list []channel.Channel) []channel.Channel {
	out := append([]channel.Channel(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		return s.statFor(out[i].ID).score() > s.statFor(out[j].ID).score()
	})
	return out
}

func (s *Scheduler) candidatesForSearch(platform song.Platform) []channel.Channel {
	if platform != "" {
		return s.ranked(channel.Searchable(platform))
	}
	// 音源之间按配置的优先级排, 音源内部才按分数 —— 否则分数会把优先级顶掉。
	out := make([]channel.Channel, 0, 16)
	for _, p := range s.platformOrder() {
		out = append(out, s.ranked(channel.Searchable(p))...)
	}
	return out
}

// Search 不给 -s 时把全部音源的渠道按配置的优先级摊平: 谁先回谁赢, 前面那家没结果才轮到下一家。
func (s *Scheduler) Search(ctx context.Context, platform song.Platform, channelID, keyword string, limit int) ([]song.Song, error) {
	candidates := pickChannels(s.candidatesForSearch(platform), channelID)
	if len(candidates) == 0 {
		return nil, ErrAllChannels
	}
	key := "s|" + string(platform) + "|" + channelID + "|" + keyword
	return s.search.Do(key, func() ([]song.Song, error) {
		return race(ctx, candidates, s.hedge, func(ctx context.Context, c channel.Channel) ([]song.Song, error) {
			got, err := run(s, c, func() ([]song.Song, error) { return c.Search(ctx, keyword, limit) })
			if err == nil {
				for i := range got {
					got[i].Channel = c.ID
				}
			}
			return got, err
		})
	})
}

func (s *Scheduler) Resolve(ctx context.Context, want song.Song, channelID string) (song.Song, error) {
	candidates := pickChannels(channel.Resolvable(want.Platform), channelID)
	if len(candidates) == 0 {
		return song.Song{}, ErrAllChannels
	}
	if channelID == "" && want.Channel != "" {
		candidates = promote(candidates, want.Channel)
	}
	key := "r|" + string(want.Platform) + "|" + channelID + "|" + want.ID
	return s.resolve.Do(key, func() (song.Song, error) {
		return race(ctx, s.ranked(candidates), s.hedge, func(ctx context.Context, c channel.Channel) (song.Song, error) {
			return run(s, c, func() (song.Song, error) { return c.Resolve(ctx, want) })
		})
	})
}

func run[T any](s *Scheduler, c channel.Channel, fn func() (T, error)) (T, error) {
	var zero T
	st := s.statFor(c.ID)
	if err := st.enter(); err != nil {
		return zero, err
	}
	start := time.Now()
	got, err := fn()
	if err == nil && isEmpty(got) {
		err = errEmpty
	}
	st.done(time.Since(start), err)
	return got, err
}

func isEmpty[T any](value T) bool {
	switch v := any(value).(type) {
	case []song.Song:
		return len(v) == 0
	case song.Song:
		return v.Audio == ""
	}
	return false
}

func (s *Scheduler) Channels() []ChannelStatus {
	out := make([]ChannelStatus, 0, 16)
	for _, c := range channel.All() {
		st := s.statFor(c.ID)
		st.mu.Lock()
		out = append(out, ChannelStatus{
			ID: c.ID, Platform: c.Platform, Label: c.Label,
			OK: st.ok, Fail: st.fail, LatencyMS: int(st.latency),
			LastErr: st.lastErr, Cooling: st.gate.Cooling(),
		})
		st.mu.Unlock()
	}
	return out
}

func pickChannels(list []channel.Channel, channelID string) []channel.Channel {
	if channelID == "" {
		return list
	}
	out := make([]channel.Channel, 0, 1)
	for _, c := range list {
		if c.ID == channelID {
			out = append(out, c)
		}
	}
	return out
}

func promote(list []channel.Channel, id string) []channel.Channel {
	out := make([]channel.Channel, 0, len(list))
	for _, c := range list {
		if c.ID == id {
			out = append(out, c)
		}
	}
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}

// race 按顺序发候选人: 前一个报错就立刻换下一个, 前一个只是慢就等到 hedge 再并行开一个。
// 第一个成功的结果胜出, 其余随 ctx 一起取消。
func race[T any](ctx context.Context, list []channel.Channel, hedge time.Duration, run func(context.Context, channel.Channel) (T, error)) (T, error) {
	var zero T
	if len(list) == 0 {
		return zero, ErrAllChannels
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type outcome struct {
		value T
		err   error
	}
	results := make(chan outcome, len(list))
	launched, done := 0, 0
	timer := time.NewTimer(hedge)
	defer timer.Stop()

	launch := func() {
		c := list[launched]
		launched++
		go func() {
			value, err := run(ctx, c)
			select {
			case results <- outcome{value, err}:
			case <-ctx.Done():
			}
		}()
	}
	launch()

	var lastErr error
	// best 收着"发得出去、但不够好"的结果(加密格式这类)。它不打断这一轮 ——
	// 先把别的渠道问完, 有够好的就用够好的, 实在没有才回落到这里最像样的那一份。
	var best T
	hasBest := false
	bestScore := 0
	for {
		select {
		case got := <-results:
			done++
			if got.err == nil {
				score, ranked := usability(got.value)
				// 没有分级概念的结果(搜索返回的列表)一律当作已经够好, 够好的立刻收工。
				if !ranked || score >= int(quality.Usable) {
					return got.value, nil
				}
				// 低于"降级"的等于没有结果 —— 别把一份不可用的东西当兜底发出去。
				if score >= int(quality.Degraded) && (!hasBest || score > bestScore) {
					best, hasBest, bestScore = got.value, true, score
				}
			} else {
				lastErr = got.err
			}
			if launched < len(list) {
				launch()
				timer.Reset(hedge)
				continue
			}
			if done == launched {
				if hasBest {
					return best, nil
				}
				return zero, lastErr
			}
		case <-timer.C:
			if launched < len(list) {
				launch()
				timer.Reset(hedge)
			}
		case <-ctx.Done():
			if hasBest {
				return best, nil
			}
			if lastErr != nil {
				return zero, lastErr
			}
			return zero, ctx.Err()
		}
	}
}

// rankable 让结果自己声明"这份东西有多可用", 调度因此不必认识 song 的字段。
type rankable interface{ Usability() int }

// usability 取结果的可用等级; 没有分级的返回 ok=false, 视为已经够好。
func usability[T any](value T) (int, bool) {
	if r, ok := any(value).(rankable); ok {
		return r.Usability(), true
	}
	return 0, false
}
