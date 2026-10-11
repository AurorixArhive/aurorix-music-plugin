package molan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 两条路都跑一遍: 海棠湾(墨澜那条)先给链就收工; 它不给才轮到官方 getSongInfo。
func TestKugouResolveLadder(t *testing.T) {
	var audioURL string
	officialCalls := 0
	calls := []string{}

	mux := http.NewServeMux()
	mux.HandleFunc("/resolve-url", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "haitangw")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"url":"` + audioURL + `"}}`))
	})
	mux.HandleFunc("/getSongInfo.php", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "official")
		officialCalls++
		_, _ = w.Write([]byte(`{"url":"` + audioURL + `","timeLength":269,"errcode":0}`))
	})
	mux.HandleFunc("/audio", serveStubMP3)
	server := httptest.NewServer(mux)
	defer server.Close()
	audioURL = server.URL + "/audio"

	oldAPI, oldInfo := haitangwResolveAPI, kugouInfoURL
	haitangwResolveAPI = server.URL + "/resolve-url"
	kugouInfoURL = server.URL + "/getSongInfo.php?cmd=playInfo"
	defer func() { haitangwResolveAPI, kugouInfoURL = oldAPI, oldInfo }()

	c := channelFor(t, "kg.molan")
	pick := song.Song{Platform: song.Kugou, ID: "HASH123|79360569", Title: "晴天"}

	got, err := c.Resolve(context.Background(), pick)
	if err != nil {
		t.Fatalf("取直链失败: %v", err)
	}
	if got.Audio != audioURL || got.Channel != "kg.molan" {
		t.Errorf("结果不对: %+v", got)
	}
	if officialCalls != 0 {
		t.Errorf("海棠湾给了链就不该再打官方接口, 实际打了 %d 次", officialCalls)
	}
}

func TestKugouFallsBackToOfficial(t *testing.T) {
	var audioURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/resolve-url", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":500,"message":"上游挂了","data":{"url":""}}`))
	})
	mux.HandleFunc("/getSongInfo.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"` + audioURL + `","timeLength":269,"errcode":0}`))
	})
	mux.HandleFunc("/audio", serveStubMP3)
	server := httptest.NewServer(mux)
	defer server.Close()
	audioURL = server.URL + "/audio"

	oldAPI, oldInfo := haitangwResolveAPI, kugouInfoURL
	haitangwResolveAPI = server.URL + "/resolve-url"
	kugouInfoURL = server.URL + "/getSongInfo.php?cmd=playInfo"
	defer func() { haitangwResolveAPI, kugouInfoURL = oldAPI, oldInfo }()

	got, err := channelFor(t, "kg.molan").Resolve(context.Background(), song.Song{Platform: song.Kugou, ID: "HASH123"})
	if err != nil {
		t.Fatalf("兜底也没成: %v", err)
	}
	if got.Audio != audioURL {
		t.Errorf("直链不对: %+v", got)
	}
}

func TestKugouSearchParsesHits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"lists":[
			{"FileHash":"AAA","AlbumID":"79360569","SongName":"晴天","SingerName":"周杰伦","Duration":269},
			{"FileHash":"","AlbumID":"1","SongName":"没 hash","SingerName":"x","Duration":0}
		]}}`))
	}))
	defer server.Close()

	oldSearch := kugouSearchURL
	kugouSearchURL = server.URL + "/song_search_v2?page=1"
	defer func() { kugouSearchURL = oldSearch }()

	found, err := channelFor(t, "kg.molan").Search(context.Background(), "晴天", 5)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("脏数据没滤掉: %+v", found)
	}
	// id 要把 album_id 一起带上, 官方接口取链要用
	if found[0].ID != "AAA|79360569" || found[0].Duration != 269 || found[0].Title != "晴天" {
		t.Errorf("解析不对: %+v", found[0])
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
func serveStubMP3(w http.ResponseWriter, r *http.Request) {
	frame := make([]byte, 417)
	frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFB, 0x90, 0x00
	full := make([]byte, 0, 417*400)
	for i := 0; i < 400; i++ {
		full = append(full, frame...)
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Range", "bytes 0-65535/166800")
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(full[:65536])
}
