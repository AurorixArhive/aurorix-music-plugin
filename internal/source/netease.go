package source

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const (
	neteaseSearchAPI = "http://music.163.com/api/cloudsearch/pc"
	neteaseAudioAPI  = "http://music.163.com/song/media/outer/url"
)

var neteaseHeaders = map[string]string{"Referer": "http://music.163.com/"}

// 同酷我: 大厂公开接口, 且是兜底的一环, 额度放宽一点。
var neteaseGate = guard.Options{
	Name: "网易音源", RPS: 10, Burst: 20, WaitBudget: 3 * time.Second,
	Failures: 8, Cooldown: 20 * time.Second,
}

type Netease struct {
	http *requests.Client
	gate *guard.Gate
}

func NewNetease(http *requests.Client) Netease {
	return Netease{http: http, gate: guard.NewGate(neteaseGate)}
}

func (Netease) Name() song.Source { return song.Netease }

func (n Netease) Search(keyword string, limit int) ([]song.Song, error) {
	if err := n.gate.Enter(); err != nil {
		return nil, err
	}
	form := url.Values{
		"offset": {"0"},
		"limit":  {strconv.Itoa(limit)},
		"type":   {"1"},
		"s":      {keyword},
	}

	body, err := n.http.DoBytes("POST", neteaseSearchAPI, requests.Form(form), neteaseHeaders)
	if err != nil {
		n.gate.Done(false)
		return nil, err
	}
	songs, err := parseNeteaseSearch(body)
	n.gate.Done(normal(err) == nil)
	return songs, err
}

func parseNeteaseSearch(body []byte) ([]song.Song, error) {
	var res struct {
		Result struct {
			Songs []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				Ar   []struct {
					Name string `json:"name"`
				} `json:"ar"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("netease 搜索响应解析失败: %w", err)
	}

	songs := make([]song.Song, 0, len(res.Result.Songs))
	for _, item := range res.Result.Songs {
		if item.ID == 0 || item.Name == "" {
			continue
		}
		artists := make([]string, 0, len(item.Ar))
		for _, artist := range item.Ar {
			if artist.Name != "" {
				artists = append(artists, artist.Name)
			}
		}
		songs = append(songs, song.Song{
			Source: song.Netease,
			ID:     strconv.FormatInt(item.ID, 10),
			Title:  item.Name,
			Artist: strings.Join(artists, "/"),
		})
	}
	return songs, nil
}

// 这里只是拼一个跳转链接, 真正的下载由 QQ 平台发起, 所以不用过闸门。
func (Netease) Audio(pick song.Song) (string, error) {
	return neteaseAudioAPI + "?id=" + url.QueryEscape(pick.ID) + ".mp3", nil
}
