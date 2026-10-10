package source

import (
	"strings"
	"testing"

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

func (s poolStub) Audio(song.Song) (string, error) { return "", ErrNoResult }

func newPoolWithProbe() (*Pool, *[]song.Source) {
	seen := new([]song.Source)
	mk := func(name song.Source) Source { return poolStub{name: name, seen: seen} }
	return NewPool(mk(song.QQ), mk(song.Kuwo), mk(song.Netease)), seen
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
	if got := visited(t, pool, seen); got != "qq,kuwo,netease" {
		t.Fatalf("默认顺序 = %q", got)
	}
}

func TestPoolSetOrder(t *testing.T) {
	pool, seen := newPoolWithProbe()
	if err := pool.SetOrder([]string{"kw", "wyy", "qq"}); err != nil {
		t.Fatal(err)
	}
	if got := visited(t, pool, seen); got != "kuwo,netease,qq" {
		t.Fatalf("重排后 = %q", got)
	}
}

func TestPoolSetOrderPutsUnlistedAtTheEnd(t *testing.T) {
	pool, seen := newPoolWithProbe()
	if err := pool.SetOrder([]string{"qq"}); err != nil {
		t.Fatal(err)
	}
	// 只点了 qq: 它排最前, 其余按原相对顺序跟后面。
	if got := visited(t, pool, seen); got != "qq,kuwo,netease" {
		t.Fatalf("部分重排 = %q", got)
	}
}

func TestPoolSetOrderRejectsUnknownSource(t *testing.T) {
	pool, _ := newPoolWithProbe()
	if err := pool.SetOrder([]string{"wyy", "nope"}); err == nil {
		t.Fatal("不认识的音源名应报错")
	}
}
