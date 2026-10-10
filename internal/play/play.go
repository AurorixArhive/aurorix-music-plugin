package play

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/KasumiYuku/Aurorix/lib/constant"
	botcontext "github.com/KasumiYuku/Aurorix/lib/context"
	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/templates"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/source"
)

const (
	commandName = "play"
	maxResults  = 10
	// QQ 语音的硬上限是 300 秒: 超了平台会连正文一起打回(40093013 上传音频时长超过限制),
	// 与体积、文件名、上传方式都无关, 所以卡片上干脆不列。
	maxVoiceSeconds = 300
)

// 十个人同时点同一首歌, 只向上游取一次直链, 其余的人共享这次结果。
var audioFlight = guard.NewFlight()

// Play 通过 Pool 取音源: 优先级改了就跟着变, 不重建实例。
type Play struct {
	sources *source.Pool
}

func New(sources *source.Pool) *Play {
	return &Play{sources: sources}
}

// playArgs 是这条指令的参数契约, 交给框架的 kong 解析, 结果塞进 ctx.Parsed。
// 声明式而不是自己切字符串: 参数写错时框架会把原因和用法直接回给用户。
type playArgs struct {
	Source string   `kong:"name='source',short='s',help='音源: wyy 网易云 / kw 酷我 / qq QQ音乐, 不填自动挑'"`
	Words  []string `kong:"arg,optional,name='query',help='歌名, 或卡片上的短码'"`
}

func (p *Play) Command() *plugin.Command {
	return &plugin.Command{
		Prefix:      commandName,
		Aliases:     []string{"点歌", "点播", "music"},
		Role:        constant.RoleMember,
		Describe:    "搜歌并发语音, 可用 -s 指定音源",
		Args:        &playArgs{},
		Handle:      p.handle,
		HandleError: p.onError,
	}
}

func (p *Play) handle(ctx *botcontext.MessageContext) error {
	args := parsed(ctx)
	keyword := strings.Join(args.Words, " ")
	if keyword == "" {
		return p.render(ctx, "help", &templates.Args{})
	}

	// 卡片上的短码自带音源, 这时 -s 没有意义, 忽略掉即可。
	if pick, ok := song.ParseRef(keyword); ok {
		if err := p.deliver(ctx, pick); err != nil {
			return refFailure(err)
		}
		return nil
	}

	only, err := pickSource(args.Source)
	if err != nil {
		return err
	}
	return p.search(ctx, keyword, only)
}

// 短码点不动时给用户看什么。能解出短码就说明这是卡片上的那一首, 绝不能拿它再去当
// 关键词搜 —— 上游对任何字符串都会返回一堆结果, 那样只会把"这首取不到"变成一屏不相干的歌。
func refFailure(err error) error {
	if errors.Is(err, source.ErrNoResult) {
		return errors.New("这首歌在音源里取不到直链(多半是付费或版权限制), 换一首或换个关键词重新搜吧")
	}
	return err
}

// search 搜歌。only 非空时只在那一家的音源里找, 空则走默认的降级链。
func (p *Play) search(ctx *botcontext.MessageContext, keyword string, only song.Source) error {
	found, err := p.sources.SearchOn(only, keyword, maxResults)
	if err != nil {
		return err
	}
	songs, skipped := fitVoice(found)
	if len(songs) == 0 {
		return p.render(ctx, "fail", &templates.Args{
			"reason": emptyReason(keyword, only, len(found), skipped),
		})
	}
	return p.renderCard(ctx, keyword, songs)
}

func (p *Play) renderCard(ctx *botcontext.MessageContext, keyword string, songs []song.Song) error {
	return p.render(ctx, "search", &templates.Args{
		"keyword": keyword,
		"total":   strconv.Itoa(len(songs)),
		"songs":   searchRows(songs),
	})
}

func (p *Play) deliver(ctx *botcontext.MessageContext, pick song.Song) error {
	link, err := audioFlight.Do(pick.Ref(), func() (string, error) {
		return p.sources.Audio(pick)
	})
	if err != nil {
		return err
	}
	return ctx.Voice(link).Send()
}

