package haitangw

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
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
	if got.Grade != quality.Usable {
		t.Errorf("普通 mp3 该是能播: %+v", got)
	}
	if seen["source"] != "tx" || seen["rid"] != "0039MnYb0qxYhV" || seen["level"] != haitangwLevels[0] {
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

// 用户实际踩到的那条: 上游回的是酷我的 .mflac(加密)。以前这里直接判死, 用户什么都拿不到;
// 现在原样发出去, 只把加密标记带上, 让调度知道它是次优结果。
func TestResolveKeepsEncryptedLink(t *testing.T) {
	var audioURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio.mflac" {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Range", "bytes 0-65535/18180574")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("R.\x0d+v465N$3{Dyq.l"))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"url":"` + audioURL + `"}}`))
	}))
	defer server.Close()
	audioURL = server.URL + "/audio.mflac"

	oldAPI := haitangwAPI
	haitangwAPI = server.URL
	defer func() { haitangwAPI = oldAPI }()

	got, err := haitangwResolve(context.Background(), song.Song{Platform: song.QQ, ID: "0039MnYb0qxYhV"})
	if err != nil {
		t.Fatalf("加密文件不该判死: %v", err)
	}
	if got.Grade != quality.Degraded {
		t.Errorf("加密文件该是降级而不是判死: %+v", got)
	}
	if got.Audio != audioURL {
		t.Errorf("直链 = %q, 期望 %q", got.Audio, audioURL)
	}
}
