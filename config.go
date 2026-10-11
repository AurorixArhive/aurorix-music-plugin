package music

import (
	"strings"

	"github.com/KasumiYuku/Aurorix/lib/plugin"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/schedule"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// settings 管不带 -s 时的音源顺序, 以及哪些音源先别用。
type settings struct {
	scheduler *schedule.Scheduler
}

func newSettings(scheduler *schedule.Scheduler) *settings {
	scheduler.SetPlatformOrder(song.All())
	return &settings{scheduler: scheduler}
}

func (s *settings) Apply(values map[string]any) error {
	disabled := map[song.Platform]bool{}
	for _, token := range split(text(values, "disabled_platforms")) {
		if platform, ok := song.ByName(token); ok {
			disabled[platform] = true
		}
	}

	order := make([]song.Platform, 0, len(song.All()))
	for _, token := range split(text(values, "platform_order")) {
		platform, ok := song.ByName(token)
		if !ok || disabled[platform] || contains(order, platform) {
			continue
		}
		order = append(order, platform)
	}
	// 没写到的按默认顺序补在后面, 免得漏一家就少一家。
	for _, platform := range song.All() {
		if !disabled[platform] && !contains(order, platform) {
			order = append(order, platform)
		}
	}

	s.scheduler.SetPlatformOrder(order)
	return nil
}

func (s *settings) configFields() []plugin.ConfigField {
	return []plugin.ConfigField{
		{
			Key:         "platform_order",
			Label:       "音源优先级",
			Description: "逗号分隔。不带 -s 时按这个顺序找，前一个音源没结果才试下一个。可用：qq / kugou / kuwo / wyy，写中文名也行；没写到的按默认顺序补在后面。",
			Type:        "text",
			Placeholder: "qq,kugou,kuwo,wyy",
		},
		{
			Key:         "disabled_platforms",
			Label:       "关闭的音源",
			Description: "逗号分隔。留空表示全开；关掉的音源不进默认顺序，但显式 -s 仍然能用。",
			Type:        "text",
			Placeholder: "",
		},
	}
}

func contains(list []song.Platform, want song.Platform) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func split(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '/' || r == '、'
	})
}

func text(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	value, _ := raw.(string)
	return value
}
