package play

import (
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 群里的文件名叫什么, 全看这里 —— 之前直接拿整条 URL 当名字, 于是 query 也进了文件名,
// 群里收到的成了 "C4000....m4a_guid=...&vkey=...", 客户端认不出这是音频。
func TestFileNameKeepsOnlyTheExtensionFromTheURL(t *testing.T) {
	cases := []struct {
		audio string
		want  string
	}{
		{"http://isure6.stream.qqmusic.qq.com/C4000030AING1mSoiB.m4a?guid=1452621576&vkey=31DF&uin=&redirect=1", "晴天 - 周杰伦.m4a"},
		{"https://x/y.mp3?a=1&b=2", "晴天 - 周杰伦.mp3"},
		{"https://x/y.flac", "晴天 - 周杰伦.flac"},
		{"https://x/y.aac", "晴天 - 周杰伦.aac"},
		{"https://x/y.mflac?a=1", "晴天 - 周杰伦.mp3"},
		{"https://x/y", "晴天 - 周杰伦.mp3"},
	}
	for _, c := range cases {
		got := fileName(song.Song{Title: "晴天", Artist: "周杰伦", Audio: c.audio})
		if got != c.want {
			t.Errorf("fileName(%q) = %q, want %q", c.audio, got, c.want)
		}
		for _, leak := range []string{"guid=", "vkey=", "http"} {
			if strings.Contains(got, leak) {
				t.Errorf("文件名里漏了 URL 片段 %q: %q", leak, got)
			}
		}
	}
}

func TestFileNameSanitizesTitle(t *testing.T) {
	got := fileName(song.Song{Title: "A/B:C*?D", Artist: "E", Audio: "https://x/y.mp3"})
	if got != "A_B_C__D - E.mp3" {
		t.Errorf("非法字符没换成下划线: %q", got)
	}
	got = fileName(song.Song{Audio: "https://x/y.mp3"})
	if got != "music.mp3" {
		t.Errorf("没歌名时该给个兜底: %q", got)
	}
}
