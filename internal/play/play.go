package play

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/constant"
	botcontext "github.com/KasumiYuku/Aurorix/lib/context"
	"github.com/KasumiYuku/Aurorix/lib/logx"
	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/templates"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/meta"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/schedule"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const (
	commandName  = "点歌"
	maxResults   = 10
	voiceSeconds = 5 * 60
	resolveLimit = 25 * time.Second
	// fileBytes 是先下载再上传的上限: 超过就退回直传, 不把内存吃满。
	fileBytes = 64 << 20
	// templateSongCard 是发音频前先发的那张歌曲卡(见 templates/markdown/songcard.md)。
	templateSongCard = "songcard"
)

var log = logx.New("music")

type Play struct {
	scheduler *schedule.Scheduler
}

func New(scheduler *schedule.Scheduler) *Play { return &Play{scheduler: scheduler} }

type playArgs struct {
	Source string   `kong:"name='source',short='s',help='音源: qq/wyy/kuwo/kugou/migu/qishui, 可以写中文名, 不填自动挑'"`
	Words  []string `kong:"arg,optional,name='query',help='歌名, 或卡片上的短码'"`
}

func (p *Play) Command() *plugin.Command {
	return &plugin.Command{
		Prefix:      commandName,
		Aliases:     []string{"play", "点播", "music"},
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

	if pick, ok := song.ParseRef(keyword); ok {
		return p.deliver(ctx, pick, "")
	}

	target, err := pickSource(args.Source)
	if err != nil {
		// -s 写错了是用户自己的输入问题, 这句得让他看懂; 别的失败一律回一句通用的。
		return p.render(ctx, "fail", &templates.Args{"reason": err.Error()})
	}
	return p.search(ctx, keyword, target)
}

type target struct {
	platform  song.Platform
	channelID string
}

func (p *Play) search(ctx *botcontext.MessageContext, keyword string, want target) error {
	callCtx, cancel := context.WithTimeout(context.Background(), resolveLimit)
	defer cancel()

	songs, err := p.scheduler.Search(callCtx, want.platform, want.channelID, keyword, maxResults)
	if err != nil {
		return p.render(ctx, "fail", &templates.Args{"reason": emptyReason(keyword, want, err)})
	}
	return p.render(ctx, "search", &templates.Args{
		"keyword": keyword,
		"total":   strconv.Itoa(len(songs)),
		"songs":   searchRows(songs),
	})
}

func (p *Play) deliver(ctx *botcontext.MessageContext, pick song.Song, channelID string) error {
	callCtx, cancel := context.WithTimeout(context.Background(), resolveLimit)
	defer cancel()

	found, err := p.scheduler.Resolve(callCtx, pick, channelID)
	if err != nil {
		return err
	}
	// 短码只带得回平台和 ID(见 song/codec.go) —— 点下来的这一刻, 标题/作者/封面已经不在手里了。
	// 发卡前按 ID 问一次平台把它们补回来(见 internal/meta); 问不到就原样返回, 卡片会自己跳过。
	found = meta.Fill(callCtx, found)

	// 卡片先发: 用户在音频/文件到达之前就看到了标题、作者和封面, 也拿到了封面。
	// 卡片发失败不算失败 —— 音频才是主角, 不能因为一张卡片就放弃整首歌。
	if err := p.sendCard(ctx, found); err != nil {
		log.Warnf("发卡片失败(不影响音频): %v", err)
	}
	if err := p.sendAudio(ctx, found); err != nil {
		return err
	}
	return nil
}

// sendCard 发那张 id=2 的卡片: 封面 + 标题 + 作者, 点开跳到歌曲页。
//
// 卡片由 templates/markdown/songcard.md 描述而不是在 Go 里拼字符串: 参数编码、
// 可选参数的省略都由模板那层负责, 这里只给值 —— 标题里带 & 或空格也不会把卡片弄坏。
// 空值传空串即可, 模板会把整个参数省掉(空参数和缺参数在客户端是两种结果)。
func (p *Play) sendCard(ctx *botcontext.MessageContext, found song.Song) error {
	if !cardWorthSending(found) {
		return nil
	}
	card, err := ctx.MarkdownTemplate(templateSongCard, &templates.Args{
		"cover":  found.Cover,
		"link":   found.WebURL(),
		"title":  found.Title,
		"artist": found.Artist,
	})
	if err != nil {
		return err
	}
	// 这张卡自己就是一条完整消息, 底下再压一行底部按钮等于把它切成两块 —— 这一条不要。
	// 音频跟在后面发, 是媒体消息, 本来就轮不到底部按钮。
	card.NoFooter = true
	return card.Send()
}

// cardWorthSending 卡片上至少得有一行标题或一张封面才值得发。
// 一个参数都凑不出来的卡, 在客户端就是一个空壳 —— 那还不如不发(见 internal/meta)。
func cardWorthSending(s song.Song) bool {
	return strings.TrimSpace(s.Title) != "" || strings.TrimSpace(s.Cover) != ""
}

func (p *Play) sendAudio(ctx *botcontext.MessageContext, found song.Song) error {
	// 只有正经音频才值得试语音: 降级的(加密格式这类)QQ 根本收不了, 时长读不出来的也别赌。
	if found.Grade >= quality.Usable && found.Duration > 0 && found.Duration <= voiceSeconds {
		if err := ctx.Voice(found.Audio).Send(); err == nil {
			return nil
		}
		// QQ 的语音只收 silk/mp3/wav, 拿到 m4a 这类会失败 —— 退回发文件
	}
	return p.sendFile(ctx, found)
}

// sendFile 先抓下来再按字节上传: 走 URL 直传时 QQ 会无视 file_name, 拿整条 URL 当文件名
// (query 也一起塞进去), 群里收到的就成了 "C400....m4a_guid=...&vkey=...", 客户端认不出是音频。
func (p *Play) sendFile(ctx *botcontext.MessageContext, found song.Song) error {
	raw, err := download(found.Audio, fileBytes)
	if err != nil {
		log.Warnf("下载 %s 失败, 退回直传: %v", found.Label(), err)
		return ctx.File(found.Audio, fileName(found)).Send()
	}
	return ctx.File(raw, fileName(found)).Send()
}

// download 抓一份音频; 超过 limit 直接判错。
func download(rawURL string, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("上游回了 %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("音频超过 %dMB", limit>>20)
	}
	return raw, nil
}

func (p *Play) render(ctx *botcontext.MessageContext, id string, args *templates.Args) error {
	msg, err := ctx.MarkdownTemplate(id, args)
	if err != nil {
		return err
	}
	return msg.Send()
}

// onError 给用户只留一句人话, 细节进日志 —— 上游报错里带着直链和接口名。
func (p *Play) onError(ctx *botcontext.MessageContext, err error) error {
	return p.render(ctx, "fail", &templates.Args{"reason": failReason(err)})
}

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

func emptyReason(keyword string, want target, err error) string {
	if errors.Is(err, schedule.ErrAllChannels) {
		if want.platform != "" {
			return fmt.Sprintf("%s的渠道现在都不通，换一个音源试试", song.CN(want.platform))
		}
		return "所有渠道现在都不通，稍后再试"
	}
	if want.platform != "" {
		return fmt.Sprintf("%s里没有找到「%s」，换个关键词，或者去掉 -s 让我们自动挑音源", song.CN(want.platform), keyword)
	}
	return fmt.Sprintf("没有找到「%s」相关歌曲，换个关键词试试", keyword)
}

func fileName(s song.Song) string {
	illegal := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	name := strings.TrimSpace(illegal.Replace(s.Label()))
	if name == "" {
		name = "music"
	}
	return name + audioExt(s.Audio)
}

// audioExt 后缀按直链的真实格式来: 一律写 .mp3 会让 m4a/flac 被播放器认错,
// 用户那边看到的就是"一个打不开的 mp3"。
func audioExt(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	ext := strings.ToLower(path.Ext(rawURL))
	if len(ext) >= 2 && len(ext) <= 6 && isExtChars(ext[1:]) {
		return ext
	}
	return ".mp3"
}

func isExtChars(ext string) bool {
	for _, r := range ext {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func parsed(ctx *botcontext.MessageContext) *playArgs {
	if args, ok := ctx.Parsed.(*playArgs); ok {
		return args
	}
	return &playArgs{}
}

// pickSource 把 -s 的值翻成目标音源。只认音源这一层: 渠道由调度自己挑。
// 写错时给一个"你是不是想说"的建议 —— 认不出的名字直接报错比静默降级清楚。
func pickSource(name string) (target, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return target{}, nil
	}

	platformPart, channelID, hasChannel := strings.Cut(name, ".")
	if platform, ok := song.ByName(platformPart); ok {
		return target{platform: platform, channelID: channelID}, nil
	}
	if platform, ok := song.ByName(name); ok {
		return target{platform: platform}, nil
	}

	hint := ""
	if suggest := song.Suggest(name); suggest != "" {
		hint = fmt.Sprintf("，你是想说 %s 吗", song.CN(suggest))
	}
	_ = hasChannel
	return target{}, fmt.Errorf("音源只认 %s%s，%q 不在里面", strings.Join(platformNames(), " / "), hint, name)
}

func platformNames() []string {
	platforms := song.All()
	out := make([]string, 0, len(platforms))
	for _, p := range platforms {
		out = append(out, string(p))
	}
	return out
}

// friendlyReason 把内部报错翻成人话: 上游的报错里带着直链、接口名和 traceId,
// 那些只该进日志 —— 群里甩出一串 vkey, 用户只会以为机器人坏了。
// failReason 拿不到歌时给用户的唯一一句话 —— 上游的直链、接口名和 traceId 只该进日志,
// 甩到群里用户只会以为机器人坏了。具体哪一步坏了看上面那行 Warn。
func failReason(err error) string {
	log.Warnf("点歌失败: %v", err)
	return "获取歌曲失败，换一首或换个关键词重试"
}
