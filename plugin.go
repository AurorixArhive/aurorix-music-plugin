package music

import (
	"sync/atomic"

	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/play"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/source"
)

const PluginID = "music"

// 管理台填的 QQ 音乐 Cookie: 用来解锁付费曲与更高音质, 没填也能放免费曲。
// 做成按需读取而不是快照 —— 登录态刷新后重填一次即生效。
type qqCookie struct {
	value atomic.Pointer[string]
}

func (c *qqCookie) Get() string {
	if v := c.value.Load(); v != nil {
		return *v
	}
	return ""
}

func (c *qqCookie) Apply(values map[string]any) error {
	cookie, _ := values["qqmusic_cookie"].(string)
	c.value.Store(&cookie)
	return nil
}

func init() {
	http := requests.Init(15)
	cookie := &qqCookie{}
	feature := play.New(source.Chain{
		source.NewQQ(http, cookie.Get),
		source.NewKuwo(http),
		source.NewNetease(http),
	})

	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "点歌",
		Description: "搜歌并发语音",
		Commands:    []*plugin.Command{feature.Command()},
		TemplateFS:  templateFS,
		Config: []plugin.ConfigField{{
			Key:         "qqmusic_cookie",
			Label:       "QQ 音乐 Cookie",
			Description: "可选。留空只能搜到并播放免费曲目；想要付费曲与 320k 音质，登录 y.qq.com 后把完整 Cookie 填这里（含 uin / qqmusic_key / qm_keyst 等）",
			Type:        "password",
			Placeholder: "uin=...; qqmusic_key=...; qm_keyst=...",
		}},
		ApplyConfig: cookie.Apply,
	})
}
