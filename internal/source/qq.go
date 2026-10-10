package source

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// QQ 音乐走官方接口 https://u.y.qq.com/cgi-bin/musicu.fcg:
//  1. music.search.SearchCgiService 搜歌, 拿 songmid;
//  2. fcg_play_single_song 按 songmid 补一个 media_mid(拼文件名要用, 不是 songmid);
//  3. vkey.GetVkeyServer 取直链, 拿到的 purl 拼到 ws.stream 域名上。
//
// 不带登录态也能搜、能放免费曲; 但付费曲/高音质要 y.qq.com 的 Cookie, 配上就用得上。
const (
	qqStreamBase = "https://ws.stream.qqmusic.qq.com/"
	qqUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/105.0.0.0 Safari/537.36"
)

// 接口地址是变量不是常量: 测试要把它指到本地假服务器。
var (
	qqMusicAPI  = "https://u.y.qq.com/cgi-bin/musicu.fcg"
	qqDetailAPI = "https://c.y.qq.com/v8/fcg-bin/fcg_play_single_song.fcg"
)

// 音质从高到低: 无登录态时高音质回空, 取第一个非空的 purl。文件名 = 档位前缀 + media_mid + 后缀。
var qqQualities = [][2]string{
	{"M800", "mp3"}, // 320k, 要 VIP
	{"M500", "mp3"}, // 128k, 免费曲就有
	{"C400", "m4a"}, // 96k aac, 兜底
}

// 官方接口的闸门: 稳定但要防自己人点太快。
var qqGate = guard.Options{
	Name: "QQ 音源", RPS: 5, Burst: 10, WaitBudget: 3 * time.Second,
	Failures: 5, Cooldown: 30 * time.Second,
}

type QQ struct {
	http   *requests.Client
	cookie func() string
	gate   *guard.Gate
}

// NewQQ 建 QQ 音源; cookie 可为 nil(未配置)或返回空串(按未登录处理)。
func NewQQ(http *requests.Client, cookie func() string) QQ {
	return QQ{http: http, cookie: cookie, gate: guard.NewGate(qqGate)}
}

func (QQ) Name() song.Source { return song.QQ }

func (q QQ) Search(keyword string, limit int) ([]song.Song, error) {
	if err := q.gate.Enter(); err != nil {
		return nil, err
	}
	body, err := q.post(qqMusicAPI, qqSearchBody(keyword, limit))
	if err != nil {
		q.gate.Done(false)
		return nil, err
	}
	songs, err := parseQQSearch(body)
	q.gate.Done(normal(err) == nil)
	return songs, err
}

// Audio 取直链要两次请求(补 media_mid + 换 vkey), 但只算一次配额。
func (q QQ) Audio(pick song.Song) (string, error) {
	if err := q.gate.Enter(); err != nil {
		return "", err
	}
	mediaMid, err := q.mediaMid(pick.ID)
	if err != nil {
		q.gate.Done(normal(err) == nil)
		return "", err
	}
	link, err := q.streamURL(pick.ID, mediaMid)
	q.gate.Done(normal(err) == nil)
	return link, err
}

// mediaMid 按 songmid 补 media_mid: GetVkey 的文件名要用它, 不是 songmid 本身。
func (q QQ) mediaMid(mid string) (string, error) {
	query := url.Values{
		"songmid":     {mid},
		"platform":    {"yqq.json"},
		"format":      {"json"},
		"inCharset":   {"utf8"},
		"outCharset":  {"utf-8"},
		"needNewCode": {"1"},
	}
	headers := q.headers()
	headers["Referer"] = "https://y.qq.com/"
	body, err := q.http.DoBytes("GET", qqDetailAPI+"?"+query.Encode(), nil, headers)
	if err != nil {
		return "", err
	}
	var resp qqDetailResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("qq 详情解析失败: %w", err)
	}
	if len(resp.Data) == 0 || strings.TrimSpace(resp.Data[0].File.MediaMid) == "" {
		return "", ErrNoResult
	}
	return resp.Data[0].File.MediaMid, nil
}

// streamURL 换 vkey, 按音质从高到低取第一个非空的 purl。
func (q QQ) streamURL(mid, mediaMid string) (string, error) {
	body, err := q.post(qqMusicAPI, qqVkeyBody(mid, mediaMid))
	if err != nil {
		return "", err
	}
	var resp qqVkeyResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("qq 直链解析失败: %w", err)
	}
	for _, info := range resp.Req0.Data.Midurlinfo {
		if purl := strings.TrimSpace(info.Purl); purl != "" {
			return qqStreamBase + purl, nil
		}
	}
	return "", ErrNoResult
}

