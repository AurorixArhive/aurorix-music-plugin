package source

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

const txqqAPI = "https://music.txqq.pro/"

// 聚合站按移动端网页认: 少了 UA / X-Requested-With, 首页能开, 接口回空。
var txqqHeaders = map[string]string{
	"User-Agent":       "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:146.0) Gecko/20100101 Firefox/146.0",
	"Accept":           "application/json, text/javascript, */*; q=0.01",
	"X-Requested-With": "XMLHttpRequest",
	"Origin":           "https://music.txqq.pro",
	"Referer":          "https://music.txqq.pro",
}

// 聚合站一次请求就把直链、封面、歌词给全, 所以搜索和详情打的是同一个接口,
// 只是 filter 从 name 换成 id。三家平台同域名, 共用一套闸门 —— 分成三套只会让总量超限。
var txqqGate = guard.Options{
	Name: "聚合音源", RPS: 8, Burst: 16, WaitBudget: 3 * time.Second,
	Failures: 8, Cooldown: 20 * time.Second,
}

type txqq struct {
	http     *requests.Client
	gate     *guard.Gate
	platform string // 接口认的平台名, 见 txqqPlatforms
	name     song.Source
}

// txqqPlatforms 是聚合站上实测能用的那几家。
//
// 筛选口径是"搜索出来的行带 url, 且按 id 还能再取回来", 所以被排除的是:
//
//	kuwo      四个关键词搜出来的 url 全是空, 一个都播不了
//	qingting  搜索能出 url, 但 songid 带 "|", 按 id 复取一律 404 —— 卡片点不动
//	kugou/migu/lizhi/ximalaya/5singyc/5singfc/kg  上游直接返回 0 条
//
// 宁可少几家, 也不要在卡片上列一堆点了没反应的歌。
var txqqPlatforms = []struct {
	platform string
	name     song.Source
}{
	{"qq", song.QQ},
	{"baidu", song.Baidu},
	{"1ting", song.Ting1},
}

// NewTxQQ 造出聚合站上那几家音源, 顺序即默认优先级。
func NewTxQQ(http *requests.Client) []Source {
	gate := guard.NewGate(txqqGate)
	out := make([]Source, 0, len(txqqPlatforms))
	for _, p := range txqqPlatforms {
		out = append(out, &txqq{http: http, gate: gate, platform: p.platform, name: p.name})
	}
	return out
}

func (t *txqq) Name() song.Source { return t.name }

func (t *txqq) Search(keyword string, limit int) ([]song.Song, error) {
	return t.fetch("name", keyword, limit)
}

func (t *txqq) Detail(id string) (song.Song, error) {
	songs, err := t.fetch("id", id, 1)
	if err != nil {
		return song.Song{}, err
	}
	if len(songs) == 0 {
		return song.Song{}, ErrNoResult
	}
	found := songs[0]

	// 聚合站不给时长, 只能自己摸一下直链: 既拿到时长(决定发语音还是文件), 也顺手确认它真是音频。
	info := t.inspect(found.Audio)
	if !info.Playable {
		return song.Song{}, fmt.Errorf("%w: 聚合站给的这条直链现在放不出来", ErrNoResult)
	}
	found.Duration = info.Seconds
	return found, nil
}

// inspect 过闸门再探测: 探测也是一次打到 CDN 的请求, 得算在熔断里。
func (t *txqq) inspect(audio string) Audio {
	if err := t.gate.Enter(); err != nil {
		return Audio{}
	}
	info, err := Inspect(audio)
	t.gate.Done(err == nil && info.Playable)
	return info
}

func (t *txqq) fetch(filter, input string, limit int) ([]song.Song, error) {
	if err := t.gate.Enter(); err != nil {
		return nil, err
	}
	form := url.Values{
		"input":  {input},
		"filter": {filter},
		"type":   {t.platform},
		"page":   {"1"},
	}
	body, err := t.http.DoBytes("POST", txqqAPI, requests.Form(form), txqqHeaders)
	if err != nil {
		t.gate.Done(false)
		return nil, err
	}
	songs, err := parseTxQQ(body, t.name, limit)
	t.gate.Done(normal(err) == nil)
	return songs, err
}

type txqqResponse struct {
	// data 在"没这首歌"时是个空串而不是数组, 所以只能先按原始 JSON 收。
	Data  json.RawMessage `json:"data"`
	Code  int             `json:"code"`
	Error string          `json:"error"`
}

type txqqRow struct {
	// songid 在同一家的不同平台上会是字符串(qq)或数字(网易/一听), 先原样收着再自己剥。
	SongID json.RawMessage `json:"songid"`
	Title  string          `json:"title"`
	Author string          `json:"author"`
	LRC    string          `json:"lrc"`
	URL    string          `json:"url"`
}

func parseTxQQ(body []byte, name song.Source, limit int) ([]song.Song, error) {
	var res txqqResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("聚合音源响应解析失败: %w", err)
	}
	if len(res.Data) == 0 || res.Data[0] != '[' {
		return nil, nil
	}
	var rows []txqqRow
	if err := json.Unmarshal(res.Data, &rows); err != nil {
		return nil, fmt.Errorf("聚合音源结果解析失败: %w", err)
	}

	songs := make([]song.Song, 0, min(limit, len(rows)))
	for _, row := range rows {
		id := strings.Trim(string(row.SongID), `"`)
		// 没有直链的行直接丢掉: 列在卡片上只会让用户点了空欢喜。
		if id == "" || row.Title == "" || row.URL == "" {
			continue
		}
		songs = append(songs, song.Song{
			Source: name,
			ID:     id,
			Title:  row.Title,
			Artist: row.Author,
			Audio:  row.URL,
			Lyrics: row.LRC,
		})
		if len(songs) == limit {
			break
		}
	}
	return songs, nil
}

// Defaults 按默认优先级造出全部音源: 聚合站的 qq 最稳排第一, 网易其次, 然后聚合站的其余两家。
// 顺序只是初值, 管理台的 source_order 能随时改。
func Defaults(http *requests.Client) []Source {
	byName := map[song.Source]Source{}
	for _, src := range NewTxQQ(http) {
		byName[src.Name()] = src
	}
	byName[song.Wyy] = NewNetease(http)

	order := []song.Source{song.QQ, song.Wyy, song.Baidu, song.Ting1}
	out := make([]Source, 0, len(order))
	for _, name := range order {
		if src, ok := byName[name]; ok {
			out = append(out, src)
		}
	}
	return out
}
