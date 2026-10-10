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
	qqDefaultAPIKey = "sk-bc0f63c5814aaa34b3c507b9c4879e62"
	qqSearchType    = "0"
)

// 测试要把它指到本地假服务器, 所以是变量不是常量。
var qqAPIBase = "https://api2.vsaa.cn/api/music/qq2/index.php"

// 320k 只有四成左右的曲目有, 所以按序降级; 再往上就超出语音消息该有的体积了。
var qqQualities = []string{"MP3_320", "MP3_128"}

// 这个音源是第三方小站: 共享密钥、没有 SLA, 闸门拧得最紧。
var qqGate = guard.Options{
	Name: "QQ 音源", RPS: 5, Burst: 10, WaitBudget: 3 * time.Second,
	Failures: 5, Cooldown: 30 * time.Second,
}

type QQ struct {
	http   *requests.Client
	apiKey func() string
	gate   *guard.Gate
}

func NewQQ(http *requests.Client, apiKey func() string) QQ {
	return QQ{http: http, apiKey: apiKey, gate: guard.NewGate(qqGate)}
}

func (QQ) Name() song.Source { return song.QQ }

func (q QQ) Search(keyword string, limit int) ([]song.Song, error) {
	if err := q.gate.Enter(); err != nil {
		return nil, err
	}
	body, err := q.get("/search", url.Values{
		"keyword": {keyword},
		"type":    {qqSearchType},
		"num":     {strconv.Itoa(limit)},
		"page":    {"1"},
	})
	if err != nil {
		q.gate.Done(false)
		return nil, err
	}
	songs, err := parseQQSearch(body)
	q.gate.Done(normal(err) == nil)
	return songs, err
}

// 上游是"多账号池"式的实现: 同一个 mid、同一个音质, 连着请求两次可能一次给直链一次说没有
// (实测 result 在 0 和 22 之间反复横跳, 单次成功率五成上下)。所以同一个音质要试几次再降级,
// 不然点十次有五次是白点。
const qqAttempts = 3

func (q QQ) Audio(pick song.Song) (string, error) {
	var last error = ErrNoResult
	for _, quality := range qqQualities {
		for attempt := 0; attempt < qqAttempts; attempt++ {
			if err := q.gate.Enter(); err != nil {
				return "", err
			}
			body, err := q.get("/song/url", url.Values{"mid": {pick.ID}, "type": {quality}})
			if err != nil {
				q.gate.Done(false)
				return "", err
			}
			link, err := parseQQAudio(body, pick.ID)
			// 付费曲取不到直链是上游的正常回答, 不该算故障
			q.gate.Done(normal(err) == nil)
			if err == nil {
				return link, nil
			}
			last = err
		}
	}
	return "", last
}

func (q QQ) get(action string, query url.Values) ([]byte, error) {
	query.Set("action", action)
	query.Set("apikey", q.key())
	return q.http.DoBytes("GET", qqAPIBase+"?"+query.Encode(), nil, nil)
}

func (q QQ) key() string {
	if q.apiKey != nil {
		if key := strings.TrimSpace(q.apiKey()); key != "" {
			return key
		}
	}
	return qqDefaultAPIKey
}

// 出错时 code 仍是 0, 真正的判据是 errcode 与缺失的 data
type qqEnvelope struct {
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	ErrCode int             `json:"errcode"`
	Data    json.RawMessage `json:"data"`
}

func qqDecode(body []byte, out any) error {
	var env qqEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("qq 响应解析失败: %w", err)
	}
	if env.ErrCode != 0 || env.Code != 0 {
		reason := env.Msg
		if reason == "" {
			reason = fmt.Sprintf("错误码 %d", env.ErrCode)
		}
		return fmt.Errorf("qq 音源不可用: %s", reason)
	}
	if len(env.Data) == 0 {
		return fmt.Errorf("qq 音源返回空数据")
	}
	return json.Unmarshal(env.Data, out)
}

type qqSinger struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

type qqItem struct {
	Mid      string     `json:"mid"`
	Name     string     `json:"name"`
	Title    string     `json:"title"`
	Singers  []qqSinger `json:"singer"`
	Interval int        `json:"interval"`
}

func parseQQSearch(body []byte) ([]song.Song, error) {
	var items []qqItem
	if err := qqDecode(body, &items); err != nil {
		return nil, err
	}

	songs := make([]song.Song, 0, len(items))
	for _, item := range items {
		title := firstNonEmpty(item.Name, item.Title)
		if item.Mid == "" || title == "" {
			continue
		}
		songs = append(songs, song.Song{
			Source:   song.QQ,
			ID:       item.Mid,
			Title:    title,
			Artist:   qqSingerName(item.Singers),
			Duration: item.Interval,
		})
	}
	return songs, nil
}

func parseQQAudio(body []byte, mid string) (string, error) {
	var links map[string]string
	if err := qqDecode(body, &links); err != nil {
		return "", err
	}
	link := strings.TrimSpace(links[mid])
	if !strings.HasPrefix(link, "http") {
		return "", ErrNoResult
	}
	return link, nil
}

func qqSingerName(singers []qqSinger) string {
	names := make([]string, 0, len(singers))
	for _, singer := range singers {
		if name := firstNonEmpty(singer.Name, singer.Title); name != "" {
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
