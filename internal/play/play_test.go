package play

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/api"
	"github.com/KasumiYuku/Aurorix/lib/buttons"
	"github.com/KasumiYuku/Aurorix/lib/constant"
	botcontext "github.com/KasumiYuku/Aurorix/lib/context"
	"github.com/KasumiYuku/Aurorix/lib/requests"
	"github.com/KasumiYuku/Aurorix/lib/templates"

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

// 一个参数都凑不出来的卡片, 在客户端就是个空壳 —— 那种宁可不发。
func TestCardWorthSending(t *testing.T) {
	cases := []struct {
		name string
		in   song.Song
		want bool
	}{
		{"有标题", song.Song{Title: "花海"}, true},
		{"只有封面", song.Song{Cover: "https://x/y.jpg"}, true},
		{"只有链接和直链", song.Song{ID: "x", Audio: "https://x/y.mp3"}, false},
		{"标题是空白", song.Song{Title: "  "}, false},
		{"什么都没有", song.Song{}, false},
	}
	for _, c := range cases {
		if got := cardWorthSending(c.in); got != c.want {
			t.Errorf("%s: cardWorthSending = %v, want %v", c.name, got, c.want)
		}
	}
}

// 卡片这条消息不要框架级底部按钮 —— 一条卡底下再压一行按钮, 等于把它切成两块。
// 对照: 同一条上下文里发普通 markdown, 底部按钮必须还在, 否则这个断言什么都证明不了。
func TestCardSendsWithoutFooter(t *testing.T) {
	buttons.EnableFooter(func() []string { return []string{"+1 | counter | 好的"} })
	t.Cleanup(buttons.DisableFooter)

	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":"7200"}`))
			return
		}
		data, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(data))
		_, _ = w.Write([]byte(`{"id":"msg-1","timestamp":"1700000000"}`))
	}))
	defer srv.Close()

	bot := api.Init("app", "secret", srv.URL, requests.Init(5))
	bot.Tokens.TokenAPI = srv.URL + "/token"

	// 模板注册到全局命名空间: 这条只验 NoFooter, 不依赖插件 templates/markdown 里那个文件。
	templates.NewMarkdownTemplate(templateSongCard, `{{qqcard 2 leftImageUrl=cover scheme=link text1=title text2=artist}}`)

	mgr := &botcontext.MessageManager{
		MessageId: "m1", EventId: "e1", GroupId: "g1", UserId: "u1",
		Target: constant.GroupMessage, Qapi: bot, PluginId: "music",
	}
	ctx := &botcontext.MessageContext{Context: &botcontext.Context{MessageManager: mgr}}

	card := song.Song{Platform: song.QQ, ID: "x", Title: "花海", Artist: "周杰伦", Cover: "https://y.gtimg.cn/c.jpg"}
	if err := (&Play{}).sendCard(ctx, card); err != nil {
		t.Fatalf("发卡失败: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("应只发一条, 实发 %d", len(bodies))
	}
	body := bodies[0]
	if strings.Contains(body, "aurorix.footer.") {
		t.Errorf("卡片带了底部按钮: %s", body)
	}
	for _, want := range []string{"text1=花海", "text2=周杰伦", "leftImageUrl="} {
		if !strings.Contains(body, want) {
			t.Errorf("卡片少了 %q: %s", want, body)
		}
	}

	// 对照组: 同一条上下文里的普通 markdown 照旧带底部按钮。
	if err := mgr.Markdown("普通消息").Send(); err != nil {
		t.Fatalf("发对照消息失败: %v", err)
	}
	plain := bodies[len(bodies)-1]
	if !strings.Contains(plain, "aurorix.footer.") {
		t.Fatalf("对照组都没挂上底部按钮, 这一轮断言不能说明问题: %s", plain)
	}
}
