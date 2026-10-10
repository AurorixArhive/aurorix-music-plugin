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
	kuwoSearchAPI = "http://search.kuwo.cn/r.s"
	kuwoAudioAPI  = "http://antiserver.kuwo.cn/anti.s"
	kuwoRidPrefix = "MUSIC_"
)

// kuwoParams 缺一项就会退化成单引号伪 JSON, 以下参数整组照搬移动端。
var kuwoParams = map[string]string{
	"client": "kt", "ft": "music", "rformat": "json", "encoding": "utf8",
	"prod": "kwplayer_ar_10.1.2.1", "corp": "kuwo", "newver": "3",
	"ver": "kwplayer_ar_10.1.2.1", "vipver": "1", "p2p": "1",
	"source": "kwplayer_ar_10.1.2.1_40.apk", "notrace": "0",
	"show_copyright_off": "1", "correct": "1", "cluster": "0",
	"strategy": "2012", "vermerge": "1", "mobi": "1",
	"searchapi": "5", "issubtitle": "1", "spPrivilege": "0",
}

// 大厂公开接口, 而且 QQ 音源断开时流量会全压到它身上, 所以额度放宽一点。
var kuwoGate = guard.Options{
	Name: "酷我音源", RPS: 10, Burst: 20, WaitBudget: 3 * time.Second,
	Failures: 8, Cooldown: 20 * time.Second,
}

type Kuwo struct {
	http *requests.Client
	gate *guard.Gate
}

func NewKuwo(http *requests.Client) Kuwo {
	return Kuwo{http: http, gate: guard.NewGate(kuwoGate)}
}

func (Kuwo) Name() song.Source { return song.Kuwo }

func (k Kuwo) Search(keyword string, limit int) ([]song.Song, error) {
	if err := k.gate.Enter(); err != nil {
		return nil, err
	}
	query := url.Values{}
	for key, value := range kuwoParams {
		query.Set(key, value)
	}
	query.Set("all", keyword)
	query.Set("pn", "0")
	query.Set("rn", strconv.Itoa(limit))

	body, err := k.http.DoBytes("GET", kuwoSearchAPI+"?"+query.Encode(), nil, nil)
	if err != nil {
		k.gate.Done(false)
		return nil, err
	}
	songs, err := parseKuwoSearch(body)
	k.gate.Done(normal(err) == nil)
	return songs, err
}

func parseKuwoSearch(body []byte) ([]song.Song, error) {
	var res struct {
		AbsList []struct {
			Rid    string `json:"MUSICRID"`
			Name   string `json:"NAME"`
			Artist string `json:"ARTIST"`
		} `json:"abslist"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("kuwo 搜索响应解析失败: %w", err)
	}

	songs := make([]song.Song, 0, len(res.AbsList))
	for _, item := range res.AbsList {
		id := strings.TrimPrefix(item.Rid, kuwoRidPrefix)
		if id == "" || item.Name == "" {
			continue
		}
		songs = append(songs, song.Song{
			Source: song.Kuwo,
			ID:     id,
			Title:  item.Name,
			Artist: item.Artist,
		})
	}
	return songs, nil
}

func (k Kuwo) Audio(pick song.Song) (string, error) {
	if err := k.gate.Enter(); err != nil {
		return "", err
	}
	query := url.Values{
		"useless":  {"/resource/"},
		"format":   {"mp3"},
		"rid":      {kuwoRidPrefix + pick.ID},
		"response": {"url"},
		"type":     {"convert_url"},
		"br":       {"128kmp3"},
	}

	body, err := k.http.DoBytes("GET", kuwoAudioAPI+"?"+query.Encode(), nil, nil)
	if err != nil {
		k.gate.Done(false)
		return "", err
	}
	link, err := parseKuwoAudio(body)
	k.gate.Done(normal(err) == nil)
	return link, err
}

func parseKuwoAudio(body []byte) (string, error) {
	link := strings.TrimSpace(string(body))
	if !strings.HasPrefix(link, "http") {
		return "", ErrNoResult
	}
	return link, nil
}
