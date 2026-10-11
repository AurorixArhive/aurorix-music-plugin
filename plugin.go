// Package music 点歌: 按音源搜歌, 短的发语音、长的发文件。
package music

import (
	"context"

	"github.com/KasumiYuku/Aurorix/lib/plugin"

	_ "github.com/AurorixArhive/aurorix-music-plugin/internal/channel/all"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/play"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/schedule"
)

const PluginID = "music"

func init() {
	scheduler := schedule.New()
	cfg := newSettings(scheduler)
	scheduler.StartProbe(context.Background())

	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "点歌",
		Description: "搜歌, 短的发语音、长的发文件",
		Commands:    []*plugin.Command{play.New(scheduler).Command()},
		TemplateFS:  templateFS,
		Config:      cfg.configFields(),
		ApplyConfig: cfg.Apply,
	})
}
