package molan

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

// 这两个地址测试里会指到假的服务器上, 所以写成变量。
var (
	kugouSearchURL = "https://songsearch.kugou.com/song_search_v2?page=1"
	// wwwapi 的 play/getdata 现在恒回 err_code 30020, 换成官方移动端接口; 它同样按 hash 取直链。
	kugouInfoURL = "https://m.kugou.com/app/i/getSongInfo.php?cmd=playInfo"
)

var kugouClient = netx.New(12, map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36",
	"Referer":    "https://www.kugou.com/",
})

type kugouHit struct {
	FileHash   string `json:"FileHash"`
	AlbumID    string `json:"AlbumID"`
	SongName   string `json:"SongName"`
	SingerName string `json:"SingerName"`
	Duration   int    `json:"Duration"`
}

type kugouSearchBody struct {
	Data struct {
		Lists []kugouHit `json:"lists"`
	} `json:"data"`
}

type kugouInfoBody struct {
	URL        string  `json:"url"`
	BackupURL  strList `json:"backup_url"`
	TimeLength int     `json:"timeLength"`
	ErrCode    int     `json:"errcode"`
}

// strList 兼容上游把同一个字段时而给字符串时而给数组。
type strList []string

func (l *strList) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*l = strList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

func (l strList) first() string {
	if len(l) == 0 {
		return ""
	}
	return l[0]
}

func kugouSearch(ctx context.Context, keyword string, limit int) ([]song.Song, error) {
	if limit <= 0 {
		limit = 10
	}
	rawURL := kugouSearchURL + "&keyword=" + url.QueryEscape(keyword) + "&pagesize=" + strconv.Itoa(limit)
	raw, err := kugouClient.Get(rawURL, nil)
	if err != nil {
		return nil, err
	}
	var body kugouSearchBody
	if err := netx.Decode(raw, &body); err != nil {
		return nil, err
	}
	out := make([]song.Song, 0, len(body.Data.Lists))
	for _, item := range body.Data.Lists {
		if item.FileHash == "" {
			continue
		}
		// album_id 是取直链的必填参数, 连同 hash 一起塞进 id, Resolve 再拆开。
		out = append(out, song.Song{
			Platform: song.Kugou,
			ID:       item.FileHash + "|" + item.AlbumID,
			Title:    plain(item.SongName),
			Artist:   plain(item.SingerName),
			Duration: item.Duration,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("酷狗搜索无结果: %s", keyword)
	}
	return out, nil
}

func kugouResolve(ctx context.Context, pick song.Song) (song.Song, error) {
	hash, albumID, _ := strings.Cut(pick.ID, "|")
	if hash == "" {
		return song.Song{}, fmt.Errorf("酷狗缺少歌曲 hash")
	}

	// 先走墨澜那条聚合接口(海棠湾), 拿不到再问官方接口。
	last := error(nil)
	if link, err := haitangwKG(hash); err != nil {
		last = err
	} else if got, err := channel.Finish("kg.molan", "墨澜", pick, link); err != nil {
		last = err
	} else {
		return got, nil
	}

	rawURL := kugouInfoURL + "&hash=" + url.QueryEscape(hash)
	if albumID != "" {
		rawURL += "&album_id=" + url.QueryEscape(albumID)
	}
	raw, err := kugouClient.Get(rawURL, nil)
	if err != nil {
		return song.Song{}, err
	}
	var body kugouInfoBody
	if err := netx.Decode(raw, &body); err != nil {
		return song.Song{}, err
	}
	link := body.URL
	if link == "" {
		link = body.BackupURL.first()
	}
	if link == "" {
		return song.Song{}, fmt.Errorf("酷狗没给直链: hash=%s errcode=%d; 海棠湾那边: %v", hash, body.ErrCode, last)
	}
	return channel.Finish("kg.molan", "墨澜", pick, link)
}

var haitangwResolveAPI = "https://musicserver.haitangw.cc/v1/music/resolve-url"

// haitangwKG 是墨澜走的那条聚合接口: source=kg + rid(文件 hash)。
func haitangwKG(hash string) (string, error) {
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	payload := map[string]any{"source": "kg", "rid": hash, "level": "exhigh"}
	if err := kugouClient.PostJSONDecode(haitangwResolveAPI, payload, &body, map[string]string{"User-Agent": "Mozilla/5.0"}); err != nil {
		return "", err
	}
	if body.Data.URL == "" {
		return "", fmt.Errorf("海棠湾酷狗没给直链: %s", body.Message)
	}
	return body.Data.URL, nil
}

func init() {
	channel.Register(channel.Channel{
		ID:       "kg.molan",
		Platform: song.Kugou,
		Label:    "墨澜",
		Search:   kugouSearch,
		Resolve:  kugouResolve,
	})
}

// plain 去掉搜索结果里的 HTML 实体, 它们是上游给高亮用的。
func plain(text string) string {
	replacer := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return strings.TrimSpace(replacer.Replace(text))
}