func (p *Play) render(ctx *botcontext.MessageContext, id string, args *templates.Args) error {
	msg, err := ctx.MarkdownTemplate(id, args)
	if err != nil {
		return err
	}
	return msg.Send()
}
func (p *Play) onError(ctx *botcontext.MessageContext, err error) error {
	return p.render(ctx, "fail", &templates.Args{"reason": friendlyReason(err)})
}

// friendlyReason 把平台的生硬报错换成能看懂的话。
func friendlyReason(err error) string {
	reason := err.Error()
	if strings.Contains(reason, "40093013") || strings.Contains(reason, "时长超过限制") {
		return fmt.Sprintf("这首歌超过 %d 分钟, QQ 语音放不下, 换一首吧", maxVoiceSeconds/60)
	}
	if strings.Contains(reason, "暂时不可用") {
		return fmt.Sprintf("音源临时抽风了, 重新发一次 /%s <歌名> 会自动换别的音源", commandName)
	}
	if strings.Contains(reason, "请求太密集") {
		return "刚刚点的人有点多, 等两秒再来"
	}
	return reason
}

// searchRows 只把自包含的 ref 交给卡片: 不依赖服务端状态, 所以卡片放多久都还能点。
func searchRows(songs []song.Song) []any {
	rows := make([]any, 0, len(songs))
	for _, s := range songs {
		artist := ""
		if s.Artist != "" {
			artist = " · " + s.Artist
		}
		rows = append(rows, map[string]any{
			"title":   s.Title,
			"artist":  artist,
			"command": "/" + commandName + " " + s.Ref(),
		})
	}
	return rows
}

// fitVoice 丢掉超过时长上限的歌, 返回保留下来的与丢掉的数量。
// 时长未知的来源(值为 0)不动它, 免得把本来能播的也滤没了。
func fitVoice(songs []song.Song) ([]song.Song, int) {
	kept := make([]song.Song, 0, len(songs))
	skipped := 0
	for _, s := range songs {
		if s.Duration > maxVoiceSeconds {
			skipped++
			continue
		}
		kept = append(kept, s)
	}
	return kept, skipped
}

// emptyReason 没搜到结果时给用户的话。
// 指定了音源就得说清是哪一家没有 —— 否则用户会以为是全网都没有。
func emptyReason(keyword string, only song.Source, found, skipped int) string {
	if skipped > 0 && skipped == found {
		return fmt.Sprintf("搜到的 %d 首全都超过 %d 分钟, QQ 语音放不出来, 换首短点的吧",
			skipped, maxVoiceSeconds/60)
	}
	if only != "" {
		return fmt.Sprintf("%s里没有找到「%s」，换个关键词，或者去掉 -s 让我们自动挑音源",
			sourceLabel(only), keyword)
	}
	return fmt.Sprintf("没有找到「%s」相关歌曲，换个关键词试试", keyword)
}

// parsed 取框架解好的参数。类型对不上说明声明和框架对不上了, 这时按"没写参数"处理:
// 用户会看到帮助卡片, 比甩一句内部错误清楚。
func parsed(ctx *botcontext.MessageContext) *playArgs {
	if args, ok := ctx.Parsed.(*playArgs); ok {
		return args
	}
	return &playArgs{}
}

// pickSource 把 -s 的值翻成内部音源, 空表示不限定。
// 只认三个短名: 手机上打得快, 也不会和歌名里的词撞上。
func pickSource(name string) (song.Source, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		return "", nil
	case "wyy":
		return song.Netease, nil
	case "kw":
		return song.Kuwo, nil
	case "qq":
		return song.QQ, nil
	}
	return "", fmt.Errorf("音源只认 wyy(网易云)、kw(酷我)、qq(QQ音乐)，%s 不在里面", name)
}

// sourceLabel 音源的中文名, 说给用户听的时候用。
func sourceLabel(src song.Source) string {
	switch src {
	case song.QQ:
		return "QQ 音乐"
	case song.Kuwo:
		return "酷我"
	case song.Netease:
		return "网易云"
	}
	return string(src)
}
