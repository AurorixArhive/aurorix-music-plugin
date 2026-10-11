package schedule

import (
	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	_ "github.com/AurorixArhive/aurorix-music-plugin/internal/channel/all"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
	"testing"
)

// 不带 -s 时先跑哪家由配置的优先级说了算, 不能被分数顶掉 ——
// 之前是所有渠道一起按分数排, 结果配了 qq 优先还是先跑酷我。
func TestSearchOrderFollowsConfigNotScore(t *testing.T) {
	s := New()
	s.SetPlatformOrder([]song.Platform{song.QQ, song.Kugou, song.Kuwo})

	// 给排在最后的酷我刷一个很高的分, 顺序仍然应该是配置的顺序。
	best := s.statFor("kuwo.xinghai")
	best.mu.Lock()
	best.ok, best.latency = 999, 1
	best.mu.Unlock()

	got := s.candidatesForSearch("")
	if !groupedByPlatform(got, song.QQ, song.Kugou, song.Kuwo) {
		t.Fatalf("候选没按配置的音源顺序: %v", ids(got))
	}

	// 换个优先级: 酷我提到最前面, 它就该第一个被发出去。
	s.SetPlatformOrder([]song.Platform{song.Kuwo, song.QQ, song.Kugou})
	got = s.candidatesForSearch("")
	if !groupedByPlatform(got, song.Kuwo, song.QQ, song.Kugou) {
		t.Fatalf("改配置后候选没按音源顺序: %v", ids(got))
	}
}

// 单独指定 -s 时只该有那一家的渠道。
func TestSearchForOnePlatform(t *testing.T) {
	s := New()
	got := ids(s.candidatesForSearch(song.Kuwo))
	if !same(got, []string{"kuwo.xinghai"}) {
		t.Fatalf("指定音源的候选 = %v", got)
	}
}

func ids(list []channel.Channel) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.ID)
	}
	return out
}

func same(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// groupedByPlatform 检查同一个音源的渠道是挨着的, 且音源按 want 的顺序出现。
// 渠道有几个不该影响这条断言, 以后加源不用回来改这里。
func groupedByPlatform(list []channel.Channel, want ...song.Platform) bool {
	seen := map[song.Platform]bool{}
	at := 0
	for i, c := range list {
		if seen[c.Platform] {
			if list[i-1].Platform != c.Platform {
				return false
			}
			continue
		}
		seen[c.Platform] = true
		if at >= len(want) || want[at] != c.Platform {
			return false
		}
		at++
	}
	return at == len(want)
}
