// Package guard 给外部音源装两道闸: 限速与熔断。
// 目的是自保 —— 用户多、点得快的时候, 不能把第三方接口打爆。
package guard

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Limiter 令牌桶。Take 只回答"现在能不能拿、拿不到还要等多久",
// 真正的等待交给调用方 —— 时钟可注入, 测试不用睡真实时间。
type Limiter struct {
	mu     sync.Mutex
	rate   float64 // 每秒补几个令牌
	burst  float64 // 最多攒几个
	tokens float64
	last   time.Time
	now    func() time.Time
}

func NewLimiter(rate float64, burst int) *Limiter {
	return &Limiter{rate: rate, burst: float64(burst), tokens: float64(burst), now: time.Now}
}

// Take 取一个令牌; 拿到返回 0, 拿不到返回还要等多久。
func (l *Limiter) Take() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if !l.last.IsZero() {
		l.tokens = min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
	}
	l.last = now

	if l.tokens >= 1 {
		l.tokens--
		return 0
	}
	return time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
}

// Wait 最多等 budget; 等到返回 true, 排队太长返回 false 让调用方直接放弃 ——
// 宁可让用户重试, 也不能把请求堆在上游门口。
func (l *Limiter) Wait(budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		wait := l.Take()
		if wait == 0 {
			return true
		}
		if time.Now().Add(wait).After(deadline) {
			return false
		}
		time.Sleep(wait)
	}
}

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// Breaker 连续失败就断开, 断开期间不再打扰上游; 冷却结束后只放一个探测请求探路。
type Breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	failures  int
	state     breakerState
	openUntil time.Time
	probing   bool
	now       func() time.Time
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{threshold: threshold, cooldown: cooldown, now: time.Now}
}

// Allow 这次请求能不能发。
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerClosed:
		return true
	case breakerOpen:
		if b.now().Before(b.openUntil) {
			return false
		}
		b.state = breakerHalfOpen
	}
	// 半开只放一个探测请求, 其余的等结果
	if b.probing {
		return false
	}
	b.probing = true
	return true
}

// Done 回报结果: 成功复位, 失败累计, 够次数就断开。
// Cooling 只读地问一句"现在是不是被熔断挡着": 不占额度, 也不推进半开状态。
// 状态汇报必须走它 —— Allow() 会把半开状态推进一格, 拿它当只读检查会把熔断卡死。
func (b *Breaker) Cooling() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		return b.now().Before(b.openUntil)
	case breakerHalfOpen:
		return b.probing
	}
	return false
}

// Cooling 见 Breaker.Cooling。
func (g *Gate) Cooling() bool { return g.breaker.Cooling() }

func (b *Breaker) Done(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	halfOpen := b.state == breakerHalfOpen
	b.probing = false

	if ok {
		b.failures = 0
		b.state = breakerClosed
		return
	}
	if halfOpen {
		// 探路也失败: 重新断开, 再等一个冷却
		b.state = breakerOpen
		b.openUntil = b.now().Add(b.cooldown)
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.failures = 0
		b.state = breakerOpen
		b.openUntil = b.now().Add(b.cooldown)
	}
}

// Options 一个音源的闸门参数。
type Options struct {
	Name       string        // 出错提示里的名字
	RPS        float64       // 稳态每秒请求数
	Burst      int           // 允许的瞬时突发
	WaitBudget time.Duration // 排队超过这么久就放弃
	Failures   int           // 连续失败多少次断开
	Cooldown   time.Duration // 断开后冷却多久
}

// Gate 一个上游的两道闸: 先看熔断, 再排队等令牌; 请求结束后回报结果。
type Gate struct {
	name    string
	limiter *Limiter
	breaker *Breaker
	budget  time.Duration
}

func NewGate(opt Options) *Gate {
	return &Gate{
		name:    opt.Name,
		limiter: NewLimiter(opt.RPS, opt.Burst),
		breaker: NewBreaker(opt.Failures, opt.Cooldown),
		budget:  opt.WaitBudget,
	}
}

// ErrGate 表示"不是上游说没有, 而是我们主动把它拦下了"。
// 调用方靠它区分"音源抽风"和"这首歌本来就取不到", 两者该给用户看的话不一样。
var ErrGate = errors.New("被本地闸门拦下")

// Enter 拿到通行证才能发请求; 返回错误说明这次别发了。
func (g *Gate) Enter() error {
	if !g.breaker.Allow() {
		return fmt.Errorf("%s 暂时不可用: %w", g.name, ErrGate)
	}
	if !g.limiter.Wait(g.budget) {
		return fmt.Errorf("%s 请求太密集: %w", g.name, ErrGate)
	}
	return nil
}

// Done 回报这次请求的结果; ok 为 false 才计入熔断。
func (g *Gate) Done(ok bool) { g.breaker.Done(ok) }
