package yunxi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 拿假 API 跑一遍: 按歌名+歌手要直链 → 探测。
func TestResolveAgainstStubAPI(t *testing.T) {
	var audioURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio" {
			serveStubMP3(w, r)
			return
		}
		if got := r.URL.Query().Get("msg"); got != "晴天 周杰伦" {
			http.Error(w, "关键词不对: "+got, http.StatusBadRequest)
			return
		}
		if got := r.URL.Query().Get("type"); got != "1" {
			http.Error(w, "type 不对: "+got, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"获取成功","data":{"song":"晴天","singer":"周杰伦","music":"` + audioURL + `"}}`))
	}))
	defer server.Close()
	audioURL = server.URL + "/audio"

	oldAPI := yunxiAPI
	yunxiAPI = server.URL
	defer func() { yunxiAPI = oldAPI }()

	got, err := yunxiResolve(context.Background(), song.Song{Platform: song.QQ, ID: "0039MnYb0qxYhV", Title: "晴天", Artist: "周杰伦"})
	if err != nil {
		t.Fatalf("取直链失败: %v", err)
	}
	if got.Audio != audioURL || got.Duration != 10 || got.Channel != yunxiID {
		t.Errorf("直链/时长/渠道不对: %+v", got)
	}
}

// 按歌名查会撞上翻唱, 所以名字对不上必须失败, 让别的渠道接手。
func TestResolveRejectsAnotherSong(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"song":"晴天","singer":"Lucky小爱","music":"http://a/b.mp3"}}`))
	}))
	defer server.Close()

	oldAPI := yunxiAPI
	yunxiAPI = server.URL
	defer func() { yunxiAPI = oldAPI }()

	if _, err := yunxiResolve(context.Background(), song.Song{Title: "晴天", Artist: "周杰伦"}); err == nil {
		t.Fatal("歌手对不上时该报错")
	}
}

func TestSameSongTolerUpstreamSingerList(t *testing.T) {
	if !sameSong("晴天", "周杰伦、费玉清", "晴天", "周杰伦,费玉清") {
		t.Error("歌手串的分隔符不同也该算同一首")
	}
	if sameSong("晴天", "周杰伦", "晴天", "Lucky小爱") {
		t.Error("不同歌手不该算同一首")
	}
	if sameSong("晴天", "周杰伦", "搁浅", "周杰伦") {
		t.Error("不同歌名不该算同一首")
	}
}

func TestResolveNeedsTitle(t *testing.T) {
	if _, err := yunxiResolve(context.Background(), song.Song{Platform: song.QQ, ID: "0039MnYb0qxYhV"}); err == nil {
		t.Fatal("只有 id 时该报错")
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
