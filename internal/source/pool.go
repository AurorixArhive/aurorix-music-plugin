package source

import (
	"fmt"
	"strings"
	"sync"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// Pool 持有所有音源, 并按配好的优先级把它们排成一条降级链。
//
// 优先级能在运行期改(管理台保存即生效), 所以每次取链都现排 —— 这点开销可以忽略,
// 换来的是不用重启就能调「先试谁」。音源实例本身不会重建, 各自的限速与熔断状态照旧。
type Pool struct {
	mu     sync.RWMutex
	byName map[song.Source]Source
	order  []Source
}

// NewPool 按传入顺序作为初始优先级注册音源。
func NewPool(sources ...Source) *Pool {
	pool := &Pool{byName: make(map[song.Source]Source, len(sources))}
	for _, src := range sources {
		pool.byName[src.Name()] = src
	}
	pool.order = append([]Source(nil), sources...)
	return pool
}

func (p *Pool) Search(keyword string, limit int) ([]song.Song, error) {
	return p.chain().Search(keyword, limit)
}

func (p *Pool) SearchOn(only song.Source, keyword string, limit int) ([]song.Song, error) {
	return p.chain().SearchOn(only, keyword, limit)
}

// Detail 按卡片上的 (音源, ID) 换一条当前可用的直链。
func (p *Pool) Detail(pick song.Song) (song.Song, error) {
	return p.chain().Detail(pick)
}

// SetOrder 按给定名字重排优先级: 先按 names 的顺序排, 没提到的音源按原相对顺序接在后面。
// 名字认 song 包那份短名表(见 song.ByName), 这样 -s 和这里认的是同一套名字。
func (p *Pool) SetOrder(names []string) error {
	head := make([]Source, 0, len(names))
	placed := make(map[song.Source]bool, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		src, ok := song.ByName(name)
		if !ok {
			return fmt.Errorf("不认识的音源 %q(可用: %s)", name, strings.Join(song.Names(), " / "))
		}
		registered, ok := p.byName[src]
		if !ok || placed[src] {
			continue
		}
		placed[src] = true
		head = append(head, registered)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	rest := make([]Source, 0, len(p.order))
	for _, src := range p.order {
		if !placed[src.Name()] {
			rest = append(rest, src)
		}
	}
	p.order = append(head, rest...)
	return nil
}

func (p *Pool) chain() Chain {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append(Chain(nil), p.order...)
}
