package source

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

func testClient() *requests.Client { return requests.Init(10) }

// fakeQQ 把两条接口地址都指到本地假服务器: POST 是 musicu.fcg, GET 是详情接口。
// handler 拿到请求(含头)与正文/查询串, 决定回什么。
func fakeQQ(t *testing.T, handler func(r *http.Request, body string) (int, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if r.Method == http.MethodGet {
			body = r.URL.RawQuery
		}
		status, payload := handler(r, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	oldMusic, oldDetail := qqMusicAPI, qqDetailAPI
	qqMusicAPI, qqDetailAPI = srv.URL, srv.URL
	t.Cleanup(func() {
		qqMusicAPI, qqDetailAPI = oldMusic, oldDetail
		srv.Close()
	})
	return srv
}

func TestParseQQSearchStripsEmAndJoinsSingers(t *testing.T) {
	body := []byte(`{"search":{"data":{"body":{"item_song":[` +
		`{"mid":"003cI52o4daJJL","title":"<em>花海</em>","interval":264,"singer":[{"name":"周杰伦"}]},` +
		`{"mid":"","title":"没有 mid"},` +
		`{"mid":"x","title":""}` +
		`]}}}}`)

	songs, err := parseQQSearch(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 1 {
		t.Fatalf("songs = %+v", songs)
	}
	// 标题里的 <em> 高亮标签必须剥掉; interval 要落进 Duration(卡片时长过滤靠它)。
	want := song.Song{Source: song.QQ, ID: "003cI52o4daJJL", Title: "花海", Artist: "周杰伦", Duration: 264}
	if songs[0] != want {
		t.Fatalf("songs[0] = %+v, want %+v", songs[0], want)
	}
}

func TestParseQQSearchFallsBackToSongList(t *testing.T) {
	body := []byte(`{"search":{"data":{"body":{"song":{"list":[{"mid":"m","title":"备用","singer":[{"name":"A"},{"name":"B"}]}]}}}}}`)

	songs, err := parseQQSearch(body)
	if err != nil || len(songs) != 1 {
		t.Fatalf("songs = %+v, err = %v", songs, err)
	}
	if songs[0].Title != "备用" || songs[0].Artist != "A、B" {
		t.Fatalf("songs[0] = %+v", songs[0])
	}
}

func TestParseQQSearchFallsBackToNameField(t *testing.T) {
	body := []byte(`{"search":{"data":{"body":{"item_song":[{"mid":"m","name":"只有 name"}]}}}}`)

	songs, err := parseQQSearch(body)
	if err != nil || len(songs) != 1 {
		t.Fatalf("songs = %+v, err = %v", songs, err)
	}
	if songs[0].Title != "只有 name" {
		t.Fatalf("Title = %q", songs[0].Title)
	}
}

func TestParseQQSearchEmptyAndGarbage(t *testing.T) {
	if songs, err := parseQQSearch([]byte(`{"search":{"data":{"body":{}}}}`)); err != nil || len(songs) != 0 {
		t.Fatalf("songs = %+v, err = %v", songs, err)
	}
	if _, err := parseQQSearch([]byte(`not json`)); err == nil {
		t.Fatal("非 JSON 应报错")
	}
}

func TestQQSearchSendsQueryAndOptionalCookie(t *testing.T) {
	var seen *http.Request
	var body string
	fakeQQ(t, func(r *http.Request, b string) (int, string) {
		seen, body = r, b
		return 200, `{"search":{"data":{"body":{"item_song":[{"mid":"m","title":"t"}]}}}}`
	})

	if _, err := NewQQ(testClient(), func() string { return "qqmusic_key=abc" }).Search("花海", 5); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"query":"花海"`) || !strings.Contains(body, "SearchCgiService") {
		t.Fatalf("搜索请求体不对: %s", body)
	}
	if got := seen.Header.Get("Cookie"); got != "qqmusic_key=abc" {
		t.Fatalf("配了 Cookie 就该带上, got %q", got)
	}
}

func TestQQWithoutCookieSendsNoCookieHeader(t *testing.T) {
	var cookie string
	fakeQQ(t, func(r *http.Request, _ string) (int, string) {
		cookie = r.Header.Get("Cookie")
		return 200, `{"search":{"data":{"body":{"item_song":[]}}}}`
	})

	if _, err := NewQQ(testClient(), nil).Search("花海", 5); err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		t.Fatalf("没配 Cookie 就不该发, got %q", cookie)
	}
}

func TestQQAudioPicksFirstNonEmptyPurl(t *testing.T) {
	var vkeyBody string
	fakeQQ(t, func(r *http.Request, body string) (int, string) {
		if r.Method == http.MethodGet {
			return 200, `{"data":[{"file":{"media_mid":"002JkHfm3Mws2i"}}]}`
		}
		vkeyBody = body
		return 200, `{"req_0":{"code":0,"data":{"midurlinfo":[{"purl":""},{"purl":"M500002JkHfm3Mws2i.mp3?vkey=1"}]}}}`
	})

	link, err := NewQQ(testClient(), nil).Audio(song.Song{Source: song.QQ, ID: "003hKob536i6FA"})
	if err != nil {
		t.Fatal(err)
	}
	if link != qqStreamBase+"M500002JkHfm3Mws2i.mp3?vkey=1" {
		t.Fatalf("link = %q", link)
	}
	// 文件名必须用 media_mid 拼(用 songmid 会 404), 且音质从高到低都排上。
	for _, want := range []string{"M800002JkHfm3Mws2i.mp3", "M500002JkHfm3Mws2i.mp3", "C400002JkHfm3Mws2i.m4a"} {
		if !strings.Contains(vkeyBody, want) {
			t.Fatalf("取链请求里缺 %s: %s", want, vkeyBody)
		}
	}
}

func TestQQAudioNoPurlReturnsNoResult(t *testing.T) {
	fakeQQ(t, func(r *http.Request, _ string) (int, string) {
		if r.Method == http.MethodGet {
			return 200, `{"data":[{"file":{"media_mid":"mm"}}]}`
		}
		return 200, `{"req_0":{"code":0,"data":{"midurlinfo":[{"purl":""}]}}}`
	})

	if _, err := NewQQ(testClient(), nil).Audio(song.Song{Source: song.QQ, ID: "m"}); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
}

func TestQQAudioMissingMediaMidReturnsNoResult(t *testing.T) {
	fakeQQ(t, func(r *http.Request, _ string) (int, string) {
		return 200, `{"data":[]}`
	})

	if _, err := NewQQ(testClient(), nil).Audio(song.Song{Source: song.QQ, ID: "m"}); !errors.Is(err, ErrNoResult) {
		t.Fatalf("err = %v; 期望 ErrNoResult", err)
	}
}
