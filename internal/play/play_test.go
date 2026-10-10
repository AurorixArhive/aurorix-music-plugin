package play

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	botcontext "github.com/KasumiYuku/Aurorix/lib/context"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/source"
)

// 参数由框架的 kong 解好塞进 ctx.Parsed, 这里只确认取出来的关键词对得上。
// 类型对不上时按"没写参数"处理, 用户看到的是帮助卡片。
func TestParsedKeyword(t *testing.T) {
	cases := []struct {
		name   string
		parsed any
		want   string
	}{
		{"单个词", &playArgs{Words: []string{"花海"}}, "花海"},
		{"多个词", &playArgs{Words: []string{"周杰伦", "花海"}}, "周杰伦 花海"},
		{"无参数", &playArgs{}, ""},
		{"只有音源", &playArgs{Source: "wyy"}, ""},
		{"类型不符", "花海", ""},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := &botcontext.MessageContext{Parsed: c.parsed}
			if got := strings.Join(parsed(ctx).Words, " "); got != c.want {
				t.Fatalf("关键词 = %q, want %q", got, c.want)
			}
		})
	}
}

// -s 只认音源表里那几个短名。写别的必须说清认哪些, 而不是静默当成"不限定"。
func TestPickSource(t *testing.T) {
	known := []struct {
		in   string
		want song.Source
	}{
		{"", ""},
		{"wyy", song.Wyy},
		{"WYY", song.Wyy},
		{" 1ting ", song.Ting1},
		{"qq", song.QQ},
		{"baidu", song.Baidu},
	}
	for _, c := range known {
		got, err := pickSource(c.in)
		if err != nil || got != c.want {
			t.Errorf("pickSource(%q) = (%q, %v), want (%q, nil)", c.in, got, err, c.want)
		}
	}

	for _, bad := range []string{"youtube", "网易", "kuwo", "kw"} {
		got, err := pickSource(bad)
		if err == nil {
			t.Errorf("pickSource(%q) 该报错, got %q", bad, got)
			continue
		}
		// 报错要列出手上认哪些并带上用户写的值。
		if !strings.Contains(err.Error(), "qq") || !strings.Contains(err.Error(), bad) {
			t.Errorf("报错内容 = %v", err)
		}
	}
}

// 卡片上那行命令必须自包含: 发出去多久、服务端重启多少次, 点下去都还能找到同一首歌。
func TestSearchRowsUseSelfContainedRef(t *testing.T) {
	songs := []song.Song{
		{Source: song.QQ, ID: "002hG4kq0WZBma", Title: "花海", Artist: "周杰伦"},
		{Source: song.Wyy, ID: "186016", Title: "晴天"},
	}

	rows := searchRows(songs)
	if len(rows) != 2 {
		t.Fatalf("行数 = %d, want 2", len(rows))
	}

	for i, s := range songs {
		row, ok := rows[i].(map[string]any)
		if !ok {
			t.Fatalf("行类型 = %T", rows[i])
		}
		command, _ := row["command"].(string)
		code, ok := strings.CutPrefix(command, "/play ")
		if !ok {
			t.Fatalf("第 %d 行 command = %q, 该是 /play 加短码", i+1, command)
		}
		pick, ok := song.ParseRef(code)
		if !ok {
			t.Fatalf("第 %d 行的短码解不回来: %q", i+1, code)
		}
		if pick.Source != s.Source || pick.ID != s.ID {
			t.Errorf("第 %d 行解成 %+v, want %+v", i+1, pick, s)
		}
		// 短码不能把音源名或曲目 ID 明文写进去 —— 卡片上的东西用户都看得到。
		for _, leak := range []string{"qq", "wyy", "netease", s.ID, ":"} {
			if strings.Contains(code, leak) {
				t.Errorf("第 %d 行短码 %q 泄漏了 %q", i+1, code, leak)
			}
		}
	}

	if first := rows[0].(map[string]any); first["title"] != "花海" || first["artist"] != " · 周杰伦" {
		t.Errorf("首行标题/歌手 = %v / %v", first["title"], first["artist"])
	}
	if second := rows[1].(map[string]any); second["artist"] != "" {
		t.Errorf("无歌手时 artist 应为空串, got %q", second["artist"])
	}
}

func TestSearchRowsEmpty(t *testing.T) {
	if rows := searchRows(nil); len(rows) != 0 {
		t.Fatalf("空输入应得空行, got %+v", rows)
	}
}

