package source

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// probeClient 只服务探测, 所以单独一条 http.Client: 它要"只读一小段就断开",
// 而 requests 那条路会把整段正文读进内存 —— 取 64KB 和取整个文件在这里是两回事。
var probeClient = &http.Client{Timeout: 15 * time.Second}

// probeUA 是探测时的 UA; 少数 CDN 对空 UA 直接 403。
const probeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// probeBytes 是一次探测最多拉多少: 足够装下 mp4 的 moov 和 mp3 的 ID3v2 + 首帧。
const probeBytes = 64 << 10

// Audio 是一次探测的结论。
type Audio struct {
	Playable bool  // 上游给的是音频, 不是一张 404 网页
	Size     int64 // 总字节; 0 表示上游没说
	Seconds  int   // 时长; 0 表示容器头里没读出来
}

// Inspect 摸一下这个直链: 是音频吗? 多长?
//
// 一次请求同时解决三件事 —— 可用性(响应类型)、总长(Content-Range)、时长(容器头)。
// 只读前 64KB 就断开, 所以 4MB 的歌和 40MB 的歌在这里开销一样。
// 计时不受 Range 支持与否影响: 上游忽略 Range 时也只是多读了 64KB。
func Inspect(rawURL string) (Audio, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return Audio{}, err
	}
	req.Header.Set("Range", "bytes=0-"+strconv.Itoa(probeBytes-1))
	req.Header.Set("User-Agent", probeUA)

	resp, err := probeClient.Do(req)
	if err != nil {
		return Audio{}, err
	}
	defer resp.Body.Close()

	head, _ := io.ReadAll(io.LimitReader(resp.Body, probeBytes))

	out := Audio{}
	if total, ok := contentRangeTotal(resp.Header.Get("Content-Range")); ok {
		out.Size = total
	} else if resp.StatusCode == http.StatusOK {
		out.Size = resp.ContentLength
	}
	out.Seconds = seconds(head, out.Size)

	kind := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if i := strings.IndexByte(kind, ';'); i >= 0 {
		kind = strings.TrimSpace(kind[:i])
	}
	// 认 audio/* 是主线; 少数 CDN 回 octet-stream, 那就以"容器头读得出时长"为准 ——
	// 网易把付费曲回成一张 200 的 404 网页, 类型是 text/html, 两条都过不了。
	out.Playable = resp.StatusCode < 400 &&
		(strings.HasPrefix(kind, "audio/") || (kind == "application/octet-stream" && out.Seconds > 0))
	return out, nil
}

// contentRangeTotal 从 "bytes 0-65535/3512553" 里取总长。
func contentRangeTotal(value string) (int64, bool) {
	_, total, ok := strings.Cut(value, "/")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64)
	return n, err == nil && n > 0
}

// seconds 按时长从容器头里读。mp4/m4a 读 mvhd, mp3 读首帧(有 Xing 就按帧数)。
func seconds(head []byte, size int64) int {
	if s, ok := mp4Seconds(head); ok {
		return s
	}
	return mp3Seconds(head, size)
}

// mp4Seconds 从 moov/mvhd 里取时长。选它是为了快: 直播外的成片都在头部放了 moov
// (faststart), 实测聚合站给的 m4a 全在头 64KB 内。
func mp4Seconds(head []byte) (int, bool) {
	for i := 0; i+8 <= len(head); {
		size, typ := box(head, i)
		if size < 8 {
			return 0, false
		}
		if typ == "moov" {
			return mvhdSeconds(head[i+8 : min(len(head), i+size)])
		}
		i += size
	}
	return 0, false
}

func mvhdSeconds(body []byte) (int, bool) {
	for i := 0; i+8 <= len(body); {
		size, typ := box(body, i)
		if size < 8 {
			return 0, false
		}
		if typ != "mvhd" {
			i += size
			continue
		}
		head := body[i+8 : min(len(body), i+size)]
		if len(head) < 20 {
			return 0, false
		}
		// v1 的创建/修改时间是 8 字节, 时间刻度与时长整体后移 12 字节。
		scaleAt, durationAt := 12, 16
		if head[0] == 1 {
			scaleAt, durationAt = 20, 24
			if len(head) < 28 {
				return 0, false
			}
		}
		scale := beUint32(head[scaleAt:])
		if scale == 0 {
			return 0, false
		}
		var duration uint64
		if head[0] == 1 {
			duration = beUint64(head[durationAt:])
		} else {
			duration = uint64(beUint32(head[durationAt:]))
		}
		return int(duration / uint64(scale)), true
	}
	return 0, false
}