func (q QQ) post(endpoint string, payload map[string]any) ([]byte, error) {
	return q.http.DoBytes("POST", endpoint, requests.JSON(payload), q.headers())
}

func (q QQ) headers() map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   qqUserAgent,
	}
	if cookie := q.cookieValue(); cookie != "" {
		headers["Cookie"] = cookie
	}
	return headers
}

func (q QQ) cookieValue() string {
	if q.cookie == nil {
		return ""
	}
	return strings.TrimSpace(q.cookie())
}

func qqSearchBody(keyword string, limit int) map[string]any {
	return map[string]any{
		"comm": map[string]any{"uin": "0", "authst": "", "ct": 29},
		"search": map[string]any{
			"method": "DoSearchForQQMusicMobile",
			"module": "music.search.SearchCgiService",
			"param": map[string]any{
				"grp":          1,
				"num_per_page": limit,
				"page_num":     1,
				"query":        keyword,
				"remoteplace":  "miniapp.1109523715",
				"search_type":  0,
				// searchid 每次必须换一个: 固定值会被上游当成重复请求返回空结果。
				"searchid": strconv.Itoa(rand.Intn(10000000)),
			},
		},
	}
}

func qqVkeyBody(mid, mediaMid string) map[string]any {
	songmid := make([]string, 0, len(qqQualities))
	filename := make([]string, 0, len(qqQualities))
	for _, quality := range qqQualities {
		songmid = append(songmid, mid)
		filename = append(filename, quality[0]+mediaMid+"."+quality[1])
	}
	return map[string]any{
		"comm": map[string]any{"uin": "0", "format": "json", "ct": 24, "cv": 0},
		"req_0": map[string]any{
			"module": "vkey.GetVkeyServer",
			"method": "CgiGetVkey",
			"param": map[string]any{
				"guid":     qqGUID(),
				"songmid":  songmid,
				"songtype": make([]int, len(qqQualities)),
				"uin":      "0",
				"ctx":      1,
				"filename": filename,
			},
		},
	}
}

func qqGUID() string {
	sum := md5.Sum([]byte(strconv.FormatInt(time.Now().UnixNano(), 10) + strconv.Itoa(rand.Int())))
	return hex.EncodeToString(sum[:])
}

type qqItem struct {
	Mid    string `json:"mid"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Singer []struct {
		Name string `json:"name"`
	} `json:"singer"`
	Interval int `json:"interval"`
}

type qqSearchResp struct {
	Search struct {
		Data struct {
			Body struct {
				ItemSong []qqItem `json:"item_song"`
				Song     struct {
					List []qqItem `json:"list"`
				} `json:"song"`
			} `json:"body"`
		} `json:"data"`
	} `json:"search"`
}

type qqDetailResp struct {
	Data []struct {
		File struct {
			MediaMid string `json:"media_mid"`
		} `json:"file"`
	} `json:"data"`
}

type qqVkeyResp struct {
	Req0 struct {
		Data struct {
			Midurlinfo []struct {
				Purl string `json:"purl"`
			} `json:"midurlinfo"`
		} `json:"data"`
	} `json:"req_0"`
}

func parseQQSearch(body []byte) ([]song.Song, error) {
	var resp qqSearchResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qq 响应解析失败: %w", err)
	}
	items := resp.Search.Data.Body.ItemSong
	if len(items) == 0 {
		items = resp.Search.Data.Body.Song.List
	}

	songs := make([]song.Song, 0, len(items))
	for _, item := range items {
		title := firstNonEmpty(qqCleanTitle(item.Title), item.Name)
		if item.Mid == "" || title == "" {
			continue
		}
		songs = append(songs, song.Song{
			Source:   song.QQ,
			ID:       item.Mid,
			Title:    title,
			Artist:   qqSingerName(item.Singer),
			Duration: item.Interval,
		})
	}
	return songs, nil
}

// 搜索结果的标题里带 <em> 高亮标签, 直接发出去会连标签一起显示。
func qqCleanTitle(title string) string {
	replacer := strings.NewReplacer("<em>", "", "</em>", "", "<EM>", "", "</EM>", "")
	return strings.TrimSpace(replacer.Replace(title))
}

func qqSingerName(singers []struct {
	Name string `json:"name"`
}) string {
	names := make([]string, 0, len(singers))
	for _, singer := range singers {
		if name := strings.TrimSpace(singer.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, "、")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