// 分界线就是这次改动的核心: 3 分钟以内语音, 超过发文件, 说不清时按语音发。
func TestWantsFileAroundThreeMinutes(t *testing.T) {
	cases := []struct {
		seconds int
		want    bool
	}{
		{0, false},   // 不知道 -> 语音
		{59, false},  // 短歌
		{180, false}, // 正好 3 分钟 -> 语音
		{181, true},  // 差一秒过线 -> 文件
		{600, true},  // 长歌
	}
	for _, c := range cases {
		if got := wantsFile(c.seconds); got != c.want {
			t.Errorf("wantsFile(%d) = %v, want %v", c.seconds, got, c.want)
		}
	}
}

// 发文件时给 QQ 看的名字: 手机收下来得认得出是哪首歌, 而且不能带非法字符。
func TestFileName(t *testing.T) {
	got := fileName(song.Song{Title: "花海", Artist: "周杰伦", Audio: "http://aqqmusic.tc.qq.com/C400xxx.m4a?vkey=1"})
	if got != "花海 - 周杰伦.m4a" {
		t.Errorf("got %q", got)
	}

	// 网易那条直链查不到扩展名, 按 mp3 算。
	if got := fileName(song.Song{Title: "晴天", Audio: "http://music.163.com/song/media/outer/url?id=186016.mp3"}); got != "晴天.mp3" {
		t.Errorf("got %q", got)
	}

	// 歌名里带 / 之类的字符不能把文件名拆成两层目录。
	if got := fileName(song.Song{Title: "AC/DC", Artist: "a:b", Audio: "http://cdn/x.mp3"}); strings.ContainsAny(got, `/\:`) {
		t.Errorf("文件名里有非法字符: %q", got)
	}
}

func TestAudioExt(t *testing.T) {
	cases := map[string]string{
		"http://cdn/a.mp3":              ".mp3",
		"http://cdn/a.m4a?vkey=1":       ".m4a",
		"http://cdn/outer/url?id=1.mp3": ".mp3",
		"http://cdn/stream":             ".mp3",
		"http://cdn/a":                  ".mp3",
	}
	for in, want := range cases {
		if got := audioExt(in); got != want {
			t.Errorf("audioExt(%q) = %q, want %q", in, got, want)
		}
	}
}

// 指定了音源就得说清是哪一家没有, 并给条退路 —— 否则用户会以为是全网都没有。
func TestEmptyReasonNamesTheSource(t *testing.T) {
	got := emptyReason("花海", song.Wyy)
	if !strings.Contains(got, "网易云") || !strings.Contains(got, "花海") {
		t.Fatalf("要说清是哪家没有: %q", got)
	}
	if !strings.Contains(got, "-s") {
		t.Fatalf("要留一条\"去掉 -s\"的退路: %q", got)
	}

	plain := emptyReason("花海", "")
	if !strings.Contains(plain, "花海") || strings.Contains(plain, "-s") {
		t.Errorf("没指定音源时别提 -s: %q", plain)
	}
}

// 平台报错要翻译成人话, 但不再提"时长超限" —— 超长的歌我们自己就分流成文件了。
func TestFriendlyReason(t *testing.T) {
	if got := friendlyReason(fmt.Errorf("网络超时")); got != "网络超时" {
		t.Errorf("got %q", got)
	}
	if got := friendlyReason(fmt.Errorf("聚合音源 暂时不可用: %w", guard.ErrGate)); !strings.Contains(got, "抽风") {
		t.Errorf("闸门拦下时要给一句能看懂的话: %q", got)
	}
	if got := friendlyReason(fmt.Errorf("聚合音源 请求太密集: %w", guard.ErrGate)); !strings.Contains(got, "等两秒") {
		t.Errorf("got %q", got)
	}
}

// 短码点不动时只能认账, 不能拿短码当关键词重搜 ——
// 上游对任何字符串都会返回一堆结果, 那样只会把"这首取不到"变成一屏不相干的歌。
func TestRefFailureIsHonestNotASearch(t *testing.T) {
	got := refFailure(fmt.Errorf("取链失败: %w", source.ErrNoResult)).Error()
	if !strings.Contains(got, "取不到直链") {
		t.Errorf("取不到直链时要说清楚, got %q", got)
	}
	if strings.Contains(got, "共 10 首") || strings.Contains(got, "点蓝色歌名") {
		t.Errorf("失败时绝不能给出搜索结果卡片: %q", got)
	}

	// 闸门拦下这种"音源抽风"要原样透传, 它有自己的提示语。
	gate := fmt.Errorf("聚合音源 暂时不可用: %w", guard.ErrGate)
	if got := refFailure(gate); !errors.Is(got, guard.ErrGate) {
		t.Errorf("闸门错误应原样透传, got %v", got)
	}
}
