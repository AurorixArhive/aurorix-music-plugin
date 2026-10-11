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

	"github.com/AurorixArhive/aurorix-music-plugin/internal/schedule"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const (
	commandName  = "play"
	maxResults   = 10
	voiceSeconds = 5 * 60
	resolveLimit = 25 * time.Second
	// fileBytes 是先下载再上传的上限: 超过就退回直传, 不把内存吃满。
	fileBytes = 64 << 20
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

	if pick, ok := song.ParseRef(keyword); ok {
		return p.deliver(ctx, pick, "")
	}

	target, err := pickSource(args.Source)
	if err != nil {
		return err
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
		return refFailure(err)
	}
	if err := p.sendAudio(ctx, found); err != nil {
		return err
	}
	return nil
}

func (p *Play) sendAudio(ctx *botcontext.MessageContext, found song.Song) error {
	if found.Duration <= voiceSeconds {
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

func (p *Play) onError(ctx *botcontext.MessageContext, err error) error {
	return p.render(ctx, "fail", &templates.Args{"reason": friendlyReason(err)})
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

func refFailure(err error) error {
	if errors.Is(err, schedule.ErrAllChannels) {
		return errors.New("这个音源的渠道现在都不通, 换一个音源试试(加 -s)")
	}
	return err
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

func audioExt(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	switch ext := strings.ToLower(path.Ext(rawURL)); ext {
	case ".mp3", ".m4a", ".flac", ".wav", ".aac", ".ogg", ".ape":
		return ext
	}
	return ".mp3"
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

func friendlyReason(err error) string {
	reason := err.Error()
	if strings.Contains(reason, "暂时不可用") {
		return fmt.Sprintf("音源临时抽风了, 重新发一次 /%s <歌名> 会自动换别的渠道", commandName)
	}
	if strings.Contains(reason, "请求太密集") {
		return "刚刚点的人有点多, 等两秒再来"
	}
	return reason
}
