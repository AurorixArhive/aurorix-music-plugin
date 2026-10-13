package channel

import (
	"fmt"

	"github.com/KasumiYuku/Aurorix/lib/logx"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

var log = logx.New("channel")

// Finish 是渠道拿到直链后的公共收尾: 探一次, 填上时长和可用等级。
// 降级结果(加密文件这类)不算失败: 照常交出去, 用不用由调度决定。
func Finish(id, label string, pick song.Song, link string) (song.Song, error) {
	audio, err := netx.Inspect(link)
	if err != nil {
		return song.Song{}, fmt.Errorf("%s直链探测失败: %w", label, err)
	}
	if audio.Grade == quality.Unusable {
		return song.Song{}, fmt.Errorf("%s直链不可用(%s): %s", label, audio.Why, link)
	}
	if audio.Grade < quality.Usable {
		log.Warnf("%s 这条链是%s(%s), 先留着当兜底", label, audio.Grade, audio.Why)
	}
	pick.Audio = link
	pick.Grade = audio.Grade
	pick.Channel = id
	if audio.Seconds > 0 {
		pick.Duration = audio.Seconds
	}
	return pick, nil
}
