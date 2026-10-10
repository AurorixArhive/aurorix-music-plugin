package guard

import "sync"

// Flight 把同一个 key 的并发请求合并成一次: 十个人同时点同一首歌,
// 只会向上游取一次直链, 其余的人共享这次结果。
type Flight[T any] struct {
	mu sync.Mutex
	m  map[string]*flightCall[T]
}

type flightCall[T any] struct {
	wg  sync.WaitGroup
	val T
	err error
}

func NewFlight[T any]() *Flight[T] { return &Flight[T]{m: make(map[string]*flightCall[T])} }

// Do 执行 fn; 同一个 key 已经在飞的话等它的结果, 不重复调用。
func (f *Flight[T]) Do(key string, fn func() (T, error)) (T, error) {
	f.mu.Lock()
	if call, ok := f.m[key]; ok {
		f.mu.Unlock()
		call.wg.Wait()
		return call.val, call.err
	}
	call := &flightCall[T]{}
	call.wg.Add(1)
	f.m[key] = call
	f.mu.Unlock()

	call.val, call.err = fn()

	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
	call.wg.Done()

	return call.val, call.err
}
