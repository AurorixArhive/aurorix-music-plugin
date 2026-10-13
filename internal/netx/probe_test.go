package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
)

// FLAC 必须走 STREAMINFO: 否则后面的 mp3 扫描会在 FLAC 数据里撞上假同步字,
// 把 5 分钟的歌算成 61 分钟(真踩过)。
func TestFlacSecondsReadsStreamInfo(t *testing.T) {
	head := make([]byte, 64)
	copy(head, "fLaC")
	head[4] = 0x00 // 第一块, 类型 0 = STREAMINFO
	head[5], head[6], head[7] = 0x00, 0x00, 34
	// 采样率 44100 (20 位), 总采样 14376600 (36 位)
	put20(head[18:], 44100)
	put36(head[21:], 14376600)

	got, ok := flacSeconds(head)
	if !ok || got != 326 {
		t.Fatalf("flacSeconds = %d(%v), want 326", got, ok)
	}
}

func TestFlacSecondsRejectsNonFlac(t *testing.T) {
	if _, ok := flacSeconds([]byte("ID3\x04\x00\x00\x00\x00\x00\x00xxxxxxxxxxxxxxxxxxxxxx")); ok {
		t.Error("不是 FLAC 却认了")
	}
}

func TestSecondsFallsBackAndCapsGarbage(t *testing.T) {
	// 一帧 MPEG1 Layer3 128k 44.1k, 帧长 417; 拼 400 帧 = 166800 字节 → 约 10 秒
	frame := make([]byte, 417)
	frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFB, 0x90, 0x00
	head := make([]byte, 0, 65536)
	for i := 0; i < 157; i++ {
		head = append(head, frame...)
	}
	if got := seconds(head, 166800); got != 10 {
		t.Errorf("mp3 时长 = %d, want 10", got)
	}

	// 一段随便的字节: mp3 扫描会读出个天文数字, 上限要把它压成 0
	junk := make([]byte, 4096)
	for i := range junk {
		junk[i] = byte(i * 7)
	}
	junk[0], junk[1], junk[2], junk[3] = 0xFF, 0xFB, 0x90, 0x00
	if got := seconds(junk, 1<<40); got > 6*3600 {
		t.Errorf("离谱的时长没被压掉: %d", got)
	}
}

func put20(dst []byte, value int) {
	dst[0] = byte(value >> 12)
	dst[1] = byte(value >> 4)
	dst[2] = byte((value & 0x0F) << 4)
}

func put36(dst []byte, value int64) {
	dst[0] = byte((value >> 32) & 0x0F)
	dst[1] = byte(value >> 24)
	dst[2] = byte(value >> 16)
	dst[3] = byte(value >> 8)
	dst[4] = byte(value)
}

// octet-stream 的活链就是能播: 读不出时长也不算坏链(酷我的加密曲回到这儿就是这种)。
func TestInspectAcceptsOctetStream(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Range", "bytes 0-65535/18180574")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("R.\x0d+v465N$3{Dyq.l"))
	}))
	defer big.Close()

	got, err := Inspect(big.URL)
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	if got.Grade != quality.Usable {
		t.Errorf("活链被判成不可用: %+v", got)
	}
}

// 正文是空的, 不管挂着什么类型都不算媒体。
func TestInspectRejectsEmptyBody(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer empty.Close()

	got, err := Inspect(empty.URL)
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	if got.Grade != quality.Unusable {
		t.Errorf("空正文被当成媒体: %+v", got)
	}
}

// 上游有时拿 JSON 当报错正文, 这个也不能认。
func TestInspectRejectsJSONError(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":400,"message":"no resource"}`))
	}))
	defer bad.Close()

	got, err := Inspect(bad.URL)
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	if got.Grade != quality.Unusable {
		t.Errorf("JSON 报错被当成媒体: %+v", got)
	}
}

// 网易把付费曲回成一张 200 的 404 网页, 类型是 text/html —— 体积再大也不算媒体。
func TestInspectRejectsErrorPage(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Range", "bytes 0-99999/100000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("<html>404</html>"))
	}))
	defer page.Close()

	got, err := Inspect(page.URL)
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	if got.Grade != quality.Unusable {
		t.Errorf("网页被当成媒体: %+v", got)
	}
}

// 酷我的 .mflac/.mgg 是加密格式: 链是活的、发得出去, 只是播放器多半打不开 ——
// 那是降级, 不是不可用, 也不是"能直接播"。
func TestInspectMarksEncryptedFormats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Range", "bytes 0-65535/18180574")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("R.\x0d+v465N$3{Dyq.l"))
	}))
	defer srv.Close()

	got, err := Inspect(srv.URL + "/Q0M00044t2fe2.mflac?vkey=abc")
	if err != nil {
		t.Fatalf("加密格式不该判死: %v", err)
	}
	if got.Grade != quality.Degraded {
		t.Fatalf("加密格式该是降级, 实得 %+v", got)
	}
	if got.Size != 18180574 {
		t.Errorf("体积 = %d, 期望 18180574", got.Size)
	}

	plain, err := Inspect(srv.URL + "/song.mp3")
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	if plain.Grade != quality.Usable {
		t.Errorf("普通后缀该是能播, 实得 %+v", plain)
	}
}

// 后缀识别本身: 带 query 的也算, 普通格式不能误伤。
func TestEncryptedDetectsExtensions(t *testing.T) {
	for _, u := range []string{"https://x/y.mgg", "https://x/y.mflac0", "https://x/y.mgg1?a=1"} {
		if encrypted(u) == "" {
			t.Errorf("没认出加密格式: %s", u)
		}
	}
	for _, u := range []string{"https://x/y.mp3", "https://x/y.flac", "https://x/y.m4a"} {
		if encrypted(u) != "" {
			t.Errorf("普通格式被当成加密: %s", u)
		}
	}
}
