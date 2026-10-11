package haitangw

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 拿假 API 跑一遍: 按 songmid 要直链 → 探测。
func TestResolveAgainstStubAPI(t *testing.T) {
	var audioURL string
	var seen map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio" {
			serveStubMP3(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&seen)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"url":"` + audioURL + `"}}`))
	}))
	defer server.Close()
	audioURL = server.URL + "/audio"

	oldAPI := haitangwAPI
	haitangwAPI = server.URL
	defer func() { haitangwAPI = oldAPI }()

	got, err := haitangwResolve(context.Background(), song.Song{Platform: song.QQ, ID: "0039MnYb0qxYhV"})
	if err != nil {
		t.Fatalf("取直链失败: %v", err)
	}
	if got.Audio != audioURL || got.Duration != 10 || got.Channel != haitangwID {
		t.Errorf("直链/时长/渠道不对: %+v", got)
	}
	if seen["source"] != "tx" || seen["rid"] != "0039MnYb0qxYhV" || seen["level"] != haitangwLevel {
		t.Errorf("请求体不对: %v", seen)
	}
}

// code 出错时上游给的是字符串, 不能被它带崩成 JSON 解析错误。
func TestResolveToleratesStringCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"UPSTREAM_NO_RESOURCE","message":"播放地址解析失败","data":null}`))
	}))
	defer server.Close()

	oldAPI := haitangwAPI
	haitangwAPI = server.URL
	defer func() { haitangwAPI = oldAPI }()

	_, err := haitangwResolve(context.Background(), song.Song{ID: "x"})
	if err == nil {
		t.Fatal("没给直链时该报错")
	}
	if !strings.Contains(err.Error(), "没给直链") {
		t.Errorf("错误信息该是没给直链, 实际: %v", err)
	}
}

func TestResolveNeedsSongmid(t *testing.T) {
	if _, err := haitangwResolve(context.Background(), song.Song{Platform: song.QQ}); err == nil {
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
