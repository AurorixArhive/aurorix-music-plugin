package play

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/KasumiYuku/Aurorix/lib/constant"
	botcontext "github.com/KasumiYuku/Aurorix/lib/context"
	"github.com/KasumiYuku/Aurorix/lib/logx"
	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/templates"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/lyrics"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/source"
)

const (
	commandName = "play"
	maxResults  = 10
	// voiceSeconds 是语音与文件的分界线。平台对语音的硬上限是 300 秒, 这里再收一档到 3 分钟:
	// 一首歌听一半就得划走, 不如直接给文件, 用户还能拖进度。
	voiceSeconds = 3 * 60
)

var logger = logx.New("music")

// 十个人同时点同一首歌, 只向上游取一次直链与歌词, 其余的人共享这次结果。
var detailFlight = guard.NewFlight[song.Song]()

// Play 通过 Pool 取音源: 优先级改了就跟着变, 不重建实例。
type Play struct {
	sources  *source.Pool
	lyricsOn func() bool
}

func New(sources *source.Pool, lyricsOn func() bool) *Play {
	return &Play{sources: sources, lyricsOn: lyricsOn}
}

// playArgs 是这条指令的参数契约, 交给框架的 kong 解析, 结果塞进 ctx.Parsed。
// 声明式而不是自己切字符串: 参数写错时框架会把原因和用法直接回给用户。
type playArgs struct {
	Source string   `kong:"name='source',short='s',help='音源: qq QQ音乐 / wyy 网易云 / baidu 百度 / 1ting 一听, 不填自动挑'"`
	Words  []string `kong:"arg,optional,name='query',help='歌名, 或卡片上的短码'"`
}

func (p *Play) Command() *plugin.Command {
	return &plugin.Command{
		Prefix:      commandName,
		Aliases:     []string{"点歌", "点播", "music"},
		Role:        constant.RoleMember,
		Describe:    "搜歌, 短的发语音、长的发文件, 可用 -s 指定音源",
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
		return p.deliver(ctx, pick)
	}

	only, err := pickSource(args.Source)
	if err != nil {
		return err
	}
	return p.search(ctx, keyword, only)
}

// search 搜歌。only 非空时只在那一家的音源里找, 空则走默认的降级链。
func (p *Play) search(ctx *botcontext.MessageContext, keyword string, only song.Source) error {
	songs, err := p.sources.SearchOn(only, keyword, maxResults)
	if err != nil {
		return err
	}
	if len(songs) == 0 {
		return p.render(ctx, "fail", &templates.Args{"reason": emptyReason(keyword, only)})
	}
	return p.render(ctx, "search", &templates.Args{
		"keyword": keyword,
		"total":   strconv.Itoa(len(songs)),
		"songs":   searchRows(songs),
	})
}

// deliver 点开卡片之后的那一步: 按短码里的 (音源, ID) 现换一条直链, 再决定发语音还是发文件。
func (p *Play) deliver(ctx *botcontext.MessageContext, pick song.Song) error {
	found, err := detailFlight.Do(pick.Ref(), func() (song.Song, error) {
		return p.sources.Detail(pick)
	})
	if err != nil {
		return refFailure(err)
	}
	if err := p.sendAudio(ctx, found); err != nil {
		return err
	}
	return p.sendLyrics(ctx, found)
}

// sendAudio 短的发语音、长的发文件。
func (p *Play) sendAudio(ctx *botcontext.MessageContext, found song.Song) error {
	if wantsFile(found.Duration) {
		// 直链直传: QQ 自己去下, 我们不过一遍手 —— 省掉一次完整下载加一次上传。
		return ctx.File(found.Audio, fileName(found)).Send()
	}
	return ctx.Voice(found.Audio).Send()
}

// wantsFile 超过分界线就发文件。时长未知(0)按语音发 —— 那是上限之内的多数情况,
// 而平台真放不下会自己报错, 不用我们先拦。
func wantsFile(seconds int) bool { return seconds > voiceSeconds }

// sendLyrics 开开关、有歌词、排得出来才发。任何一步不成都不影响已经发出去的歌。
func (p *Play) sendLyrics(ctx *botcontext.MessageContext, found song.Song) error {
	if !p.lyricsEnabled() || strings.TrimSpace(found.Lyrics) == "" {
		return nil
	}
	img, err := lyrics.Render(found.Title, found.Artist, found.Lyrics)
	if errors.Is(err, lyrics.ErrEmpty) {
		return nil
	}
	if err != nil {
		logger.Warnf("歌词出图失败 %s: %v", found.Label(), err)
		return nil
	}
	return ctx.Image(img, "").Send()
}

func (p *Play) lyricsEnabled() bool {
	return p.lyricsOn == nil || p.lyricsOn()
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

// refFailure 短码点不动时给用户看的话。能解出短码就说明这是卡片上的那一首,
// 绝不能拿它再去当关键词搜 —— 上游对任何字符串都会返回一堆结果,
// 那样只会把"这首取不到"变成一屏不相干的歌。
func refFailure(err error) error {
	if errors.Is(err, source.ErrNoResult) {
		return errors.New("这首歌取不到直链(多半是付费或版权限制), 换一首或换个关键词重新搜吧")
	}
	return err
}

// emptyReason 没搜到结果时给用户的话。
// 指定了音源就得说清是哪一家没有 —— 否则用户会以为是全网都没有。
func emptyReason(keyword string, only song.Source) string {
	if only != "" {
		return fmt.Sprintf("%s里没有找到「%s」，换个关键词，或者去掉 -s 让我们自动挑音源", only.CN(), keyword)
	}
	return fmt.Sprintf("没有找到「%s」相关歌曲，换个关键词试试", keyword)
}

// fileName 是发文件时给 QQ 看的名字: 收下来就是"花海 - 周杰伦.m4a"。
func fileName(s song.Song) string {
	illegal := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	name := strings.TrimSpace(illegal.Replace(s.Label()))
	if name == "" {
		name = "music"
	}
	return name + audioExt(s.Audio)
}

// audioExt 从直链的路径上取扩展名; 取不到按 mp3 —— 两家最常用的就是它。
func audioExt(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	ext := strings.ToLower(path.Ext(rawURL))
	switch ext {
	case ".mp3", ".m4a", ".flac", ".wav", ".aac", ".ogg", ".ape":
		return ext
	}
	return ".mp3"
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
// 名字表在 song 包里, 与配置项 source_order 认的是同一份。
func pickSource(name string) (song.Source, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	src, ok := song.ByName(name)
	if !ok {
		return "", fmt.Errorf("音源只认 %s，%q 不在里面", strings.Join(song.Names(), " / "), name)
	}
	return src, nil
}

// friendlyReason 把平台的生硬报错换成能看懂的话。
func friendlyReason(err error) string {
	reason := err.Error()
	if strings.Contains(reason, "暂时不可用") {
		return fmt.Sprintf("音源临时抽风了, 重新发一次 /%s <歌名> 会自动换别的音源", commandName)
	}
	if strings.Contains(reason, "请求太密集") {
		return "刚刚点的人有点多, 等两秒再来"
	}
	return reason
}
