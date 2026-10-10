package guard

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock 让限速/熔断的测试不睡真实时间, 结果才是确定的。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock { return &fakeClock{now: time.Unix(1700000000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestLimiterAllowsBurstThenQueues(t *testing.T) {
	clock := newClock()
	l := NewLimiter(2, 3)
	l.now = clock.Now

	for i := 1; i <= 3; i++ {
		if wait := l.Take(); wait != 0 {
			t.Fatalf("第 %d 个令牌在突发额度内, 不该排队 (要等 %v)", i, wait)
		}
	}
	if wait := l.Take(); wait <= 0 {
		t.Fatal("突发额度用完后必须开始排队")
	}
}

func TestLimiterRefillsByRate(t *testing.T) {
	clock := newClock()
	l := NewLimiter(2, 1) // 每秒补 2 个
	l.now = clock.Now

	l.Take()
	if wait := l.Take(); wait <= 0 {
		t.Fatal("桶空了应该排队")
	}

	clock.Advance(500 * time.Millisecond) // 半秒正好补一个
	if wait := l.Take(); wait != 0 {
		t.Fatalf("半秒后该补回一个令牌, 却要等 %v", wait)
	}
}

func TestLimiterNeverBanksBeyondBurst(t *testing.T) {
	clock := newClock()
	l := NewLimiter(100, 2)
	l.now = clock.Now

	clock.Advance(time.Hour) // 攒很久也不该超过 burst
	l.Take()
	l.Take()
	if wait := l.Take(); wait <= 0 {
		t.Fatal("攒的令牌不能超过 burst, 否则限速形同虚设")
	}
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	clock := newClock()
	b := NewBreaker(3, time.Minute)
	b.now = clock.Now

	for i := 1; i <= 3; i++ {
		if !b.Allow() {
			t.Fatalf("第 %d 次失败前不该被拦", i)
		}
		b.Done(false)
	}
	if b.Allow() {
		t.Fatal("连续 3 次失败后必须断开")
	}
}

func TestBreakerSuccessClearsFailureStreak(t *testing.T) {
	b := NewBreaker(3, time.Minute)

	for i := 0; i < 2; i++ {
		b.Allow()
		b.Done(false)
	}
	b.Allow()
	b.Done(true) // 中间成功一次就该清零
	for i := 0; i < 2; i++ {
		b.Allow()
		b.Done(false)
	}
	if !b.Allow() {
		t.Fatal("失败必须连续才算数, 中间成功过就不该断开")
	}
}

func TestBreakerHalfOpenLetsExactlyOneProbe(t *testing.T) {
	clock := newClock()
	b := NewBreaker(1, time.Minute)
	b.now = clock.Now

	b.Allow()
	b.Done(false) // 断开

	if b.Allow() {
		t.Fatal("冷却期内不该放请求")
	}

	clock.Advance(time.Minute)
	if !b.Allow() {
		t.Fatal("冷却结束后该放一个探测请求")
	}
	if b.Allow() {
		t.Fatal("探测还在飞的时候不能再放, 否则又变成打上游")
	}

	b.Done(true)
	if !b.Allow() {
		t.Fatal("探测成功后应该闭合, 恢复正常放行")
	}
}

func TestBreakerFailedProbeReopens(t *testing.T) {
	clock := newClock()
	b := NewBreaker(1, time.Minute)
	b.now = clock.Now

	b.Allow()
	b.Done(false)

	clock.Advance(time.Minute)
	b.Allow()
	b.Done(false) // 探路也失败

	if b.Allow() {
		t.Fatal("探测失败要重新断开")
	}
	clock.Advance(time.Minute)
	if !b.Allow() {
		t.Fatal("再等一个冷却后还要给机会")
	}
}

func TestGateDeniesWhileBreakerOpen(t *testing.T) {
	g := NewGate(Options{Name: "测试音源", RPS: 100, Burst: 100,
		WaitBudget: time.Second, Failures: 2, Cooldown: time.Minute})

	for i := 0; i < 2; i++ {
		if err := g.Enter(); err != nil {
			t.Fatalf("第 %d 次不该被拦: %v", i+1, err)
		}
		g.Done(false)
	}

	err := g.Enter()
	if err == nil {
		t.Fatal("熔断打开后 Enter 必须报错, 让调用方换源")
	}
	if got := err.Error(); !strings.Contains(got, "测试音源") {
		t.Fatalf("报错该带上音源名, got %q", got)
	}
}

func TestGateStopsCountingNormalMissesAsFailures(t *testing.T) {
	g := NewGate(Options{Name: "测试音源", RPS: 100, Burst: 100,
		WaitBudget: time.Second, Failures: 2, Cooldown: time.Minute})

	// "上游正常回答但没这首歌"不是故障, 连着来多少次都不能断开。
	for i := 0; i < 10; i++ {
		if err := g.Enter(); err != nil {
			t.Fatalf("正常空结果不该触发熔断: %v", err)
		}
		g.Done(true)
	}
}

func TestFlightSharesResultWhileInFlight(t *testing.T) {
	f := NewFlight[string]()
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})

	leader := make(chan string, 1)
	go func() {
		value, _ := f.Do("q:001", func() (string, error) {
			calls.Add(1)
			close(started)
			<-release
			return "link", nil
		})
		leader <- value
	}()

	<-started // fn 已经在飞

	waiter := make(chan string, 1)
	go func() {
		value, _ := f.Do("q:001", func() (string, error) {
			calls.Add(1)
			return "不该走到这里", nil
		})
		waiter <- value
	}()

	// 并发测试里给后来者一点时间挂到同一个 key 上; 这里等的是调度, 不是业务逻辑。
	time.Sleep(30 * time.Millisecond)
	close(release)

	if got := <-leader; got != "link" {
		t.Fatalf("发起方拿到 %q", got)
	}
	if got := <-waiter; got != "link" {
		t.Fatalf("等待方该共享同一个结果, got %q", got)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("同一首歌被并发点开, 上游只该被请求 1 次, 实际 %d 次", n)
	}
}

func TestFlightSeparatesDifferentKeys(t *testing.T) {
	f := NewFlight[string]()
	var calls atomic.Int64

	for _, key := range []string{"q:001", "q:002", "k:003"} {
		value, err := f.Do(key, func() (string, error) {
			calls.Add(1)
			return "link-" + key, nil
		})
		if err != nil || value != "link-"+key {
			t.Fatalf("key %s 拿到 %q / %v", key, value, err)
		}
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("不同 key 不该合并, 期望 3 次调用, 实际 %d", n)
	}
}

func TestFlightForgetsCallAfterItFinishes(t *testing.T) {
	f := NewFlight[string]()
	var calls atomic.Int64

	for i := 0; i < 3; i++ {
		if _, err := f.Do("q:001", func() (string, error) {
			calls.Add(1)
			return "link", nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("前一次结束后必须重新请求, 期望 3 次, 实际 %d", n)
	}
}

func TestFlightPropagatesError(t *testing.T) {
	f := NewFlight[string]()
	want := errors.New("上游挂了")

	_, err := f.Do("q:001", func() (string, error) { return "", want })
	if !errors.Is(err, want) {
		t.Fatalf("错误要原样透出, got %v", err)
	}
}

// 冻结时钟下并发抢令牌: 能立刻通过的个数必须正好等于 burst,
// 也就是"最多只有 burst 个请求同时压到上游"。
func TestLimiterCapsImmediatePassesUnderConcurrency(t *testing.T) {
	const (
		burst  = 5
		caller = 100
	)
	clock := newClock()
	l := NewLimiter(1, burst)
	l.now = clock.Now

	var immediate atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < caller; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Take() == 0 {
				immediate.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := immediate.Load(); got != burst {
		t.Fatalf("%d 个并发里立刻拿到令牌的应该是 %d 个, 实际 %d —— 限速没兜住就会同时打上游",
			caller, burst, got)
	}
}

func TestLimiterWaitGivesUpWhenQueueIsTooLong(t *testing.T) {
	clock := newClock()
	l := NewLimiter(1, 1) // 每秒 1 个, 桶里只有 1 个
	l.now = clock.Now

	l.Take()
	if l.Wait(10 * time.Millisecond) {
		t.Fatal("桶空且要等 1 秒, 只给 10ms 预算时必须放弃, 不能把用户挂在那里")
	}
}
