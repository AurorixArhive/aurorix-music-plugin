package source

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 走网易自己的三个公开端点, 不碰第三方中转:
//
//	api/search/get/web         搜索, 响应里直接带时长(毫秒)
//	api/song/detail            按 id 取歌名/歌手
//	api/song/lyric             歌词
//	song/media/outer/url       直链, 302 跳到真文件
//
// 好处是不用登录、不用签名密钥, 而且省掉了"时长另外算"这一步。
// 代价是付费与部分版权受限的曲目取不到 —— 实测冷门/翻唱能放, 周杰伦这种版权大户放不了。
const (
	neteaseSearchAPI = "http://music.163.com/api/search/get/web"
	neteaseDetailAPI = "http://music.163.com/api/song/detail"
	neteaseLyricAPI  = "http://music.163.com/api/song/lyric"
	neteaseOuterURL  = "http://music.163.com/song/media/outer/url"
)

var neteaseHeaders = map[string]string{
	"Referer":    "http://music.163.com/",
	"Cookie":     "appver=2.0.2",
	"User-Agent": probeUA,
}

var neteaseGate = guard.Options{
	Name: "网易音源", RPS: 6, Burst: 12, WaitBudget: 3 * time.Second,
	Failures: 8, Cooldown: 20 * time.Second,
}

type netease struct {
	http *requests.Client
	gate *guard.Gate
}

func NewNetease(http *requests.Client) Source {
	return &netease{http: http, gate: guard.NewGate(neteaseGate)}
}

func (*netease) Name() song.Source { return song.Wyy }

func (n *netease) Search(keyword string, limit int) ([]song.Song, error) {
	if err := n.gate.Enter(); err != nil {
		return nil, err
	}
	form := url.Values{
		"s":      {keyword},
		"limit":  {strconv.Itoa(limit)},
		"type":   {"1"},
		"offset": {"0"},
	}
	body, err := n.http.DoBytes("POST", neteaseSearchAPI, requests.Form(form), neteaseHeaders)
	if err != nil {
		n.gate.Done(false)
		return nil, err
	}
	songs, err := parseNeteaseSearch(body, limit)
	n.gate.Done(normal(err) == nil)
	return songs, err
}

func (n *netease) Detail(id string) (song.Song, error) {
	audio := neteaseAudioURL(id)
	info, err := n.inspect(audio)
	if err != nil {
		return song.Song{}, err
	}
	if !info.Playable {
		return song.Song{}, fmt.Errorf("%w: 网易云这首要会员或没有版权", ErrNoResult)
	}

	// 歌名歌手和歌词跟探测没有依赖, 并行取; 两个都失败也不影响播 —— 歌名退回 id。
	var (
		wg   sync.WaitGroup
		meta song.Song
		lrc  string
	)
	wg.Add(2)
	go func() { defer wg.Done(); meta = n.meta(id) }()
	go func() { defer wg.Done(); lrc = n.lyric(id) }()
	wg.Wait()

	meta.Source = song.Wyy
	meta.ID = id
	meta.Audio = audio
	meta.Lyrics = lrc
	if meta.Duration == 0 {
		meta.Duration = info.Seconds
	}
	return meta, nil
}

// inspect 过闸门再探测; 探测本身不消耗上游配额之外的资源, 但要算进熔断。
func (n *netease) inspect(audio string) (Audio, error) {
	if err := n.gate.Enter(); err != nil {
		return Audio{}, err
	}
	info, err := Inspect(audio)
	n.gate.Done(err == nil && info.Playable)
	return info, err
}

func (n *netease) meta(id string) song.Song {
	if err := n.gate.Enter(); err != nil {
		return song.Song{}
	}
	form := url.Values{
		"ids": {"[" + id + "]"},
		"c":   {`[{"id":` + id + `}]`},
	}
	body, err := n.http.DoBytes("POST", neteaseDetailAPI, requests.Form(form), neteaseHeaders)
	n.gate.Done(err == nil)
	if err != nil {
		return song.Song{}
	}

	var res struct {
		Songs []struct {
			Name     string `json:"name"`
			Duration int    `json:"duration"`
			Artists  []struct {
				Name string `json:"name"`
			} `json:"artists"`
		} `json:"songs"`
	}
	if json.Unmarshal(body, &res) != nil || len(res.Songs) == 0 {
		return song.Song{}
	}
	item := res.Songs[0]
	names := make([]string, 0, len(item.Artists))
	for _, a := range item.Artists {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return song.Song{
		Title:    item.Name,
		Artist:   strings.Join(names, "/"),
		Duration: item.Duration / 1000,
	}
}

// lyric 拿歌词; 拿不到就返回空串 —— 没歌词不是错误。
func (n *netease) lyric(id string) string {
	if err := n.gate.Enter(); err != nil {
		return ""
	}
	query := neteaseLyricAPI + "?id=" + url.QueryEscape(id) + "&lv=-1&kv=-1&tv=-1"
	var res struct {
		LRC struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
	}
	err := n.http.Get(query, &res, neteaseHeaders)
	n.gate.Done(err == nil)
	if err != nil {
		return ""
	}
	return res.LRC.Lyric
}

// neteaseAudioURL 是那条"跳到真文件"的直链。它自己不是文件, 所以取直链和探测可用性
// 是同一次请求的事 —— 见 Inspect。
func neteaseAudioURL(id string) string {
	return neteaseOuterURL + "?id=" + url.QueryEscape(id) + ".mp3"
}

func parseNeteaseSearch(body []byte, limit int) ([]song.Song, error) {
	var res struct {
		Result struct {
			Songs []struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Duration int    `json:"duration"`
				Fee      int    `json:"fee"`
				Artists  []struct {
					Name string `json:"name"`
				} `json:"artists"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("网易搜索响应解析失败: %w", err)
	}

	songs := make([]song.Song, 0, len(res.Result.Songs))
	for _, item := range res.Result.Songs {
		if item.ID == 0 || item.Name == "" {
			continue
		}
		// fee 1 是要会员、4 是要买专辑, 这两种外层链接必回 404, 列出来只是让用户白点。
		if item.Fee == 1 || item.Fee == 4 {
			continue
		}
		names := make([]string, 0, len(item.Artists))
		for _, a := range item.Artists {
			if a.Name != "" {
				names = append(names, a.Name)
			}
		}
		songs = append(songs, song.Song{
			Source:   song.Wyy,
			ID:       strconv.FormatInt(item.ID, 10),
			Title:    item.Name,
			Artist:   strings.Join(names, "/"),
			Duration: item.Duration / 1000,
		})
		if len(songs) == limit {
			break
		}
	}
	return songs, nil
}
