package source

import (
	"errors"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

type stub struct {
	name      song.Source
	songs     []song.Song
	detail    song.Song
	err       error
	detailErr error
	called    bool
}

func (s *stub) Name() song.Source { return s.name }

func (s *stub) Search(string, int) ([]song.Song, error) {
	s.called = true
	return s.songs, s.err
}

func (s *stub) Detail(string) (song.Song, error) {
	s.called = true
	if s.detailErr != nil {
		return song.Song{}, s.detailErr
	}
	if s.detail.Audio == "" && s.detail.ID == "" {
		return song.Song{}, ErrNoResult
	}
	return s.detail, nil
}

func TestChainSearchFallsThroughOnError(t *testing.T) {
	boom := errors.New("boom")
	first := &stub{name: song.QQ, err: boom}
	second := &stub{name: song.Wyy, songs: []song.Song{{Source: song.Wyy, ID: "1"}}}

	got, err := Chain{first, second}.Search("花海", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("got = %+v", got)
	}
	if !first.called || !second.called {
		t.Fatalf("两个源都应被调用: first=%v second=%v", first.called, second.called)
	}
}

func TestChainSearchStopsAtFirstHit(t *testing.T) {
	first := &stub{name: song.QQ, songs: []song.Song{{Source: song.QQ, ID: "1"}}}
	second := &stub{name: song.Wyy, songs: []song.Song{{Source: song.Wyy, ID: "2"}}}

	got, err := Chain{first, second}.Search("花海", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0].Source != song.QQ {
		t.Fatalf("应命中第一个源, got = %+v", got)
	}
	if second.called {
		t.Fatal("命中后不应再调用后面的源")
	}
}

func TestChainSearchAllFailKeepsLastError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Chain{
		&stub{name: song.QQ, err: boom},
		&stub{name: song.Wyy},
	}.Search("花海", 10)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; 期望保留最后一个真实错误", err)
	}
}

func TestChainSearchNoSource(t *testing.T) {
	if _, err := (Chain{}).Search("花海", 10); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
}

// 短码里带音源, 所以取直链时只该问那一家 —— 别家连试都不该试。
func TestChainDetailRoutesBySource(t *testing.T) {
	chain := Chain{
		&stub{name: song.QQ, detail: song.Song{Source: song.QQ, ID: "1", Audio: "qq-link"}},
		&stub{name: song.Wyy, detail: song.Song{Source: song.Wyy, ID: "1", Audio: "wyy-link"}},
	}

	got, err := chain.Detail(song.Song{Source: song.Wyy, ID: "1"})
	if err != nil || got.Audio != "wyy-link" {
		t.Fatalf("got = %+v, err = %v", got, err)
	}

	if _, err := chain.Detail(song.Song{Source: "unknown", ID: "1"}); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
}

// -s 指定了音源就只问那一家的: 别家连试都不该试。
func TestSearchOnAsksOnlyTheNamedSource(t *testing.T) {
	qq := &stub{name: song.QQ, songs: []song.Song{{Source: song.QQ, ID: "1"}}}
	wyy := &stub{name: song.Wyy, songs: []song.Song{{Source: song.Wyy, ID: "2"}}}

	got, err := Chain{qq, wyy}.SearchOn(song.Wyy, "花海", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].Source != song.Wyy {
		t.Fatalf("got = %+v", got)
	}
	if qq.called {
		t.Error("指定了网易就不该再去问 QQ")
	}
	if !wyy.called {
		t.Error("指定的那家必须问到")
	}
}

// 指定的那家没有, 就是没有 —— 不悄悄换别家, 也不报成"上游故障",
// 好让上层能说清是哪一家没有。
func TestSearchOnReportsNoResultAsEmpty(t *testing.T) {
	wyy := &stub{name: song.Wyy}
	qq := &stub{name: song.QQ, songs: []song.Song{{Source: song.QQ, ID: "1"}}}

	got, err := Chain{wyy, qq}.SearchOn(song.Wyy, "花海", 10)
	if err != nil {
		t.Fatalf("没搜到不该报错, err = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v", got)
	}
	if qq.called {
		t.Error("指定了网易就不该再去问 QQ")
	}
}

// "上游正常回答但没这首歌"不是故障: 付费曲、搜不到都不该把音源断掉。
func TestNormalTreatsEmptyResultAsHealthy(t *testing.T) {
	if normal(nil) != nil {
		t.Fatal("成功就是成功")
	}
	if normal(ErrNoResult) != nil {
		t.Fatal("没结果不等于故障, 算成故障会让音源被自己断掉")
	}
	if normal(errors.New("上游 500")) == nil {
		t.Fatal("真故障必须记一笔, 否则熔断永远不会触发")
	}
}
