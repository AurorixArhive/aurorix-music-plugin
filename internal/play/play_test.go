package play

import (
	"errors"
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/schedule"
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
		{"https://x/y.mflac?a=1", "晴天 - 周杰伦.mflac"}, // 别谎报成 .mp3: 用户打开只会说"文件损坏"
		{"https://x/y", "晴天 - 周杰伦.mp3"},             // 真没后缀才兜底
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

// 上游报错里带着直链、接口名和 traceId —— 群里只该看到这一句。
func TestFailReasonIsOnePlainSentence(t *testing.T) {
	const want = "获取歌曲失败，换一首或换个关键词重试"
	leaky := []error{
		errors.New("海棠湾直链不可播: https://car-er.kuwo.cn/d7908e81/6acd08c0/resource/30106/trackmedia/Q0M00044t2fe2.mflac"),
		errors.New(`云汐直链探测失败: Get "http://ws.stream.qqmusic.qq.com/M800003Qui1q2u1Zho.mp3?vkey=ABC": context deadline exceeded`),
		errors.New("酷狗没给直链: hash=48C685F679FFC7CF08B8A8341CA9DB44 errcode=30020; 海棠湾那边: 500"),
		errors.New("上游回了 502"),
		schedule.ErrAllChannels,
	}
	for _, err := range leaky {
		if got := failReason(err); got != want {
			t.Errorf("failReason(%v) = %q, want %q", err, got, want)
		}
	}
}

// 点歌才是正名, play 只是别名。
func TestCommandNameAndAliases(t *testing.T) {
	cmd := New(nil).Command()
	if cmd.Prefix != "点歌" {
		t.Errorf("主命令名 = %q, want 点歌", cmd.Prefix)
	}
	hasPlay := false
	for _, alias := range cmd.Aliases {
		if alias == "play" {
			hasPlay = true
		}
	}
	if !hasPlay {
		t.Errorf("play 该是别名之一, 实际 %v", cmd.Aliases)
	}
}
