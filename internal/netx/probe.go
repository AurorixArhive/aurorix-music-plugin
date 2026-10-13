package netx

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
)

// probeHTTP 只服务探测, 所以单独一条 http.Client: 它要"只读一小段就断开",
// 而 requests 那条路会把整段正文读进内存 —— 取 64KB 和取整个文件在这里是两回事。
var probeHTTP = &http.Client{Timeout: 15 * time.Second}

// probeUA 是探测时的 UA; 少数 CDN 对空 UA 直接 403。
const probeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// probeBytes 是一次探测最多拉多少: 足够装下 mp4 的 moov 和 mp3 的 ID3v2 + 首帧。
const probeBytes = 64 << 10

// Audio 是一次探测的结论。
type Audio struct {
	// Grade 是这份文件能用到什么程度; 判级规则只有 grade() 一处。
	Grade   quality.Grade
	Size    int64 // 总字节; 0 表示上游没说
	Seconds int   // 时长; 0 表示容器头里没读出来
	// Why 是判级的原因, 只进日志 —— 用户那边永远只会看到一句"获取失败"。
	Why string
}

// Inspect 摸一下这个直链: 是音频吗? 多长? 能直接播吗?
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

	resp, err := probeHTTP.Do(req)
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
	out.Grade, out.Why = grade(resp.StatusCode, head, resp.Header.Get("Content-Type"), rawURL)
	return out, nil
}

// grade 是"这份东西能用到什么程度"的唯一判断点 —— 以后再加一种坏链形态只改这里。
//
// 判"是不是媒体"只认硬信号, 不做正面证明: 状态码不对、正文是空的、或者类型一眼就不是
// 媒体(text/html、application/json) 就直接否, 其余一律当成媒体。网易那种 200 的假 404
// 是 text/html, 这里挡得住。
//
// 确认是媒体之后才问"能不能直接用": 加密格式能发、用户打不开, 那是降级而不是不可用 ——
// 挑不到更好的时候, 一份打不开的文件也比一句"获取失败"强。
func grade(status int, head []byte, contentType, rawURL string) (quality.Grade, string) {
	switch {
	case status >= 400:
		return quality.Unusable, fmt.Sprintf("上游回了 %d", status)
	case len(head) == 0:
		return quality.Unusable, "正文是空的"
	}
	kind := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(kind, ';'); i >= 0 {
		kind = strings.TrimSpace(kind[:i])
	}
	if notMedia(kind) {
		return quality.Unusable, "正文是 " + kind + ", 不是媒体"
	}
	if format := encrypted(rawURL); format != "" {
		return quality.Degraded, "加密格式 " + format + ", 播放器多半打不开"
	}
	return quality.Usable, ""
}

// encrypted 认酷我的加密格式并返回是哪种; 不是就返回空串。
// 文件是真的、链是活的, 只是内容加过密, 谁打开都只会说"文件损坏"。
func encrypted(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	ext := strings.ToLower(path.Ext(rawURL))
	switch ext {
	case ".mflac", ".mgg", ".mflac0", ".mgg1":
		return ext
	}
	return ""
}

// notMedia 是"一眼就知道不是音频"的类型: 报错网页和 JSON 都属于这类。
func notMedia(kind string) bool {
	return strings.HasPrefix(kind, "text/") || kind == "application/json" || kind == "application/xml"
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

// seconds 按时长从容器头里读: FLAC 读 STREAMINFO, mp4/m4a 读 mvhd, mp3 读首帧(有 Xing 就按帧数)。
func seconds(head []byte, size int64) int {
	if s, ok := flacSeconds(head); ok {
		return s
	}
	if s, ok := mp4Seconds(head); ok {
		return s
	}
	// mp3 是"扫同步字"的启发式: 撞上别的容器会读出离谱的时长, 超过 6 小时一律当没读出来。
	if s := mp3Seconds(head, size); s > 0 && s <= 6*3600 {
		return s
	}
	return 0
}

// flacSeconds 从 STREAMINFO 里取时长: 采样率 20 位、总采样数 36 位, 位置都是定死的。
// 不认它的话 mp3 那条启发式会在 FLAC 数据里撞上假同步字(实测把 5 分钟的歌算成 61 分钟)。
func flacSeconds(head []byte) (int, bool) {
	if len(head) < 26 || string(head[:4]) != "fLaC" || head[4]&0x7F != 0 {
		return 0, false
	}
	rate := int(head[18])<<12 | int(head[19])<<4 | int(head[20])>>4
	total := int64(head[21]&0x0F)<<32 | int64(head[22])<<24 | int64(head[23])<<16 | int64(head[24])<<8 | int64(head[25])
	if rate <= 0 || total <= 0 {
		return 0, false
	}
	return int(total / int64(rate)), true
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
		// ID3 里混着假的同步字节: 单看一个头会把比特率读成个位数、算成几十小时的时长。
		// 真帧的下一帧头必须正好落在按帧长算出的位置上。
		padding := int(head[i+2]>>1) & 0x01
		frameLen := 144*bitrate/rate + padding
		if version != 3 {
			frameLen = 72*bitrate/rate + padding
		}
		if i+frameLen+4 > len(head) || head[i+frameLen] != 0xFF || head[i+frameLen+1]&0xE0 != 0xE0 {
			continue
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
