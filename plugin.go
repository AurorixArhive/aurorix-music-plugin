package music

import (
	"strings"
	"sync/atomic"

	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/play"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/source"
)

const PluginID = "music"

// settings 把管理台的配置接到运行期: lyrics_enabled 管发不发歌词图, source_order 重排音源优先级。
// 都按"保存即生效"走, 不用重启。
type settings struct {
	lyrics atomic.Bool
	pool   *source.Pool
}

func (s *settings) lyricsOn() bool { return s.lyrics.Load() }

func (s *settings) Apply(values map[string]any) error {
	if on, ok := values["lyrics_enabled"].(bool); ok {
		s.lyrics.Store(on)
	}
	if order, ok := values["source_order"].(string); ok && strings.TrimSpace(order) != "" {
		return s.pool.SetOrder(strings.Split(order, ","))
	}
	return nil
}

func init() {
	http := requests.Init(15)
	set := &settings{}
	set.pool = source.NewPool(source.Defaults(http)...)
	set.lyrics.Store(true)

	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "点歌",
		Description: "搜歌, 短的发语音、长的发文件",
		Commands:    []*plugin.Command{play.New(set.pool, set.lyricsOn).Command()},
		TemplateFS:  templateFS,
		Config: []plugin.ConfigField{
			{
				Key:         "source_order",
				Label:       "音源优先级",
				Description: "逗号分隔，前一家搜不到才试下一家。可用：qq QQ音乐 / wyy 网易云 / baidu 百度 / 1ting 一听",
				Type:        "text",
				Placeholder: "qq,wyy,baidu,1ting",
			},
			{
				Key:         "lyrics_enabled",
				Label:       "发送歌词图",
				Description: "音源带歌词时，在歌曲后面补一张歌词图。默认开启。",
				Type:        "boolean",
			},
		},
		ApplyConfig: set.Apply,
	})
}
