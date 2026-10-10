package source

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
)

// boxOf 拼一个 mp4 box: 4 字节长度 + 4 字节类型 + 正文。
func boxOf(kind string, body []byte) []byte {
	out := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	copy(out[4:8], kind)
	copy(out[8:], body)
	return out
}

// mvhdOf 造一个 mvhd: version 0 用 4 字节时间刻度与时长, version 1 用 8 字节。
func mvhdOf(version byte, timescale uint32, duration uint64) []byte {
	size := 100
	if version == 1 {
		size = 112
	}
	body := make([]byte, size)
	body[0] = version
	if version == 1 {
		binary.BigEndian.PutUint32(body[20:], timescale)
		binary.BigEndian.PutUint64(body[24:], duration)
	} else {
		binary.BigEndian.PutUint32(body[12:], timescale)
		binary.BigEndian.PutUint32(body[16:], uint32(duration))
	}
	return boxOf("mvhd", body)
}

// faststart 的 m4a 把 moov 放在文件开头, 所以前 64KB 就够算了 —— 聚合站给的都是这种。
func TestMP4SecondsReadsMoovAtTheFront(t *testing.T) {
	head := append(boxOf("ftyp", []byte("mp42")), boxOf("moov", mvhdOf(0, 600, 120000))...)
	if got, ok := mp4Seconds(head); !ok || got != 200 {
		t.Fatalf("mp4Seconds = %d,%v; want 200,true", got, ok)
	}
}

func TestMP4SecondsReadsVersionOneHeader(t *testing.T) {
	head := append(boxOf("ftyp", []byte("mp42")), boxOf("moov", mvhdOf(1, 44100, 44100*95))...)
	if got, ok := mp4Seconds(head); !ok || got != 95 {
		t.Fatalf("mp4Seconds = %d,%v; want 95,true", got, ok)
	}
}

// moov 还没到就断了(非 faststart 或窗口太小): 宁可说不知道, 也别瞎猜一个时长。
func TestMP4SecondsGivesUpWhenMoovIsMissing(t *testing.T) {
	head := boxOf("ftyp", []byte("mp42"))
	if got, ok := mp4Seconds(head); ok {
		t.Fatalf("没有 moov 却算出了 %d 秒", got)
	}
}

// 128kbps / 44.1kHz 的 MPEG1 Layer III 首帧: 总长 1MB 就是 62 秒。
func TestMP3SecondsFromConstantBitrate(t *testing.T) {
	head := append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 64)...)
	if got := mp3Seconds(head, 1_000_000); got != 62 {
		t.Fatalf("mp3Seconds = %d, want 62", got)
	}
}

// 变比特率的歌靠 Xing 里的总帧数算, 按首帧比特率外推会差出几十秒。
func TestMP3SecondsPrefersXingFrameCount(t *testing.T) {
	head := make([]byte, 200)
	copy(head, []byte{0xFF, 0xFB, 0x90, 0x00})
	at := 4 + 32 // MPEG1 立体声的边信息长度
	copy(head[at:], "Xing")
	binary.BigEndian.PutUint32(head[at+4:], 1)    // flags: 有帧数
	binary.BigEndian.PutUint32(head[at+8:], 5000) // 帧数

	if got := mp3Seconds(head, 10_000_000); got != 130 {
		t.Fatalf("mp3Seconds = %d, want 130(5000*1152/44100)", got)
	}
}

func TestID3Size(t *testing.T) {
	head := make([]byte, 64)
	copy(head, "ID3")
	head[3], head[4] = 3, 0
	head[6], head[7], head[8], head[9] = 0, 0, 0x02, 0x00 // syncsafe 256
	if got := id3Size(head); got != 266 {
		t.Fatalf("id3Size = %d, want 266", got)
	}
	if got := id3Size([]byte{0xFF, 0xFB, 0x90, 0x00}); got != 0 {
		t.Fatalf("没有 ID3 时 id3Size = %d, want 0", got)
	}
}

func TestContentRangeTotal(t *testing.T) {
	if got, ok := contentRangeTotal("bytes 0-65535/3512553"); !ok || got != 3512553 {
		t.Fatalf("got %d,%v", got, ok)
	}
	if _, ok := contentRangeTotal("*"); ok {
		t.Fatal("拿不到总长时该说不知道")
	}
}

// 探测一次就同时回答三件事: 是不是音频、总长多少、多长。
func TestInspectReadsAudioAndDurationInOneShot(t *testing.T) {
	body := append(boxOf("ftyp", []byte("mp42")), boxOf("moov", mvhdOf(0, 600, 120000))...)
	srv := newProbeServer(t, "audio/mp4", body, 3512553)

	got, err := Inspect(srv.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got.Playable {
		t.Fatal("audio/mp4 该判成可播")
	}
	if got.Size != 3512553 {
		t.Fatalf("总长 = %d, want 3512553(来自 Content-Range)", got.Size)
	}
	if got.Seconds != 200 {
		t.Fatalf("时长 = %d, want 200", got.Seconds)
	}
}

// 网易把付费曲回成一张 200 的 404 网页 —— 必须判成"不可播", 否则用户会收到一个 HTML。
func TestInspectRejectsTheTencentPageInsteadOfAudio(t *testing.T) {
	srv := newProbeServer(t, "text/html;charset=utf8", []byte("<html>404</html>"), 107191)

	got, err := Inspect(srv.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Playable {
		t.Fatalf("text/html 不该判成可播: %+v", got)
	}
}

// 上游不认 Range(回 200 整份)时也不能算错, 只是总长从 Content-Length 拿。
func TestInspectToleratesServersWithoutRangeSupport(t *testing.T) {
	body := append(boxOf("ftyp", []byte("mp42")), boxOf("moov", mvhdOf(0, 600, 60000))...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mp4")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := Inspect(srv.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got.Playable || got.Seconds != 100 {
		t.Fatalf("got = %+v, want 可播且 100 秒", got)
	}
}

func newProbeServer(t *testing.T, contentType string, body []byte, total int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-65535/"+itoa(total))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
