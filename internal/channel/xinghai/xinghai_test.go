package xinghai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
)

// 拿假 API 把整条链跑一遍: 搜索 → 取直链 → 探测。
func TestKuwoChannelAgainstStubAPI(t *testing.T) {
	var audioURL string
	shellCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/r.s", func(w http.ResponseWriter, r *http.Request) {
		// 酷我回的是单引号伪 JSON, 还带高亮实体
		_, _ = w.Write([]byte(`{'abslist': [
			{'SONGNAME':'晴天&nbsp;','ARTIST':'周杰伦','MUSICRID':'MUSIC_277246372','DURATION':'269'},
			{'SONGNAME':'没 id','ARTIST':'x','MUSICRID':'','DURATION':'0'}
		]}`))
	})
	mux.HandleFunc("/mobi.s", func(w http.ResponseWriter, r *http.Request) {
		shellCalls++
		if strings.Contains(r.URL.Query().Get("source"), "kwplayer_ar_8.5.5.0") {
			_, _ = w.Write([]byte(`{"data":{"url":""}}`)) // 第一个壳不给链
			return
		}
		_, _ = w.Write([]byte(`{"data":{"url":"` + audioURL + `"}}`))
	})
	mux.HandleFunc("/audio", serveStubMP3)
	server := httptest.NewServer(mux)
	defer server.Close()
	audioURL = server.URL + "/audio"

	oldSearch, oldShells, oldBR := kuwoSearchURL, kuwoShells, kuwoBR
	kuwoSearchURL = server.URL + "/r.s?client=kt&ft=music&rformat=json&encoding=utf8&pn=0&vipver=1"
	kuwoShells = []struct{ base, source string }{
		{server.URL, "kwplayer_ar_8.5.5.0_apk_keluze.apk"},
		{server.URL, "kwplayercar_ar_6.1.0.0_B_jiakong_vh.apk"},
	}
	kuwoBR = []string{"320kmp3", "128kmp3"}
	defer func() { kuwoSearchURL, kuwoShells, kuwoBR = oldSearch, oldShells, oldBR }()

	c := channelFor(t, "kuwo.xinghai")

	found, err := c.Search(context.Background(), "晴天", 5)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("脏数据没滤掉, 拿到 %d 条: %+v", len(found), found)
	}
	if found[0].ID != "277246372" || found[0].Duration != 269 {
		t.Errorf("id/时长解析不对: %+v", found[0])
	}
	if found[0].Title != "晴天" {
		t.Errorf("HTML 实体没清掉: %q", found[0].Title)
	}

	got, err := c.Resolve(context.Background(), found[0])
	if err != nil {
		t.Fatalf("取直链失败: %v", err)
	}
	if got.Audio != audioURL || got.Duration != 10 {
		t.Errorf("直链/时长不对: %+v", got)
	}
	// 第一个壳 320 和 128 都不给, 才轮到第二个壳
	if shellCalls != 3 {
		t.Errorf("第一个壳不给链时该换第二个壳, 实际请求 %d 次", shellCalls)
	}
}

func TestPlainStripsHighlightEntities(t *testing.T) {
	if got := plain("晴天&nbsp;&amp;&nbsp;"); got != "晴天 &" {
		t.Errorf("plain() = %q", got)
	}
}

func channelFor(t *testing.T, id string) channel.Channel {
	t.Helper()
	for _, c := range channel.All() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("渠道 %s 没注册", id)
	return channel.Channel{}
}

// serveStubMP3 回一段能过探测的假音频: MPEG1 Layer3 128k 的帧, 帧长 417 字节。
// 探测要连着的下一帧头对得上, 所以假数据也得老实拼帧。
func serveStubMP3(w http.ResponseWriter, r *http.Request) {
	frame := make([]byte, 417)
	frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFB, 0x90, 0x00
	full := bytes.Repeat(frame, 400)
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Range", "bytes 0-65535/166800")
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(full[:65536])
}