func box(b []byte, i int) (int, string) {
	size := int(beUint32(b[i:]))
	if size == 1 {
		// 64 位长度: 我们只用到 moov/mvhd, 实际不会碰到, 能跳过就行。
		if i+16 > len(b) {
			return 0, ""
		}
		return int(beUint64(b[i+8:])), string(b[i+4 : i+8])
	}
	return size, string(b[i+4 : i+8])
}

func beUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func beUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return uint64(beUint32(b))<<32 | uint64(beUint32(b[4:]))
}

// mp3 的比特率表, 按 [MPEG 版本][层] 取。索引 0 是"保留", 视为无效帧。
var (
	// 行: MPEG1 / MPEG2 / MPEG2.5; 列: Layer I / II / III
	mp3Bitrates = [3][3][15]int{}
	mp3Rates    = [3][3]int{{44100, 48000, 32000}, {22050, 24000, 16000}, {11025, 12000, 8000}}
)

func init() {
	mpeg1 := [3][15]int{
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	}
	mpeg2 := [3][15]int{
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	}
	mp3Bitrates[0] = mpeg1
	mp3Bitrates[1] = mpeg2
	mp3Bitrates[2] = mpeg2
}

// mp3Seconds 从 mp3 首帧算时长: 有 Xing/Info 头就按它给的帧数(变比特率也准),
// 没有就按首帧的比特率当常数估算。
func mp3Seconds(head []byte, size int64) int {
	start := id3Size(head)
	for i := start; i+4 <= len(head); i++ {
		if head[i] != 0xFF || head[i+1]&0xE0 != 0xE0 {
			continue
		}
		version := int(head[i+1]>>3) & 0x03
		layer := int(head[i+1]>>1) & 0x03
		if version == 1 || layer == 0 {
			continue
		}
		brIdx := int(head[i+2]>>4) & 0x0F
		srIdx := int(head[i+2]>>2) & 0x03
		if brIdx == 0 || brIdx == 15 || srIdx == 3 {
			continue
		}
		// version: 3=MPEG1, 2=MPEG2, 0=MPEG2.5
		row := 0
		if version == 2 {
			row = 1
		} else if version == 0 {
			row = 2
		}
		// layer: 3=Layer I, 2=Layer II, 1=Layer III, 正好是比特率表的倒序索引。
		rate := mp3Rates[row][srIdx]
		bitrate := mp3Bitrates[row][3-layer][brIdx] * 1000
		if rate == 0 || bitrate == 0 {
			return 0
		}
		if frames, ok := xingFrames(head[i:], version, head[i+3]>>6 == 3); ok {
			per := 1152
			if version != 3 {
				per = 576
			}
			return frames * per / rate
		}
		if size <= 0 {
			return 0
		}
		return int((size - int64(start)) * 8 / int64(bitrate))
	}
	return 0
}

// xingFrames 读 Xing/Info 头里的总帧数; 没有返回 false。
func xingFrames(frame []byte, version int, mono bool) (int, bool) {
	at := 4 + sideInfo(version, mono)
	if at+8 > len(frame) {
		return 0, false
	}
	tag := string(frame[at : at+4])
	if tag != "Xing" && tag != "Info" {
		return 0, false
	}
	flags := beUint32(frame[at+4:])
	if flags&1 == 0 || at+12 > len(frame) {
		return 0, false
	}
	return int(beUint32(frame[at+8:])), true
}

// sideInfo 是 Xing/Info 头在一帧里的偏移: 跟着 MPEG 版本与声道数走。
func sideInfo(version int, mono bool) int {
	switch {
	case version == 3 && mono:
		return 17
	case version == 3:
		return 32
	case mono:
		return 9
	default:
		return 17
	}
}

// id3Size 算出 ID3v2 头声明的长度(含头), 用来跳过标签找音频帧。
func id3Size(head []byte) int {
	if len(head) < 10 || string(head[:3]) != "ID3" {
		return 0
	}
	// 标签长度是三个 7 位字节组成的 syncsafe 整数。
	size := int(head[6])<<21 | int(head[7])<<14 | int(head[8])<<7 | int(head[9])
	return 10 + size
}
