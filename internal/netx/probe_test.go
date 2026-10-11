package netx

import "testing"

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
