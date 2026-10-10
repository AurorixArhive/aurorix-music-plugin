package play

import (
	"strings"
	"sync"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/parser"
	"github.com/KasumiYuku/Aurorix/lib/plugin"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 指令只注册一次: 注册表是全局的, 反复注册同一个前缀会刷冲突告警。
var registerOnce sync.Once

func commandForTest() *plugin.Command {
	cmd := New(nil, nil).Command()
	registerOnce.Do(func() {
		plugin.Register(&plugin.Plugin{Id: "play-args-probe", Commands: []*plugin.Command{cmd}})
	})
	return cmd
}

// 端到端: 一条真实消息经框架的原生命令解析之后, 插件手里该是什么参数。
//
// 这里不启机器人, 只把 dispatchCommand 的两步 —— 词元解析、kong 解析 —— 各走一遍:
// "指令注册对了但参数取不到"正是最容易编译过、线上哑掉的那一类。
func TestArgsFromRawMessage(t *testing.T) {
	cmd := commandForTest()

	cases := []struct {
		msg    string
		source string
		words  []string
	}{
		{"/play 花海", "", []string{"花海"}},
		{"/点歌 花海", "", []string{"花海"}},
		{"/点播 周杰伦 花海", "", []string{"周杰伦", "花海"}},
		{"点歌 花海", "", []string{"花海"}}, // 不带前缀符号
		{"/点歌 -s wyy 花海", "wyy", []string{"花海"}},
		{"/点歌 花海 -s 1ting", "1ting", []string{"花海"}}, // 参数写在歌名后面
		{"/play 花海 --source=baidu", "baidu", []string{"花海"}},
		{"/点歌 -s wyy", "wyy", nil},             // 只有音源没有歌名 -> 上层回帮助
		{"/点歌", "", nil},                       // 裸指令 -> 上层回帮助
		{"/点歌 花海 -s 网易", "网易", []string{"花海"}}, // 认不出的音源由插件自己报错
		// 带 "-" 的曲名不能被当成参数, 短码字母表里也有 "-"。
		{"/点歌 Lyrith -迷宮リリス-", "", []string{"Lyrith", "-迷宮リリス-"}},
		{"/点歌 -s wyy UAAAA-K", "wyy", []string{"UAAAA-K"}},
	}
	for _, c := range cases {
		t.Run(c.msg, func(t *testing.T) {
			args := parseArgs(t, cmd, c.msg)
			if args.Source != c.source {
				t.Errorf("音源 = %q, want %q", args.Source, c.source)
			}
			if got, want := strings.Join(args.Words, " "), strings.Join(c.words, " "); got != want {
				t.Errorf("关键词 = %q, want %q", got, want)
			}
		})
	}
}

// 卡片上那行命令得一路走到能播的那首歌: 短码里的字符一个都不能被参数解析吃掉。
func TestCardRefSurvivesCommandParsing(t *testing.T) {
	cmd := commandForTest()

	picks := []song.Song{
		{Source: song.QQ, ID: "002hG4kq0WZBma"},
		{Source: song.Wyy, ID: "1901371647"},
		{Source: song.Baidu, ID: "T10049755016"},
		{Source: song.Ting1, ID: "1219544"},
	}
	// 字母表里带 "-": 随便编一批数字 ID, 挑出真的带横杠的那些一起验。
	for i := 1; i < 400 && len(picks) < 8; i++ {
		ref := (song.Song{Source: song.Wyy, ID: itoa(i)}).Ref()
		if strings.Contains(ref, "-") {
			picks = append(picks, song.Song{Source: song.Wyy, ID: itoa(i)})
		}
	}

	for _, pick := range picks {
		args := parseArgs(t, cmd, "/"+commandName+" "+pick.Ref())
		got, ok := song.ParseRef(strings.Join(args.Words, " "))
		if !ok {
			t.Fatalf("%s %s 的短码没解回来: %q", pick.Source, pick.ID, args.Words)
		}
		if got.Source != pick.Source || got.ID != pick.ID {
			t.Errorf("解成 %+v, want %+v", got, pick)
		}
		if args.Source != "" {
			t.Errorf("%s 的短码被当成了 -s 的值: %q", pick.Ref(), args.Source)
		}
	}
}

// parseArgs 复刻框架 dispatchCommand 把一条消息变成 ctx.Parsed 的过程。
func parseArgs(t *testing.T, cmd *plugin.Command, msg string) *playArgs {
	t.Helper()

	tokens := strings.Fields(msg)
	root, afterRoot, ok := plugin.ResolveRoot(tokens)
	if !ok {
		t.Fatalf("%q 没解析出任何指令", msg)
	}
	if root.Prefix != commandName {
		t.Fatalf("%q 解析成了指令 %q", msg, root.Prefix)
	}
	_, _, rest := plugin.Resolve(root, afterRoot)

	parsed, _, err := parser.ParseArgs(commandName, cmd.Args, rest)
	if err != nil {
		t.Fatalf("%q 的参数解析失败: %v", msg, err)
	}
	args, ok := parsed.(*playArgs)
	if !ok {
		t.Fatalf("%q 解出来的是 %T", msg, parsed)
	}
	return args
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
