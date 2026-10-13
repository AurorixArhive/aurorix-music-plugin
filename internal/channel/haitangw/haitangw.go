// Package haitangw 走海棠湾的解析接口, 按 songmid 拿 QQ 会员曲。
package haitangw

import (
	"context"
	"fmt"
	"strings"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/qqsearch"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const (
	haitangwID = "qq.haitangw"
	haitangwCN = "海棠湾"
)

// haitangwLevels 从高到低试; 拿不到高音质时上游会回退成加密文件, 能不能用由探测决定。
var haitangwLevels = []string{"hires", "lossless", "exhigh", "standard"}

var (
	haitangwAPI    = "https://musicserver.haitangw.cc/v1/music/resolve-url"
	haitangwClient = netx.New(20, map[string]string{"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"})
)

// code 有时是字符串("UPSTREAM_NO_RESOURCE"), 所以不接这个字段, 只看 data.url。
type haitangwBody struct {
	Message string `json:"message"`
	Data    struct {
		URL string `json:"url"`
	} `json:"data"`
}

func haitangwResolve(ctx context.Context, pick song.Song) (song.Song, error) {
	if err := ctx.Err(); err != nil {
		return song.Song{}, err
	}
	mid := strings.TrimSpace(pick.ID)
	if mid == "" {
		return song.Song{}, fmt.Errorf("QQ 音乐缺少 songmid")
	}
	return channel.Ladder(haitangwLevels, func(level string) (song.Song, error) {
		payload := map[string]any{"source": "tx", "rid": mid, "level": level}
		var body haitangwBody
		if err := haitangwClient.PostJSONDecode(haitangwAPI, payload, &body, nil); err != nil {
			return song.Song{}, err
		}
		link := strings.TrimSpace(body.Data.URL)
		if link == "" {
			return song.Song{}, fmt.Errorf("海棠湾没给直链: %s", body.Message)
		}
		return channel.Finish(haitangwID, haitangwCN, pick, link)
	})
}

func init() {
	channel.Register(channel.Channel{
		ID:       haitangwID,
		Platform: song.QQ,
		Label:    haitangwCN,
		Search:   qqsearch.Search,
		Resolve:  haitangwResolve,
	})
}
