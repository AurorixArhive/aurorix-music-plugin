// Package yunxi 走云汐 API 的 QQ 接口: 按歌名+歌手取直链, 会员曲也给。
package yunxi

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
	yunxiID = "qq.yunxi"
	yunxiCN = "云汐"
	// type=1 是中品质, 实测回 M800 320k mp3, 跟其它音源一个档。
	yunxiType = "1"
)

var (
	yunxiAPI    = "https://a.aa.cab/qq.music"
	yunxiClient = netx.New(15, map[string]string{"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"})
)

type yunxiBody struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Song   string `json:"song"`
		Singer string `json:"singer"`
		Music  string `json:"music"`
	} `json:"data"`
}

// yunxiResolve 只能按歌名取链 —— 云汐没有按 songmid 的接口(用 mid 拼出来的文件名是假的, 回 404)。
func yunxiResolve(ctx context.Context, pick song.Song) (song.Song, error) {
	if err := ctx.Err(); err != nil {
		return song.Song{}, err
	}
	title := strings.TrimSpace(pick.Title)
	if title == "" {
		return song.Song{}, fmt.Errorf("云汐要歌名才能取链")
	}
	keyword := title
	if artist := firstArtist(pick.Artist); artist != "" {
		keyword = title + " " + artist
	}
	query := url.Values{"msg": {keyword}, "n": {"1"}, "type": {yunxiType}}
	var body yunxiBody
	if err := yunxiClient.GetJSON(yunxiAPI+"?"+query.Encode(), &body, nil); err != nil {
		return song.Song{}, err
	}
	link := strings.TrimSpace(body.Data.Music)
	if body.Code != 0 || link == "" {
		return song.Song{}, fmt.Errorf("云汐没给直链: %s", body.Msg)
	}
	// 按歌名查就会撞上翻唱, 对不上宁可失败让别的渠道来。
	if !sameSong(title, pick.Artist, body.Data.Song, body.Data.Singer) {
		return song.Song{}, fmt.Errorf("云汐给回来的是 %s - %s, 不是想要的那首", body.Data.Song, body.Data.Singer)
	}
	return channel.Finish(yunxiID, yunxiCN, pick, link)
}

// sameSong 歌名必须一样, 歌手沾上一个就行 —— 上游的歌手串有 "A,B" 也有 "A、B"。
func sameSong(title, artist, gotTitle, gotSinger string) bool {
	if !strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(gotTitle)) {
		return false
	}
	artist = firstArtist(artist)
	if artist == "" {
		return true
	}
	return strings.Contains(strings.ReplaceAll(gotSinger, " ", ""), artist)
}

func firstArtist(artist string) string {
	parts := strings.FieldsFunc(artist, func(r rune) bool {
		return r == '、' || r == ',' || r == '，' || r == '/' || r == '&'
	})
	if len(parts) == 0 {
		return strings.TrimSpace(artist)
	}
	return strings.TrimSpace(parts[0])
}

func init() {
	channel.Register(channel.Channel{
		ID:       yunxiID,
		Platform: song.QQ,
		Label:    yunxiCN,
		Search:   qqsearch.Search,
		Resolve:  yunxiResolve,
	})
}
