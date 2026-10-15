package music

import (
	"strings"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/templates"
)

func TestSearchTemplateRendersQuotedInlineLinks(t *testing.T) {
	if err := templates.RegisterFS("music-test", templateFS, "templates/markdown"); err != nil {
		t.Fatal(err)
	}

	got, err := templates.FillFor("music-test", "search", templates.Args{
		"keyword": "花海",
		"total":   "2",
		"songs": []any{
			map[string]any{"title": "花海", "artist": " · 周杰伦", "command": "/点歌 QAAMywUJgQ0q229"},
			map[string]any{"title": "花海", "artist": " · 中孝介", "command": "/点歌 QAAO5-Ec0F1EwT2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("渲染结果:\n%s", got)

	for _, want := range []string{
		"# 🎵 花海",
		"共 2 首 · 点蓝色歌名直接发送",
		"> <qqbot-cmd-input text=\"%2F%E7%82%B9%E6%AD%8C%20QAAMywUJgQ0q229\" show=\"%E8%8A%B1%E6%B5%B7\" />",
		" · 周杰伦",
		" · 中孝介",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("渲染结果缺少 %q\n%s", want, got)
		}
	}

	// 卡片上不能出现音源名、曲目 ID 或分隔符。
	for _, leak := range []string{"kuwo", "netease", "qq:"} {
		if strings.Contains(got, leak) {
			t.Errorf("卡片泄漏了 %q\n%s", leak, got)
		}
	}
}

func TestFailAndHelpTemplatesRender(t *testing.T) {
	if err := templates.RegisterFS("music-test", templateFS, "templates/markdown"); err != nil {
		t.Fatal(err)
	}

	fail, err := templates.FillFor("music-test", "fail", templates.Args{"reason": "没有找到相关歌曲"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fail, "没有找到相关歌曲") {
		t.Errorf("fail 模板 = %q", fail)
	}

	help, err := templates.FillFor("music-test", "help", templates.Args{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help, "<qqbot-cmd-input") {
		t.Errorf("help 模板应含可点击的指令标签: %q", help)
	}
}

// 点歌时先发的那张卡片: 封面 + 标题 + 作者, 点开跳歌曲页。
// 模板里的参数名与 play.sendCard 传的必须是同一套, 这里照着实际传的值钉死。
func TestSongCardTemplateRenders(t *testing.T) {
	if err := templates.RegisterFS("music-test", templateFS, "templates/markdown"); err != nil {
		t.Fatal(err)
	}

	rendered, err := templates.FillFor("music-test", "songcard", templates.Args{
		"cover":  "https://y.gtimg.cn/cover.jpg",
		"link":   "https://y.qq.com/n/ryqq/songDetail/abc",
		"title":  "江湖少年",
		"artist": "封茗囧菌,双笙",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("歌曲卡片:\n%s", rendered)

	want := "<mqqapi://markdown/template?id=2" +
		"&leftImageUrl=https%3A%2F%2Fy.gtimg.cn%2Fcover.jpg" +
		"&scheme=https%3A%2F%2Fy.qq.com%2Fn%2Fryqq%2FsongDetail%2Fabc" +
		"&text1=%E6%B1%9F%E6%B9%96%E5%B0%91%E5%B9%B4" +
		"&text2=%E5%B0%81%E8%8C%97%E5%9B%A7%E8%8F%8C%2C%E5%8F%8C%E7%AC%99>"
	if got := strings.TrimSpace(rendered); got != want {
		t.Fatalf("歌曲卡片不对:\n实得 %s\n期望 %s", got, want)
	}
}
