// Package lyrics 把 LRC 歌词排成一张竖图。
//
// 为什么出图而不是直接发文字: 歌词动辄三四十行, 直接刷屏会把群聊顶掉;
// 一张图点开才看, 而且能给标题和歌手留位置。
package lyrics

import (
	"bytes"
	"embed"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"regexp"
	"strings"
	"sync"

	"github.com/KasumiYuku/ink/raster"
	"github.com/KasumiYuku/ink/text"
)

//go:embed assets/font/DingTalkJinBuTi.ttf
var fontFS embed.FS

// ErrEmpty 表示这份 LRC 里没有可显示的行(纯音乐、或只带了 [ti:][ar:] 标签)。
var ErrEmpty = errors.New("没有可显示的歌词")

const (
	canvasW    = 1080
	padX       = 72
	padY       = 72
	titleSize  = 52
	artistSize = 34
	lyricSize  = 40
	lineGap    = 30 // 歌词行距
	headGap    = 56 // 标题到歌手、歌手到正文的间隔
	// maxLines 是正文行数上限。再长就不是一张"图"而是一条带子了, 发出去也刷不完。
	maxLines = 60
	quality  = 88
)

var (
	fontOnce sync.Once
	fontSet  *text.Set
	fontErr  error
)

// palette 是这张图的配色, 收在一起免得色值散在绘制代码里。
var (
	inkGround = color.RGBA{R: 0x10, G: 0x12, B: 0x18, A: 0xFF}
	inkTitle  = color.RGBA{R: 0xF2, G: 0xF4, B: 0xF8, A: 0xFF}
	inkArtist = color.RGBA{R: 0x8E, G: 0x96, B: 0xA8, A: 0xFF}
	inkLyric  = color.RGBA{R: 0xDD, G: 0xE2, B: 0xEC, A: 0xFF}
)

// Render 把歌名与 LRC 排成一张 JPEG。没有可显示的行时返回 ErrEmpty。
func Render(title, artist, lrc string) ([]byte, error) {
	set, err := fonts()
	if err != nil {
		return nil, err
	}
	rows := parse(lrc)
	if len(rows) == 0 {
		return nil, ErrEmpty
	}
	if len(rows) > maxLines {
		rows = append(rows[:maxLines:maxLines], "……")
	}

	chain := set.Chain("cjk")
	faceTitle, faceArtist, faceLyric := chain.Face(titleSize), chain.Face(artistSize), chain.Face(lyricSize)

	// 先排版再开画布: 宽度定了才知道每行折几折, 折完才知道图得多高。
	width := float64(canvasW - 2*padX)
	body := make([]string, 0, len(rows))
	for _, row := range rows {
		body = append(body, text.Wrap(faceLyric, row, width, text.Style{})...)
	}

	lineHeight := faceLyric.Metrics().LineHeight + lineGap
	height := padY + int(faceTitle.Metrics().LineHeight) + headGap
	if line := strings.TrimSpace(artist); line != "" {
		height += int(faceArtist.Metrics().LineHeight) + headGap
	}
	height += int(float64(len(body))*lineHeight) + padY

	dst := image.NewRGBA(image.Rect(0, 0, canvasW, height))
	raster.Fill(dst, dst.Rect, inkGround)

	y := float64(padY) + faceTitle.Metrics().Ascent
	text.Draw(dst, faceTitle, padX, y, firstLine(faceTitle, title, width), text.Style{Ink: inkTitle, Quantize: true})
	y += headGap
	if line := strings.TrimSpace(artist); line != "" {
		y += faceArtist.Metrics().Ascent
		text.Draw(dst, faceArtist, padX, y, firstLine(faceArtist, line, width), text.Style{Ink: inkArtist, Quantize: true})
		y += headGap
	}

	for i, line := range body {
		y += faceLyric.Metrics().Ascent
		text.Draw(dst, faceLyric, padX, y, line, text.Style{Ink: inkLyric, Quantize: true})
		if i < len(body)-1 {
			y += lineHeight - faceLyric.Metrics().Ascent
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// firstLine 把一行标题按宽度收成一行(放不下就带省略号)。
func firstLine(face text.Face, s string, width float64) string {
	lines := text.WrapLimit(face, s, width, 1, text.Style{})
	if len(lines) == 0 {
		return s
	}
	return lines[0]
}

var (
	timeTag = regexp.MustCompile(`\[\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?\]`)
	// metaTag 是 LRC 的头部标签; 这些行是元信息, 不是歌词。
	metaTag = regexp.MustCompile(`^\[(ti|ar|al|by|offset|re|ve|kana):`)
)

// parse 抽出一行行纯歌词。带多个时间戳的行只算一次 —— 时间戳用来对轴, 出图不需要。
func parse(lrc string) []string {
	out := make([]string, 0, 64)
	for _, raw := range strings.Split(lrc, "\n") {
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if raw == "" || metaTag.MatchString(raw) {
			continue
		}
		line := strings.TrimSpace(timeTag.ReplaceAllString(raw, ""))
		if line == "" || isPlaceholder(line) {
			continue
		}
		// 上游常把同一句连写两遍, 连着重复的只留一条。
		if len(out) > 0 && out[len(out)-1] == line {
			continue
		}
		out = append(out, line)
	}
	return out
}

// isPlaceholder 认出上游给纯音乐填的那句占位词 —— 它不是歌词, 印出来只会让人以为出错了。
func isPlaceholder(line string) bool {
	return strings.Contains(line, "此歌曲为没有填词的纯音乐") ||
		strings.Contains(line, "纯音乐，请欣赏") ||
		strings.Contains(line, "纯音乐,请欣赏")
}

// fonts 只加载一次字体: 解析一个 2MB 的 ttf 要几十毫秒, 每次出图都做一遍纯属浪费。
func fonts() (*text.Set, error) {
	fontOnce.Do(func() {
		data, err := fontFS.ReadFile("assets/font/DingTalkJinBuTi.ttf")
		if err != nil {
			fontErr = err
			return
		}
		set := text.NewSet(text.Options{})
		if err := set.Load("cjk", data); err != nil {
			fontErr = err
			return
		}
		fontSet = set
	})
	return fontSet, fontErr
}
