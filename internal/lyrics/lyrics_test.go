package lyrics

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"strings"
	"testing"
)

const sampleLRC = `[ti:花海]
[ar:周杰伦]
[al:]
[by:]
[offset:0]
[00:00.00]花海 - 周杰伦
[00:27.320]静止了 所有的花开
[00:34.068]遥远了 清晰了爱
[00:40.574]天郁闷 爱却很喜欢
[00:40.574]天郁闷 爱却很喜欢
`

func TestParseDropsMetadataAndRepeats(t *testing.T) {
	lines := parse(sampleLRC)
	want := []string{"花海 - 周杰伦", "静止了 所有的花开", "遥远了 清晰了爱", "天郁闷 爱却很喜欢"}
	if len(lines) != len(want) {
		t.Fatalf("解析出 %d 行: %q", len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("第 %d 行 = %q, want %q", i+1, lines[i], want[i])
		}
	}
}

// 上游给纯音乐填的就是这句占位词; 印到图上会让人以为是我们坏了。
func TestParseDropsTheInstrumentalPlaceholder(t *testing.T) {
	lrc := "[00:00:00]此歌曲为没有填词的纯音乐，请您欣赏\n[00:10.00]真的歌词"
	if lines := parse(lrc); len(lines) != 1 || lines[0] != "真的歌词" {
		t.Fatalf("got %q", lines)
	}
}

func TestParseEmpty(t *testing.T) {
	for _, lrc := range []string{"", "[ti:花海]\n[ar:周杰伦]\n", "\n\n"} {
		if lines := parse(lrc); len(lines) != 0 {
			t.Errorf("parse(%q) = %q, want 空", lrc, lines)
		}
	}
}

func TestRenderProducesJPEG(t *testing.T) {
	data, err := Render("花海", "周杰伦", sampleLRC)
	if err != nil {
		t.Fatalf("出图失败: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("出来的不是合法 JPEG: %v", err)
	}
	if got := img.Bounds().Dx(); got != canvasW {
		t.Errorf("宽度 = %d, want %d", got, canvasW)
	}
	// 高度得跟行数走: 四行歌词的图不该矮到只剩页头, 也不该高得像长图。
	if h := img.Bounds().Dy(); h < 300 || h > 2000 {
		t.Errorf("高度 = %d, 看起来没按行数排版", h)
	}
}

// 同一个标题不该因为换行数不同而变高变矮到离谱。
func TestRenderHeightFollowsLineCount(t *testing.T) {
	short, err := Render("短", "", "[00:00.00]一句")
	if err != nil {
		t.Fatalf("出图失败: %v", err)
	}
	var many []string
	for i := 0; i < 20; i++ {
		many = append(many, fmt.Sprintf("[00:%02d.00]第 %d 句歌词", i, i))
	}
	long, err := Render("长", "", strings.Join(many, "\n"))
	if err != nil {
		t.Fatalf("出图失败: %v", err)
	}
	hs, hl := decodeHeight(t, short), decodeHeight(t, long)
	if hl <= hs {
		t.Fatalf("20 行的图 (%d) 不该比 1 行的图 (%d) 矮", hl, hs)
	}
}

// 中文得真能画出来: 字体没装上的话, 画面会整片空白, 而这不报错。
func TestRenderActuallyPaintsGlyphs(t *testing.T) {
	data, err := Render("花海", "周杰伦", "[00:00.00]静止了所有的花开")
	if err != nil {
		t.Fatalf("出图失败: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	ground := img.At(padX/2, padY/2)
	ink := 0
	b := img.Bounds()
	for y := padY; y < b.Dy()-padY; y += 3 {
		for x := padX; x < b.Dx()-padX; x += 3 {
			if !sameColor(img.At(x, y), ground) {
				ink++
			}
		}
	}
	if ink < 100 {
		t.Fatalf("正文区域只有 %d 个非背景像素, 字体多半没画上去", ink)
	}
}

func TestRenderEmptyLyrics(t *testing.T) {
	if _, err := Render("花海", "周杰伦", "[ti:花海]\n[ar:周杰伦]"); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
}

func decodeHeight(t *testing.T, data []byte) int {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Height
}

func sameColor(a, b interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

// 字体缺字时画出来的是同一个"豆腐块", 看着有像素、其实一个字都没认出来。
// 拿两个不同的常用汉字比一比: 字形不同才说明字库里真收着它们。
func TestGlyphsAreDistinctNotTofu(t *testing.T) {
	inkOf := func(lrc string) int {
		data, err := Render("花海", "", lrc)
		if err != nil {
			t.Fatalf("出图失败: %v", err)
		}
		return bodyInk(t, data)
	}
	if a, b := inkOf("[00:00.00]花"), inkOf("[00:00.00]海"); a == b {
		t.Fatalf("「花」与「海」画出来一模一样(%d 个字形像素), 字体多半没有中文字形", a)
	}
}

// bodyInk 数一遍正文区域里的字形像素。
func bodyInk(t *testing.T, data []byte) int {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ground := img.At(padX/2, padY/2)
	count := 0
	b := img.Bounds()
	for y := padY; y < b.Dy(); y++ {
		for x := padX; x < b.Dx()-padX; x++ {
			if !sameColor(img.At(x, y), ground) {
				count++
			}
		}
	}
	return count
}
