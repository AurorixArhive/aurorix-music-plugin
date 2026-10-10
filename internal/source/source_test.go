package source

import (
	"errors"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

type stub struct {
	name     song.Source
	songs    []song.Song
	link     string
	err      error
	audioErr error
	called   bool
	picked   song.Song
}

func (s *stub) Name() song.Source { return s.name }

func (s *stub) Search(string, int) ([]song.Song, error) {
	s.called = true
	return s.songs, s.err
}

func (s *stub) Audio(pick song.Song) (string, error) {
	s.picked = pick
	if s.audioErr != nil {
		return "", s.audioErr
	}
	return s.link, s.err
}

func TestChainSearchFallsThroughOnError(t *testing.T) {
	boom := errors.New("boom")
	first := &stub{name: song.Kuwo, err: boom}
	second := &stub{name: song.Netease, songs: []song.Song{{Source: song.Netease, ID: "1"}}}

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

func TestChainSearchFallsThroughOnEmpty(t *testing.T) {
	first := &stub{name: song.Kuwo}
	second := &stub{name: song.Netease, songs: []song.Song{{Source: song.Netease, ID: "2"}}}

	got, err := Chain{first, second}.Search("花海", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("got = %+v", got)
	}
}

func TestChainSearchStopsAtFirstHit(t *testing.T) {
	first := &stub{name: song.Kuwo, songs: []song.Song{{Source: song.Kuwo, ID: "1"}}}
	second := &stub{name: song.Netease, songs: []song.Song{{Source: song.Netease, ID: "2"}}}

	got, err := Chain{first, second}.Search("花海", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0].Source != song.Kuwo {
		t.Fatalf("应命中第一个源, got = %+v", got)
	}
	if second.called {
		t.Fatal("命中后不应再调用后面的源")
	}
}

func TestChainSearchAllFailKeepsLastError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Chain{
		&stub{name: song.Kuwo, err: boom},
		&stub{name: song.Netease},
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

func TestChainAudioRoutesBySource(t *testing.T) {
	chain := Chain{
		&stub{name: song.Kuwo, link: "kuwo-link"},
		&stub{name: song.Netease, link: "netease-link"},
	}

	got, err := chain.Audio(song.Song{Source: song.Netease, ID: "1"})
	if err != nil || got != "netease-link" {
		t.Fatalf("got = %q, err = %v", got, err)
	}

	got, err = chain.Audio(song.Song{Source: song.Kuwo, ID: "1"})
	if err != nil || got != "kuwo-link" {
		t.Fatalf("got = %q, err = %v", got, err)
	}

	if _, err := chain.Audio(song.Song{Source: "unknown", ID: "1"}); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
}

func TestChainAudioFallsBackOnOwnerFailure(t *testing.T) {
	owner := &stub{name: song.QQ, audioErr: ErrNoResult}
	backup := &stub{
		name:  song.Kuwo,
		link:  "kuwo-link",
		songs: []song.Song{{Source: song.Kuwo, ID: "1", Title: "花海", Artist: "周杰伦"}},
	}

	got, err := Chain{owner, backup}.Audio(song.Song{Source: song.QQ, ID: "m", Title: "花海", Artist: "周杰伦"})
	if err != nil || got != "kuwo-link" {
		t.Fatalf("got = %q, err = %v", got, err)
	}
}

func TestChainAudioPrefersSameArtist(t *testing.T) {
	owner := &stub{name: song.QQ, audioErr: ErrNoResult}
	backup := &stub{
		name: song.Kuwo,
		link: "kuwo-link",
		songs: []song.Song{
			{Source: song.Kuwo, ID: "cover", Title: "花海", Artist: "粉白噪音"},
			{Source: song.Kuwo, ID: "origin", Title: "花海", Artist: "周杰伦"},
		},
	}

	if _, err := (Chain{owner, backup}).Audio(song.Song{Source: song.QQ, ID: "m", Title: "花海", Artist: "周杰伦"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if backup.picked.ID != "origin" {
		t.Fatalf("应优先选歌手一致的, picked = %+v", backup.picked)
	}
}

func TestChainAudioTakesTitleOnlyWhenArtistMissing(t *testing.T) {
	owner := &stub{name: song.QQ, audioErr: ErrNoResult}
	backup := &stub{
		name:  song.Kuwo,
		link:  "kuwo-link",
		songs: []song.Song{{Source: song.Kuwo, ID: "cover", Title: "花海", Artist: "粉白噪音"}},
	}

	got, err := (Chain{owner, backup}).Audio(song.Song{Source: song.QQ, ID: "m", Title: "花海", Artist: "查无此人"})
	if err != nil || got != "kuwo-link" {
		t.Fatalf("同名歌手对不上也应给个能播的: got = %q, err = %v", got, err)
	}
}

func TestChainAudioKeepsOwnerErrorWhenNothingMatches(t *testing.T) {
	boom := errors.New("boom")
	owner := &stub{name: song.QQ, audioErr: boom}
	other := &stub{
		name:  song.Kuwo,
		songs: []song.Song{{Source: song.Kuwo, ID: "1", Title: "别的歌", Artist: "周杰伦"}},
	}

	_, err := Chain{owner, other}.Audio(song.Song{Source: song.QQ, ID: "m", Title: "花海", Artist: "周杰伦"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; 期望保留本源错误", err)
	}
}

func TestChainAudioNoTitleSkipsFallback(t *testing.T) {
	owner := &stub{name: song.QQ, audioErr: ErrNoResult}
	other := &stub{name: song.Kuwo, link: "kuwo-link"}

	if _, err := (Chain{owner, other}).Audio(song.Song{Source: song.QQ, ID: "m"}); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
	if other.called {
		t.Fatal("没有标题时不该去别的源搜")
	}
}

func TestNormalizeIgnoresSeparatorsAndCase(t *testing.T) {
	if normalize("A、B") != normalize("A/B") {
		t.Fatal("顿号与斜杠应等价")
	}
	if normalize(" Hello World ") != "helloworld" {
		t.Fatalf("normalize = %q", normalize(" Hello World "))
	}
}

// -s 指定了音源就只问那一家的: 别家连试都不该试。
func TestSearchOnAsksOnlyTheNamedSource(t *testing.T) {
	qq := &stub{name: song.QQ, songs: []song.Song{{Source: song.QQ, ID: "1"}}}
	kuwo := &stub{name: song.Kuwo, songs: []song.Song{{Source: song.Kuwo, ID: "2"}}}

	got, err := Chain{qq, kuwo}.SearchOn(song.Kuwo, "花海", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].Source != song.Kuwo {
		t.Fatalf("got = %+v", got)
	}
	if qq.called {
		t.Error("指定了酷我就不该再去问 QQ")
	}
	if !kuwo.called {
		t.Error("指定的那家必须问到")
	}
}

// 指定的那家没有, 就是没有 —— 不悄悄换别家, 也不报成"上游故障",
// 好让上层能说清是哪一家没有。
func TestSearchOnReportsNoResultAsEmpty(t *testing.T) {
	wyy := &stub{name: song.Netease}
	qq := &stub{name: song.QQ, songs: []song.Song{{Source: song.QQ, ID: "1"}}}

	got, err := Chain{wyy, qq}.SearchOn(song.Netease, "花海", 10)
	if err != nil {
		t.Fatalf("没搜到不该报错, err = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v", got)
	}
	if qq.called {
		t.Error("指定了网易云就不该再去问 QQ")
	}
}

// 不指定音源时行为与原来逐字一致: 走降级链, 命中即停。
func TestSearchOnWithoutSourceFallsBackToChain(t *testing.T) {
	first := &stub{name: song.Kuwo, songs: []song.Song{{Source: song.Kuwo, ID: "1"}}}
	second := &stub{name: song.Netease, songs: []song.Song{{Source: song.Netease, ID: "2"}}}

	got, err := Chain{first, second}.SearchOn("", "花海", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].Source != song.Kuwo {
		t.Fatalf("应命中第一个源, got = %+v", got)
	}
	if second.called {
		t.Error("命中后不应再问后面的源")
	}
}
