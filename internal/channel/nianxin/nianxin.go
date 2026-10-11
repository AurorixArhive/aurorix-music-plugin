// Package nianxin 走念心的模板接口, 按 songmid 拿 QQ 会员曲(它会 302 到真正的音频)。
package nianxin

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/qqsearch"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const (
	nianxinID = "qq.nianxin"
	nianxinCN = "念心"
)

var nianxinAPI = "http://mcp.nianxinxz.com/share/ceshi/tx.php"

func nianxinResolve(ctx context.Context, pick song.Song) (song.Song, error) {
	if err := ctx.Err(); err != nil {
		return song.Song{}, err
	}
	mid := strings.TrimSpace(pick.ID)
	if mid == "" {
		return song.Song{}, fmt.Errorf("QQ 音乐缺少 songmid")
	}
	query := url.Values{"id": {mid}, "level": {"exhigh"}, "type": {"mp3"}}
	link, err := netx.FinalURL(nianxinAPI+"?"+query.Encode(), nil)
	if err != nil {
		return song.Song{}, err
	}
	return finish(pick, link)
}

// finish 探一次直链: 确认能播, 顺手把时长补上。
func finish(pick song.Song, link string) (song.Song, error) {
	audio, err := netx.Inspect(link)
	if err != nil {
		return song.Song{}, fmt.Errorf("念心直链探测失败: %w", err)
	}
	if !audio.Playable {
		return song.Song{}, fmt.Errorf("念心直链不可播: %s", link)
	}
	pick.Audio = link
	if audio.Seconds > 0 {
		pick.Duration = audio.Seconds
	}
	pick.Channel = nianxinID
	return pick, nil
}

func init() {
	channel.Register(channel.Channel{
		ID:       nianxinID,
		Platform: song.QQ,
		Label:    nianxinCN,
		Search:   qqsearch.Search,
		Resolve:  nianxinResolve,
	})
}
