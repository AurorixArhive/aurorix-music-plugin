package ikun

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 拿假 API 把整条链跑一遍: 搜索(官方) → 取直链(IKUN) → 探测。
func TestWyyChannelAgainstStubAPI(t *testing.T) {
	var audioURL, searchQuery, apiKey string
	qualities := []string{}
	payloadSeen := map[string]any{}

	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		searchQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"result":{"songs":[
			{"id":347230,"name":"海阔天空","duration":324000,"artists":[{"name":"Beyond"},{"name":"其他"}]},
			{"id":0,"name":"没有 id 的脏数据"}
		]}}`))
	})
	mux.HandleFunc("/url", func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("X-Api-Key")
		_ = json.NewDecoder(r.Body).Decode(&payloadSeen)
		quality, _ := payloadSeen["quality"].(string)
		qualities = append(qualities, quality)
		if quality == "320k" {
			_, _ = w.Write([]byte(`{"code":500,"message":"上游没给"}`)) // 一档不给 → 要能降档
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"成功","url":"` + audioURL + `"}`))
	})
	mux.HandleFunc("/audio", serveStubMP3)
	server := httptest.NewServer(mux)
	defer server.Close()
	audioURL = server.URL + "/audio"

	oldSearch, oldURL, oldBR := wySearchURL, ikunURL, ikunBR
	wySearchURL, ikunURL, ikunBR = server.URL+"/search", server.URL+"/url", []string{"320k", "128k"}
	defer func() { wySearchURL, ikunURL, ikunBR = oldSearch, oldURL, oldBR }()

	c := channelFor(t, "wyy.ikun")

	found, err := c.Search(context.Background(), "海阔天空", 5)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("脏数据没滤掉: %+v", found)
	}
	if found[0].ID != "347230" || found[0].Title != "海阔天空" || found[0].Artist != "Beyond/其他" {
		t.Errorf("解析不对: %+v", found[0])
	}
	if found[0].Duration != 324 { // 官方给毫秒, 要换成秒
		t.Errorf("时长没换算: %d", found[0].Duration)
	}
	if found[0].Channel != "wyy.ikun" || found[0].Platform != song.Wyy {
		t.Errorf("渠道/音源没标对: %+v", found[0])
	}
	if !strings.Contains(searchQuery, "type=1") || !strings.Contains(searchQuery, "limit=5") {
		t.Errorf("搜索参数不对: %q", searchQuery)
	}

	got, err := c.Resolve(context.Background(), found[0])
	if err != nil {
		t.Fatalf("取直链失败: %v", err)
	}
	if got.Audio != audioURL || got.Duration != 10 {
		t.Errorf("直链/时长不对: %+v", got)
	}
	if payloadSeen["source"] != "wy" || payloadSeen["musicId"] != "347230" {
		t.Errorf("IKUN 参数不对: %v", payloadSeen)
	}
	if len(qualities) != 2 || qualities[0] != "320k" || qualities[1] != "128k" {
		t.Errorf("没按降级链试: %v", qualities)
	}
	if apiKey != "" {
		t.Errorf("X-Api-Key 该是空的, 实际 %q", apiKey)
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
	full := bytes.Repeat(frame, 400)
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Range", "bytes 0-65535/166800")
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(full[:65536])
}
