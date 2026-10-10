package source

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

func testClient() *requests.Client { return requests.Init(10) }

// fakeQQ 把 qqAPIBase 指到本地假服务器, 由 handler 决定回什么。
func fakeQQ(t *testing.T, handler func(query url.Values) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(handler(r.URL.Query())))
	}))
	old := qqAPIBase
	qqAPIBase = srv.URL
	t.Cleanup(func() {
		qqAPIBase = old
		srv.Close()
	})
	return srv
}

func TestQQKeyOverride(t *testing.T) {
	if got := NewQQ(nil, nil).key(); got != qqDefaultAPIKey {
		t.Fatalf("无配置时应回落到内置密钥, got %q", got)
	}
	if got := NewQQ(nil, func() string { return " sk-custom " }).key(); got != "sk-custom" {
		t.Fatalf("key = %q", got)
	}
	if got := NewQQ(nil, func() string { return "   " }).key(); got != qqDefaultAPIKey {
		t.Fatalf("空配置时应回落到内置密钥, got %q", got)
	}
}

func TestParseQQSearch(t *testing.T) {
	body := []byte(`{"code":0,"data":[` +
		`{"mid":"003cI52o4daJJL","id":449198,"name":"花海","interval":264,"singer":[{"name":"周杰伦"}],"album":{"mid":"002Neh8l0uciQZ"}},` +
		`{"mid":"","name":"没有 mid"},` +
		`{"mid":"x","name":""}` +
		`]}`)

	songs, err := parseQQSearch(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 1 {
		t.Fatalf("songs = %+v", songs)
	}
	// interval 要落进 Duration: 卡片的时长过滤全靠它。
	want := song.Song{Source: song.QQ, ID: "003cI52o4daJJL", Title: "花海", Artist: "周杰伦", Duration: 264}
	if songs[0] != want {
		t.Fatalf("songs[0] = %+v, want %+v", songs[0], want)
	}
}
func TestParseQQSearchFallsBackToTitleField(t *testing.T) {
	body := []byte(`{"code":0,"data":[{"mid":"m","title":"备用标题","singer":[{"title":"备用歌手"}]}]}`)

	songs, err := parseQQSearch(body)
	if err != nil || len(songs) != 1 {
		t.Fatalf("songs = %+v, err = %v", songs, err)
	}
	if songs[0].Title != "备用标题" || songs[0].Artist != "备用歌手" {
		t.Fatalf("songs[0] = %+v", songs[0])
	}
}

func TestParseQQSearchJoinsSingers(t *testing.T) {
	body := []byte(`{"code":0,"data":[{"mid":"m","name":"合唱","singer":[{"name":"A"},{"name":"B"}]}]}`)

	songs, err := parseQQSearch(body)
	if err != nil || len(songs) != 1 {
		t.Fatalf("songs = %+v, err = %v", songs, err)
	}
	if songs[0].Artist != "A、B" {
		t.Fatalf("Artist = %q", songs[0].Artist)
	}
}

func TestParseQQSearchEmpty(t *testing.T) {
	songs, err := parseQQSearch([]byte(`{"code":0,"data":[]}`))
	if err != nil || len(songs) != 0 {
		t.Fatalf("songs = %+v, err = %v", songs, err)
	}
}

func TestParseQQSearchReportsKeyError(t *testing.T) {
	_, err := parseQQSearch([]byte(`{"code":0,"msg":"密钥错误","errcode":11002}`))
	if err == nil || !strings.Contains(err.Error(), "密钥错误") {
		t.Fatalf("err = %v; 上游把出错也写成 code=0, 必须靠 errcode 认出来", err)
	}
}

func TestParseQQSearchRejectsGarbage(t *testing.T) {
	if _, err := parseQQSearch([]byte(`not json`)); err == nil {
		t.Fatal("非 JSON 应报错")
	}
	if _, err := parseQQSearch([]byte(`<html>502</html>`)); err == nil {
		t.Fatal("网关错误页应报错")
	}
}

func TestParseQQAudio(t *testing.T) {
	body := []byte(`{"code":0,"data":{"m1":"https://isure.stream.qqmusic.qq.com/x.mp3?vkey=1"},"diag":{"m1":{"needBuy":1}}}`)

	link, err := parseQQAudio(body, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "https://") {
		t.Fatalf("link = %q", link)
	}
}

func TestParseQQAudioNeedBuyReturnsNoResult(t *testing.T) {
	body := []byte(`{"code":0,"data":{"m1":""},"diag":{"m1":{"result":22,"needBuy":1}}}`)

	if _, err := parseQQAudio(body, "m1"); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
}

func TestParseQQAudioMissingKeyReturnsNoResult(t *testing.T) {
	body := []byte(`{"code":0,"data":{"other":"https://x/y.mp3"}}`)

	if _, err := parseQQAudio(body, "m1"); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
}

func TestQQQualityLadder(t *testing.T) {
	if len(qqQualities) < 2 || qqQualities[0] != "MP3_320" {
		t.Fatalf("应从 320k 起按序降级, got %v", qqQualities)
	}
	if last := qqQualities[len(qqQualities)-1]; last != "MP3_128" {
		t.Fatalf("兜底应是 128k, got %q", last)
	}
}

// 上游是"多账号池"式的实现, 同一个 mid 同一个音质单次成功率只有五成 ——
// 所以同一个音质必须试几次再降级, 否则点十次有五次白点。
func TestQQAudioRetriesSameQualityBeforeDegrading(t *testing.T) {
	var got []string
	srv := fakeQQ(t, func(query url.Values) string {
		got = append(got, query.Get("type"))
		// 前两次(假装)抽风, 第三次才给直链。
		if len(got) < 3 {
			return `{"code":0,"data":{"002hG4kq0WZBma":""},"diag":{"002hG4kq0WZBma":{"result":22,"needBuy":1}}}`
		}
		return `{"code":0,"data":{"002hG4kq0WZBma":"https://isure.stream.qqmusic.qq.com/x.mp3?vkey=1"}}`
	})
	defer srv.Close()

	link, err := NewQQ(testClient(), nil).Audio(song.Song{Source: song.QQ, ID: "002hG4kq0WZBma"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(link, "https://") {
		t.Fatalf("link = %q", link)
	}
	if len(got) != 3 {
		t.Fatalf("请求了 %d 次 (%v), 期望 3 次", len(got), got)
	}
	for _, quality := range got {
		if quality != "MP3_320" {
			t.Fatalf("还没试够次数就降级了: %v", got)
		}
	}
}

func TestQQAudioGivesUpAfterAllAttempts(t *testing.T) {
	var count int
	srv := fakeQQ(t, func(url.Values) string {
		count++
		return `{"code":0,"data":{"002hG4kq0WZBma":""},"diag":{"002hG4kq0WZBma":{"result":22}}}`
	})
	defer srv.Close()

	_, err := NewQQ(testClient(), nil).Audio(song.Song{Source: song.QQ, ID: "002hG4kq0WZBma"})
	if !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
	if count != qqAttempts*len(qqQualities) {
		t.Fatalf("总请求数 = %d, 期望 %d(音质数 × 每音质次数)", count, qqAttempts*len(qqQualities))
	}
}
