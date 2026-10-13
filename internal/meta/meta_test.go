package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 三个平台各问一次: 假服务器回真的形状, 断言标题/作者/封面都被补上。
func TestFillByPlatform(t *testing.T) {
	cases := []struct {
		name     string
		pick     song.Song
		path     string
		response string
		want     song.Song
	}{
		{
			name: "QQ 按 songmid 问官方详情, 封面由专辑 mid 拼出来",
			pick: song.Song{Platform: song.QQ, ID: "002rgUAM3J19lJ"},
			path: "/fcg_play_single_song.fcg",
			response: `{"code":0,"data":[{"name":"江湖少年","singer":[{"name":"东篱"},{"name":"弦雾"}],
				"album":{"mid":"002VAn1M3AoYfr"}}]}`,
			want: song.Song{
				Platform: song.QQ, ID: "002rgUAM3J19lJ",
				Title: "江湖少年", Artist: "东篱、弦雾",
				Cover: "https://y.gtimg.cn/music/photo_new/T002R500x500M000002VAn1M3AoYfr.jpg",
			},
		},
		{
			name: "酷狗只拿 hash 去问, 封面替掉 {size}",
			pick: song.Song{Platform: song.Kugou, ID: "29B24C14D7365AE9F50B8836DB40C64C|154587915"},
			path: "/getSongInfo.php",
			response: `{"errcode":0,"songName":"花海","singerName":"枯北",
				"album_img":"http://imge.kugou.com/stdmusic/{size}/20251220/x.jpg"}`,
			want: song.Song{
				Platform: song.Kugou, ID: "29B24C14D7365AE9F50B8836DB40C64C|154587915",
				Title: "花海", Artist: "枯北",
				Cover: "http://imge.kugou.com/stdmusic/480/20251220/x.jpg",
			},
		},
		{
			name: "网易云按歌曲 id 问官方详情",
			pick: song.Song{Platform: song.Wyy, ID: "1974976237"},
			path: "/song/detail",
			response: `{"songs":[{"name":"爱人","artists":[{"name":"莉莉周她说"}],
				"album":{"picUrl":"https://p1.music.126.net/x.jpg"}}]}`,
			want: song.Song{
				Platform: song.Wyy, ID: "1974976237",
				Title: "爱人", Artist: "莉莉周她说",
				Cover: "https://p1.music.126.net/x.jpg",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotQuery string
			mux := http.NewServeMux()
			mux.HandleFunc(c.path, func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				_, _ = w.Write([]byte(c.response))
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			restore := pointAt(server.URL)
			defer restore()

			got := Fill(context.Background(), c.pick)
			if got != c.want {
				t.Errorf("补齐结果不对:\n实得 %+v\n期望 %+v", got, c.want)
			}
			// 请求要带够平台认的 ID: QQ 用 songmid, 酷狗用 hash(不带 album_id), 网易云用 [id]。
			for _, want := range wantParams(c.pick) {
				if !strings.Contains(gotQuery, want) {
					t.Errorf("请求串 %q 里少了 %q", gotQuery, want)
				}
			}
		})
	}
}

func wantParams(pick song.Song) []string {
	switch pick.Platform {
	case song.QQ:
		return []string{"songmid=" + pick.ID}
	case song.Kugou:
		return []string{"hash=29B24C14D7365AE9F50B8836DB40C64C"}
	case song.Wyy:
		return []string{"ids=%5B" + pick.ID + "%5D"}
	}
	return nil
}

// 搜索结果本来就带全了标题和封面的那条路(不点短码的时候), 一次请求都不该发。
func TestFillSkipsRequestWhenNothingMissing(t *testing.T) {
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		_, _ = w.Write([]byte(`{"code":0,"data":[{"name":"不该被用到"}]}`))
	}))
	defer server.Close()
	defer pointAt(server.URL)()

	in := song.Song{Platform: song.QQ, ID: "002rgUAM3J19lJ", Title: "江湖少年", Cover: "https://x/y.jpg"}
	if got := Fill(context.Background(), in); got != in {
		t.Errorf("有标题有封面就该原样返回: %+v", got)
	}
	if called != 0 {
		t.Errorf("什么都不缺还发了 %d 次请求", called)
	}
}

