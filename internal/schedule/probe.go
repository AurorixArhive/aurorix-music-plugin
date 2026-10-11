package schedule

import (
	"context"
	"math/rand"
	"time"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const (
	probePeriod  = 30 * time.Minute
	probeGap     = 400 * time.Millisecond
	probeKeyword = "晴天"
	probeBudget  = 25 * time.Second
)

// StartProbe 后台低频体检: 每条渠道搜一次固定关键词, 再拿结果去解析直链。
// 走的是真实调用路径, 所以分数是拿真金白银测出来的 —— 死掉的渠道会自己掉到队尾。
// 间隔带抖动, 免得所有渠道每小时同一秒钟一起醒。
func (s *Scheduler) StartProbe(ctx context.Context) {
	go func() {
		timer := time.NewTimer(jitter(probePeriod))
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.ProbeOnce(ctx)
				timer.Reset(jitter(probePeriod))
			}
		}
	}()
}

func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int63n(int64(d)))
}

// ProbeOnce 立刻体检一轮。它只记分, 不返回错误 —— 体检失败本来就是常态。
func (s *Scheduler) ProbeOnce(ctx context.Context) {
	for _, c := range channel.All() {
		if ctx.Err() != nil {
			return
		}
		s.probeOne(ctx, c)
		select {
		case <-ctx.Done():
			return
		case <-time.After(probeGap):
		}
	}
}

func (s *Scheduler) probeOne(ctx context.Context, c channel.Channel) {
	// 每条渠道单独限时: 一条卡住的渠道不能把整轮体检拖没, 否则排在后面的永远轮不到。
	ctx, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()

	st := s.statFor(c.ID)
	if err := st.enter(); err != nil {
		st.mu.Lock()
		st.lastErr = "闸门拦下: " + err.Error()
		st.mu.Unlock()
		return
	}

	start := time.Now()
	if c.Search != nil {
		found, err := c.Search(ctx, probeKeyword, 3)
		if err != nil {
			st.done(time.Since(start), err)
			return
		}
		if c.Resolve == nil {
			st.done(time.Since(start), nil)
			return
		}
		last := errEmpty
		for _, pick := range found {
			pick.Platform = c.Platform
			_, err := c.Resolve(ctx, pick)
			if err == nil {
				st.done(time.Since(start), nil)
			}
			// 搜到的都不行再试一遍探针曲: QQ 那种搜出来一堆会员曲, 不能因此把渠道判死。
			for _, pick := range canaries(c.Platform) {
				_, err := c.Resolve(ctx, pick)
				if err == nil {
					st.done(time.Since(start), nil)
					return
				}
				last = err
			}
			st.done(time.Since(start), last)
			return
		}
		st.done(time.Since(start), last)
		return
	}

	if c.Resolve == nil {
		return
	}
	last := errEmpty
	for _, pick := range canaries(c.Platform) {
		_, err := c.Resolve(ctx, pick)
		if err == nil {
			st.done(time.Since(start), nil)
			return
		}
		last = err
	}
	st.done(time.Since(start), last)
}

// canaries 是只支持解析直链的渠道的探针曲: 每个平台几首长期在架的免费曲, 挨个试到通为止 ——
// 单挑一首很容易正好撞上付费曲, 把活渠道判成死的。
func canaries(platform song.Platform) []song.Song {
	ids := map[song.Platform][]string{
		song.Kuwo:  {"277246372", "4406153", "51685512"},
		song.Wyy:   {"347230", "1330348068", "186016"},
		song.Kugou: {"48C685F679FFC7CF08B8A8341CA9DB44|79360569", "28F83BBD8CAB043895039E98366AD2B4|79360569"},
		song.QQ:    {"001C38wg2rh0pc", "002AdZTO4TdIHS", "001Qu4I30eVFYb"},
	}
	out := make([]song.Song, 0, 3)
	for _, id := range ids[platform] {
		out = append(out, song.Song{Platform: platform, ID: id})
	}
	return out
}
