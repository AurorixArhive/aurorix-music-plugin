// Package xinghai 星海音乐源的酷我链: 走酷我官方直连。
package xinghai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const Label = "星海"

// 这两个地址测试里会指到假的服务器上, 所以写成变量。
var (
	kuwoSearchURL = "https://search.kuwo.cn/r.s?client=kt&ft=music&rformat=json&encoding=utf8&pn=0&vipver=1"
	// 320 拿不到就退 128
	kuwoBR = []string{"320kmp3", "128kmp3"}
)

// kuwoShells 是酷我几个客户端的壳; 一个壳不给链就换下一个, 和星海那边的做法一致。
var kuwoShells = []struct{ base, source string }{
	{"https://mobi.kuwo.cn", "kwplayer_ar_8.5.5.0_apk_keluze.apk"},
	{"https://nmobi.kuwo.cn", "kwplayercar_ar_6.1.0.0_B_jiakong_vh.apk"},
	{"https://nmobi.kuwo.cn", "kwplayercar_ar_6.0.0.5_B_jiakong_vh.apk"},
}

// 酷我对 UA 与 Referer 都挑, 少了会给个加密空壳。
var kuwoClient = netx.New(15, map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36",
	"Referer":    "https://www.kuwo.cn/",
})

func init() {
	channel.Register(channel.Channel{
		ID:       "kuwo.xinghai",
		Platform: song.Kuwo,
		Label:    Label,
		Search:   kuwoSearch,
		Resolve:  kuwoResolve,
	})
}

type kuwoHit struct {
	Songname string `json:"SONGNAME"`
	Artist   string `json:"ARTIST"`
	Musicrid string `json:"MUSICRID"`
	Duration string `json:"DURATION"`
}

type kuwoSearchBody struct {
	Abslist []kuwoHit `json:"abslist"`
}

func kuwoSearch(ctx context.Context, keyword string, limit int) ([]song.Song, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	rawURL := kuwoSearchURL + "&all=" + url.QueryEscape(keyword) + "&rn=" + strconv.Itoa(limit)
	raw, err := kuwoClient.Get(rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%s 酷我搜索: %w", Label, err)
	}

	// 酷我回的是单引号包键值的伪 JSON, 先整体换成双引号再解。
	var body kuwoSearchBody
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "'", `"`)), &body); err != nil {
		return nil, fmt.Errorf("%s 酷我搜索响应解析失败: %w", Label, err)
	}

	out := make([]song.Song, 0, len(body.Abslist))
	for _, item := range body.Abslist {
		id := strings.TrimPrefix(strings.TrimSpace(item.Musicrid), "MUSIC_")
		if id == "" {
			continue
		}
		seconds, _ := strconv.Atoi(strings.TrimSpace(item.Duration))
		out = append(out, song.Song{
			Platform: song.Kuwo,
			ID:       id,
			Title:    plain(item.Songname),
			Artist:   plain(item.Artist),
			Duration: seconds,
			Channel:  "kuwo.xinghai",
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 酷我没搜到 %q", Label, keyword)
	}
	return out, nil
}

type kuwoPlayBody struct {
	URL  string `json:"url"`
	Data struct {
		URL  string `json:"url"`
		Surl string `json:"surl"`
	} `json:"data"`
}

func kuwoResolve(ctx context.Context, pick song.Song) (song.Song, error) {
	if err := ctx.Err(); err != nil {
		return song.Song{}, err
	}
	id := strings.TrimSpace(pick.ID)
	if id == "" {
		return song.Song{}, fmt.Errorf("%s 酷我缺少歌曲 id", Label)
	}

	last := fmt.Errorf("%s 酷我未返回直链", Label)
	for _, shell := range kuwoShells {
		for _, br := range kuwoBR {
			query := url.Values{}
			query.Set("user", "0")
			query.Set("source", shell.source)
			query.Set("type", "convert_url_with_sign")
			query.Set("br", br)
			query.Set("format", "mp3")
			query.Set("rid", id)

			raw, err := kuwoClient.Get(shell.base+"/mobi.s?"+query.Encode(), nil)
			if err != nil {
				last = err
				continue
			}
			var body kuwoPlayBody
			if err := json.Unmarshal(raw, &body); err != nil {
				last = fmt.Errorf("%s 酷我直链响应解析失败: %w", Label, err)
				continue
			}
			link := firstOf(body.Data.URL, body.Data.Surl, body.URL)
			if link == "" {
				continue
			}
			got, err := channel.Finish("kuwo.xinghai", Label, pick, link)
			if err == nil {
				return got, nil
			}
			last = err
		}
	}
	return song.Song{}, last
}

// plain 去掉搜索结果里的 HTML 实体, 它们是上游给高亮用的。
func plain(text string) string {
	replacer := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return strings.TrimSpace(replacer.Replace(text))
}

func firstOf(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
