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

// nianxinLevels 从高到低试, 见 channel.Ladder。
var nianxinLevels = []string{"hires", "lossless", "exhigh", "standard"}

var nianxinAPI = "http://mcp.nianxinxz.com/share/ceshi/tx.php"

func nianxinResolve(ctx context.Context, pick song.Song) (song.Song, error) {
	if err := ctx.Err(); err != nil {
		return song.Song{}, err
	}
	mid := strings.TrimSpace(pick.ID)
	if mid == "" {
		return song.Song{}, fmt.Errorf("QQ 音乐缺少 songmid")
	}
	return channel.Ladder(nianxinLevels, func(level string) (song.Song, error) {
		query := url.Values{"id": {mid}, "level": {level}, "type": {"mp3"}}
		link, err := netx.FinalURL(nianxinAPI+"?"+query.Encode(), nil)
		if err != nil {
			return song.Song{}, err
		}
		return channel.Finish(nianxinID, nianxinCN, pick, link)
	})
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
