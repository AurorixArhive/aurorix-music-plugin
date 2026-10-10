package source

import (
	"strings"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

type poolStub struct {
	name song.Source
	seen *[]song.Source
}

func (s poolStub) Name() song.Source { return s.name }

func (s poolStub) Search(string, int) ([]song.Song, error) {
	*s.seen = append(*s.seen, s.name)
	return nil, nil
}

func (s poolStub) Detail(string) (song.Song, error) { return song.Song{}, ErrNoResult }

func newPoolWithProbe() (*Pool, *[]song.Source) {
	seen := new([]song.Source)
	mk := func(name song.Source) Source { return poolStub{name: name, seen: seen} }
	return NewPool(mk(song.QQ), mk(song.Wyy), mk(song.Baidu), mk(song.Ting1)), seen
}

func visited(t *testing.T, pool *Pool, seen *[]song.Source) string {
	t.Helper()
	*seen = nil
	_, _ = pool.Search("x", 5)
	names := make([]string, 0, len(*seen))
	for _, name := range *seen {
		names = append(names, string(name))
	}
	return strings.Join(names, ",")
}

func TestPoolKeepsRegistrationOrderByDefault(t *testing.T) {
	pool, seen := newPoolWithProbe()
	if got := visited(t, pool, seen); got != "qq,wyy,baidu,1ting" {
		t.Fatalf("默认顺序 = %q", got)
	}
}

func TestPoolSetOrder(t *testing.T) {
	pool, seen := newPoolWithProbe()
	if err := pool.SetOrder([]string{"1ting", "baidu", "qq"}); err != nil {
		t.Fatal(err)
	}
	if got := visited(t, pool, seen); got != "1ting,baidu,qq,wyy" {
		t.Fatalf("重排后 = %q", got)
	}
}

// 全名也能写, 大小写和空格都不计较 —— 配置项是人手打的。
func TestPoolSetOrderAcceptsFullNames(t *testing.T) {
	pool, seen := newPoolWithProbe()
	if err := pool.SetOrder([]string{" netease ", "QQMusic"}); err != nil {
		t.Fatal(err)
	}
	if got := visited(t, pool, seen); got != "wyy,qq,baidu,1ting" {
		t.Fatalf("重排后 = %q", got)
	}
}

func TestPoolSetOrderPutsUnlistedAtTheEnd(t *testing.T) {
	pool, seen := newPoolWithProbe()
	if err := pool.SetOrder([]string{"baidu"}); err != nil {
		t.Fatal(err)
	}
	if got := visited(t, pool, seen); got != "baidu,qq,wyy,1ting" {
		t.Fatalf("部分重排 = %q", got)
	}
}

func TestPoolSetOrderRejectsUnknownSource(t *testing.T) {
	pool, _ := newPoolWithProbe()
	err := pool.SetOrder([]string{"wyy", "kuwo"})
	if err == nil {
		t.Fatal("不认识的音源名应报错")
	}
	// 报错要列出手上认哪些并带上用户写的值。
	if !strings.Contains(err.Error(), "kuwo") || !strings.Contains(err.Error(), "1ting") {
		t.Fatalf("报错内容 = %v", err)
	}
}

// 每家音源各有一套闸门: 聚合站的三家同域名共用一套(否则总量会超限),
// 网易是另一个出口, 必须分开 —— 一个被限住不能连带拖死另一个。
func TestGatesAreSharedPerUpstream(t *testing.T) {
	http := requests.Init(5)
	tx := NewTxQQ(http)
	if len(tx) < 2 {
		t.Fatalf("聚合音源只有 %d 家", len(tx))
	}

	first, ok := tx[0].(*txqq)
	if !ok {
		t.Fatalf("聚合音源类型 = %T", tx[0])
	}
	for _, src := range tx[1:] {
		other, ok := src.(*txqq)
		if !ok {
			t.Fatalf("聚合音源类型 = %T", src)
		}
		if other.gate != first.gate {
			t.Errorf("%s 和 %s 同域名却各拿一套闸门", first.name, other.name)
		}
	}

	wyy, ok := NewNetease(http).(*netease)
	if !ok {
		t.Fatal("网易音源类型不对")
	}
	if wyy.gate == first.gate {
		t.Error("网易和聚合站共用闸门, 一边出问题会连带拖死另一边")
	}
	if wyy.gate == nil || first.gate == nil {
		t.Fatal("音源必须挂闸门, 否则高并发时会把上游打爆")
	}
}

// 默认优先级: 聚合站的 qq 最稳排第一, 网易其次, 四家一个都不能漏。
func TestDefaultsOrder(t *testing.T) {
	got := Defaults(requests.Init(5))
	if len(got) != 4 {
		t.Fatalf("默认音源数 = %d, want 4", len(got))
	}
	want := []song.Source{song.QQ, song.Wyy, song.Baidu, song.Ting1}
	for i, src := range got {
		if src.Name() != want[i] {
			t.Errorf("第 %d 家 = %s, want %s", i+1, src.Name(), want[i])
		}
	}
}
