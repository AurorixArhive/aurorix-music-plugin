package nianxin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 念心会 302 到真正的音频, 所以要跟到最后一跳再拿去探测。
func TestResolveFollowsRedirect(t *testing.T) {
	var audioURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio" {
			serveStubMP3(w, r)
			return
		}
		if got := r.URL.Query().Get("id"); got != "0039MnYb0qxYhV" {
			http.Error(w, "id 不对: "+got, http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, audioURL, http.StatusFound)
	}))
	defer server.Close()
	audioURL = server.URL + "/audio"

	oldAPI := nianxinAPI
	nianxinAPI = server.URL + "/tx.php"
	defer func() { nianxinAPI = oldAPI }()

	got, err := nianxinResolve(context.Background(), song.Song{Platform: song.QQ, ID: "0039MnYb0qxYhV"})
	if err != nil {
		t.Fatalf("取直链失败: %v", err)
	}
	if got.Audio != audioURL || got.Duration != 10 || got.Channel != nianxinID {
		t.Errorf("该落到重定向后的地址: %+v", got)
	}
}

func TestResolveFailsWhenUpstreamIsNotAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html>404</html>`))
	}))
	defer server.Close()

	oldAPI := nianxinAPI
	nianxinAPI = server.URL
	defer func() { nianxinAPI = oldAPI }()

	if _, err := nianxinResolve(context.Background(), song.Song{ID: "x"}); err == nil {
		t.Fatal("上游给网页时该报错")
	}
}

func TestResolveNeedsSongmid(t *testing.T) {
	if _, err := nianxinResolve(context.Background(), song.Song{Platform: song.QQ}); err == nil {
		t.Fatal("缺 songmid 时该报错")
	}
}

// serveStubMP3 回一段能过探测的假音频: MPEG1 Layer3 128k 的帧, 帧长 417 字节。
func serveStubMP3(w http.ResponseWriter, r *http.Request) {
	frame := make([]byte, 417)
	frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFB, 0x90, 0x00
	full := bytes.Repeat(frame, 400)
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Range", "bytes 0-65535/166800")
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(full[:65536])
}
