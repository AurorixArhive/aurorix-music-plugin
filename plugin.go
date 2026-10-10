package music

import (
	"sync/atomic"

	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/play"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/source"
)

const PluginID = "music"

type apiKey struct {
	value atomic.Pointer[string]
}

func (a *apiKey) Get() string {
	if v := a.value.Load(); v != nil {
		return *v
	}
	return ""
}

func (a *apiKey) Apply(values map[string]any) error {
	key, _ := values["qq_apikey"].(string)
	a.value.Store(&key)
	return nil
}

func init() {
	http := requests.Init(15)
	key := &apiKey{}
	feature := play.New(source.Chain{
		source.NewQQ(http, key.Get),
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
			Key:         "qq_apikey",
			Label:       "QQ 音源密钥",
			Description: "留空使用内置默认密钥，密钥失效时可在管理台换成自己的",
			Type:        "text",
			Placeholder: "sk-...",
		}},
		ApplyConfig: key.Apply,
	})
}