// 上游挂了、回的不是 JSON、回的是一首别的歌 —— 都不该影响点歌这件事本身。
func TestFillKeepsPickWhenUpstreamIsUseless(t *testing.T) {
	const path = "/fcg_play_single_song.fcg"
	responses := []struct{ name, body string }{
		{"不是 JSON", `<html>502 Bad Gateway</html>`},
		{"空列表", `{"code":0,"data":[]}`},
		{"字段全空", `{"code":0,"data":[{"name":"","singer":[],"album":{"mid":""}}]}`},
	}
	for _, c := range responses {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(c.body))
			}))
			defer server.Close()
			defer pointAt(server.URL)()

			pick := song.Song{Platform: song.QQ, ID: "002rgUAM3J19lJ", Audio: "https://x/y.mp3"}
			got := Fill(context.Background(), pick)
			if got != pick {
				t.Errorf("补不到就该原样返回, 实得 %+v", got)
			}
		})
	}
}

// 连服务器都没起(地址打不通)时同样要原样返回, 不能把点歌带崩。
func TestFillSurvivesDeadUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close() // 立刻关掉, 后面的请求必然失败
	defer pointAt(server.URL)()

	pick := song.Song{Platform: song.Wyy, ID: "1974976237", Audio: "https://x/y.mp3"}
	if got := Fill(context.Background(), pick); got != pick {
		t.Errorf("上游不通时该原样返回, 实得 %+v", got)
	}
}

// 酷我没有公开的按 ID 查详情的接口 —— 那就一个请求都别发, 让卡片自己跳过。
func TestFillLeavesKuwoAlone(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"data":{"name":"不该被用到"}}`))
	}))
	defer server.Close()
	defer pointAt(server.URL)()

	pick := song.Song{Platform: song.Kuwo, ID: "7728138"}
	if got := Fill(context.Background(), pick); got != pick {
		t.Errorf("酷我应该原样返回: %+v", got)
	}
	if called {
		t.Error("酷我不该发请求")
	}
}

// 已经取消的上下文不该再发请求。
func TestFillStopsOnCanceledContext(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"code":0,"data":[{"name":"江湖少年"}]}`))
	}))
	defer server.Close()
	defer pointAt(server.URL)()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pick := song.Song{Platform: song.QQ, ID: "002rgUAM3J19lJ"}
	if got := Fill(ctx, pick); got != pick {
		t.Errorf("上下文已取消就该原样返回: %+v", got)
	}
	if called {
		t.Error("上下文已取消还发了请求")
	}
}

// 已有值的字段不该被上游覆盖: 卡片上的歌名以搜索结果为准。
func TestFillDoesNotOverwritePresentFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":[{"name":"上游的歌名","singer":[{"name":"上游的歌手"}],
			"album":{"mid":"002VAn1M3AoYfr"}}]}`))
	}))
	defer server.Close()
	defer pointAt(server.URL)()

	in := song.Song{Platform: song.QQ, ID: "002rgUAM3J19lJ", Title: "本地的歌名", Artist: "本地的歌手"}
	got := Fill(context.Background(), in)
	if got.Title != "本地的歌名" || got.Artist != "本地的歌手" {
		t.Errorf("已有值被覆盖了: %+v", got)
	}
	if got.Cover == "" {
		t.Error("缺的封面该被补上")
	}
}

// pointAt 把三个平台地址都指到假服务器上, 返回还原函数。
func pointAt(base string) func() {
	oldQQ, oldKG, oldWY := qqDetailURL, kgInfoURL, wyDetailURL
	qqDetailURL = base + "/fcg_play_single_song.fcg"
	kgInfoURL = base + "/getSongInfo.php?cmd=playInfo"
	wyDetailURL = base + "/song/detail"
	return func() { qqDetailURL, kgInfoURL, wyDetailURL = oldQQ, oldKG, oldWY }
}

// 真联网: 三个平台的详情接口今天还认不认我们这套 ID 和字段。加 -short 跳过。
func TestFillOnline(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}
	picks := []song.Song{
		{Platform: song.QQ, ID: "002rgUAM3J19lJ"},
		{Platform: song.Kugou, ID: "29B24C14D7365AE9F50B8836DB40C64C|154587915"},
		{Platform: song.Wyy, ID: "1974976237"},
	}
	for _, pick := range picks {
		got := Fill(context.Background(), pick)
		t.Logf("%s %s -> 标题=%q 作者=%q 封面=%s", got.Platform, got.ID, got.Title, got.Artist, got.Cover)
		if got.Title == "" {
			t.Errorf("%s 没补到标题", got.Platform)
		}
	}
}
