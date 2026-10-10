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

// -s 只认三个短名。写别的必须说清认哪些, 而不是静默当成"不限定"。
func TestPickSource(t *testing.T) {
	known := []struct {
		in   string
		want song.Source
	}{
		{"", ""},
		{"wyy", song.Netease},
		{"WYY", song.Netease},
		{" kw ", song.Kuwo},
		{"qq", song.QQ},
	}
	for _, c := range known {
		got, err := pickSource(c.in)
		if err != nil || got != c.want {
			t.Errorf("pickSource(%q) = (%q, %v), want (%q, nil)", c.in, got, err, c.want)
		}
	}

	for _, bad := range []string{"netease", "网易", "kww"} {
		got, err := pickSource(bad)
		if err == nil {
			t.Errorf("pickSource(%q) 该报错, got %q", bad, got)
			continue
		}
		if !strings.Contains(err.Error(), "wyy") || !strings.Contains(err.Error(), bad) {
			t.Errorf("报错要列出手上认哪些并带上用户写的值: %v", err)
		}
	}
}

// 卡片上那行命令必须自包含: 发出去多久、服务端重启多少次, 点下去都还能找到同一首歌。
func TestSearchRowsUseSelfContainedRef(t *testing.T) {
	songs := []song.Song{
		{Source: song.QQ, ID: "003cI52o4daJJL", Title: "花海", Artist: "周杰伦"},
		{Source: song.Kuwo, ID: "440615", Title: "花海"},
	}

	rows := searchRows(songs)
	if len(rows) != 2 {
		t.Fatalf("行数 = %d, want 2", len(rows))
	}

	for i := range songs {
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
		if pick.Source != songs[i].Source || pick.ID != songs[i].ID {
			t.Errorf("第 %d 行解成 %+v, want %+v", i+1, pick, songs[i])
		}
	}

	// 短码必须短、必须只用那一套字母表, 而且不能把音源名或曲目 ID 写进去。
	for i, row := range rows {
		code := strings.TrimPrefix(row.(map[string]any)["command"].(string), "/play ")
		want := 7
		if songs[i].Source == song.QQ {
			want = 15
		}
		if len(code) != want {
			t.Errorf("第 %d 行短码 %q 长度 %d, want %d", i+1, code, len(code), want)
		}
		for _, c := range code {
			if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", c) {
				t.Errorf("第 %d 行短码 %q 里有字母表外的字符 %q", i+1, code, c)
			}
		}
		for _, leak := range []string{"qq", "kuwo", "netease", songs[i].ID, ":"} {
			if strings.Contains(code, leak) {
				t.Errorf("第 %d 行短码 %q 泄漏了 %q", i+1, code, leak)
			}
		}
	}

	first := rows[0].(map[string]any)
	if first["title"] != "花海" || first["artist"] != " · 周杰伦" {
		t.Errorf("首行标题/歌手 = %v / %v", first["title"], first["artist"])
	}
	second := rows[1].(map[string]any)
	if second["artist"] != "" {
		t.Errorf("无歌手时 artist 应为空串, got %q", second["artist"])
	}
}

func TestSearchRowsEmpty(t *testing.T) {
	if rows := searchRows(nil); len(rows) != 0 {
		t.Fatalf("空输入应得空行, got %+v", rows)
	}
}

// QQ 语音硬上限 300 秒, 超了整条消息会被平台打回, 所以卡片上不能列。
func TestFitVoiceDropsOnlyOverLimit(t *testing.T) {
	in := []song.Song{
		{Title: "刚过线", Duration: 301},
		{Title: "正好 300", Duration: 300},
		{Title: "299", Duration: 299},
		{Title: "时长未知", Duration: 0},
		{Title: "很长", Duration: 378},
	}

	kept, skipped := fitVoice(in)
	if skipped != 2 {
		t.Fatalf("跳过数 = %d, want 2", skipped)
	}
	if len(kept) != 3 {
		t.Fatalf("保留数 = %d, want 3", len(kept))
	}
	for _, s := range kept {
		if s.Title == "刚过线" || s.Title == "很长" {
			t.Errorf("超过上限的 %q 不该留下", s.Title)
		}
	}
}

func TestEmptyReasonDistinguishesTooLongFromNotFound(t *testing.T) {
	allLong := emptyReason("花海", "", 4, 4)
	if !strings.Contains(allLong, "5 分钟") || !strings.Contains(allLong, "4 首") {
		t.Errorf("全被滤掉时要说明原因: %q", allLong)
	}

	notFound := emptyReason("花海", "", 0, 0)
	if !strings.Contains(notFound, "花海") || strings.Contains(notFound, "5 分钟") {
		t.Errorf("真没搜到时不该提时长: %q", notFound)
	}
}

// 指定了音源就得说清是哪一家没有, 并给条退路 —— 否则用户会以为是全网都没有。
func TestEmptyReasonNamesTheSource(t *testing.T) {
	got := emptyReason("花海", song.Netease, 0, 0)
	if !strings.Contains(got, "网易云") || !strings.Contains(got, "花海") {
		t.Fatalf("要说清是哪家没有: %q", got)
	}
	if !strings.Contains(got, "-s") {
		t.Fatalf("要留一条\"去掉 -s\"的退路: %q", got)
	}

	for src, want := range map[song.Source]string{
		song.QQ:      "QQ 音乐",
		song.Kuwo:    "酷我",
		song.Netease: "网易云",
	} {
		if label := sourceLabel(src); label != want {
			t.Errorf("sourceLabel(%q) = %q, want %q", src, label, want)
		}
	}
}

// 平台报错要翻译成人话, 而不是把 40093013 直接甩给用户。
func TestOnErrorTranslatesDurationRejection(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"上传音频时长超过限制: 40093013", "超过 5 分钟"},
		{"40093013", "超过 5 分钟"},
		{"网络超时", "网络超时"},
	}
	for _, c := range cases {
		got := friendlyReason(fmt.Errorf("%s", c.in))
		if !strings.Contains(got, c.want) {
			t.Errorf("friendlyReason(%q) = %q, 应包含 %q", c.in, got, c.want)
		}
	}
}

// 短码点不动时只能认账, 不能拿短码当关键词重搜 ——
func TestRefFailureIsHonestNotASearch(t *testing.T) {
	got := refFailure(fmt.Errorf("取链失败: %w", source.ErrNoResult)).Error()
	if !strings.Contains(got, "取不到直链") {
		t.Errorf("取不到直链时要说清楚, got %q", got)
	}
	if strings.Contains(got, "共 10 首") || strings.Contains(got, "点蓝色歌名") {
		t.Errorf("失败时绝不能给出搜索结果卡片: %q", got)
	}

	// 闸门拦下这种"音源抽风"要原样透传, 它有自己的提示语。
	gate := fmt.Errorf("QQ 音源 暂时不可用: %w", guard.ErrGate)
	if got := refFailure(gate); !errors.Is(got, guard.ErrGate) {
		t.Errorf("闸门错误应原样透传, got %v", got)
	}
}

// 还原用户那次事故: 短码解出来是对的, 上游取不到直链。
func TestDecodedRefFromBugReport(t *testing.T) {
	pick, ok := song.ParseRef("QAAJGoRRoDr2VYo")
	if !ok {
		t.Fatal("用户报的那张卡片上的短码应该能解出来")
	}
	if pick.Source != song.QQ || pick.ID != "002hG4kq0WZBma" {
		t.Fatalf("解出来 = %+v", pick)
	}
	if pick.Ref() != "QAAJGoRRoDr2VYo" {
		t.Fatalf("再编回去 = %q", pick.Ref())
	}
}
