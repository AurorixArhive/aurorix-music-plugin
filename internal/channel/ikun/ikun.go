// Package ikun 网易云: 搜索走网易云官方接口, 直链走 IKUN(c.wwwweb.top)。
package ikun

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const Label = "IKUN"

// 这三个地址测试里会指到假的服务器上, 所以写成变量。
var (
	wySearchURL = "https://music.163.com/api/search/get/web"
	ikunURL     = "https://c.wwwweb.top/music/url"
	// 320 拿不到就退 128; IKUN 也认 flac, 但 mp3 上传快、够用。
	ikunBR = []string{"320k", "128k"}
)

var client = netx.New(15, map[string]string{"User-Agent": "lx-music-request/2.9.0"})

func init() {
	channel.Register(channel.Channel{
		ID:       "wyy.ikun",
		Platform: song.Wyy,
		Label:    Label,
		Search:   wySearch,
		Resolve:  wyResolve,
	})
}

type wySong struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Duration int    `json:"duration"`
	Artists  []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

type wySearchBody struct {
	Result struct {
		Songs []wySong `json:"songs"`
	} `json:"result"`
}

func wySearch(ctx context.Context, keyword string, limit int) ([]song.Song, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	query := url.Values{}
	query.Set("s", keyword)
	query.Set("type", "1")
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", "0")

	var body wySearchBody
	headers := map[string]string{"Referer": "https://music.163.com/"}
	if err := client.GetJSON(wySearchURL+"?"+query.Encode(), &body, headers); err != nil {
		return nil, fmt.Errorf("%s 网易云搜索: %w", Label, err)
	}

	out := make([]song.Song, 0, len(body.Result.Songs))
	for _, item := range body.Result.Songs {
		title := strings.TrimSpace(item.Name)
		if item.ID == 0 || title == "" {
			continue
		}
		names := make([]string, 0, len(item.Artists))
		for _, artist := range item.Artists {
			if name := strings.TrimSpace(artist.Name); name != "" {
				names = append(names, name)
			}
		}
		out = append(out, song.Song{
			Platform: song.Wyy,
			ID:       strconv.FormatInt(item.ID, 10),
			Title:    title,
			Artist:   strings.Join(names, "/"),
			Duration: item.Duration / 1000, // 官方给的是毫秒
			Channel:  "wyy.ikun",
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 网易云没搜到 %q", Label, keyword)
	}
	return out, nil
}

type ikunBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	URL     string `json:"url"`
}

func wyResolve(ctx context.Context, pick song.Song) (song.Song, error) {
	if err := ctx.Err(); err != nil {
		return song.Song{}, err
	}
	id := strings.TrimSpace(pick.ID)
	if id == "" {
		return song.Song{}, fmt.Errorf("%s 网易云缺少歌曲 id", Label)
	}

	last := fmt.Errorf("%s 没给出直链", Label)
	for _, br := range ikunBR {
		payload := map[string]any{"source": "wy", "musicId": id, "quality": br}
		var body ikunBody
		headers := map[string]string{"Content-Type": "application/json", "X-Api-Key": ""}
		if err := client.PostJSONDecode(ikunURL, payload, &body, headers); err != nil {
			last = err
			continue
		}
		if strings.TrimSpace(body.URL) == "" {
			last = fmt.Errorf("%s 没给出直链: %s", Label, strings.TrimSpace(body.Message))
			continue
		}
		got, err := channel.Finish("wyy.ikun", Label, pick, body.URL)
		if err == nil {
			return got, nil
		}
		last = err
	}
	return song.Song{}, last
}
