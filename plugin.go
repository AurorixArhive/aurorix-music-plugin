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

// settings 把管理台的配置接到运行期: Cookie 更新登录态, source_order 重排音源优先级。
type settings struct {
	cookie atomic.Pointer[string]
	pool   *source.Pool
}

func (s *settings) cookieValue() string {
	if v := s.cookie.Load(); v != nil {
		return *v
	}
	return ""
}

func (s *settings) Apply(values map[string]any) error {
	if cookie, ok := values["qqmusic_cookie"].(string); ok {
		s.cookie.Store(&cookie)
	}
	if order, ok := values["source_order"].(string); ok && strings.TrimSpace(order) != "" {
		return s.pool.SetOrder(strings.Split(order, ","))
	}
	return nil
}

func init() {
	http := requests.Init(15)
	set := &settings{}
	// 默认优先级: 酷我 → 网易云 → QQ 音乐。
	// QQ 放最后: 免费曲之外都要登录才拿得到, 排在前面只会让它先把能搜的挡一轮。
	set.pool = source.NewPool(
		source.NewKuwo(http),
		source.NewNetease(http),
		source.NewQQ(http, set.cookieValue),
	)

	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "点歌",
		Description: "搜歌并发语音",
		Commands:    []*plugin.Command{play.New(set.pool).Command()},
		TemplateFS:  templateFS,
		Config: []plugin.ConfigField{
			{
				Key:         "source_order",
				Label:       "音源优先级",
				Description: "逗号分隔，前一家搜不到才试下一家。可用：kw 酷我 / wyy 网易云 / qq QQ 音乐（默认放最后，免费曲之外要登录）",
				Type:        "text",
				Placeholder: "kw,wyy,qq",
			},
			{
				Key:         "qqmusic_cookie",
				Label:       "QQ 音乐 Cookie",
				Description: "可选。留空只能搜到并播放免费曲目；想要付费曲与 320k 音质，登录 y.qq.com 后把完整 Cookie 填这里（含 uin / qqmusic_key / qm_keyst 等）",
				Type:        "password",
				Placeholder: "uin=...; qqmusic_key=...; qm_keyst=...",
			},
		},
		ApplyConfig: set.Apply,
	})
}
