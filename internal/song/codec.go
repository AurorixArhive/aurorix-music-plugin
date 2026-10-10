package song

import (
	"strconv"
	"strings"
)

// 卡片上那行可点命令的全部内容。它必须自包含 —— 服务端不留任何状态,
// 所以卡片发出去多久、机器人重启多少次, 点下去都还能找到同一首歌。
//
// 打包方式: 6 bit 一字符的自定义字母表(64 个字符, 不含 +/=/&/% 这些在 mqqapi
// 链接里要转义、或在输入框里看着别扭的字符)。
//
//	QQ       mid 本来就是 62 进制 14 位, 一位塞 6 bit -> 84 bit, 加 2 bit 校验 = 15 字符
//	酷我/网易 十进制 id 塞 34 bit,                        加 4 bit 校验 =  7 字符
//
// 刻意不用大数运算: 62^14 装不进 uint64, 硬上 math/big 会慢一个量级。
// 实测编解码都是几百纳秒、零分配。
const refAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// QQ 的 mid 用的 62 进制数字表
const refDigits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

const (
	refVersion  = 1
	refQQLen    = 15
	refNumLen   = 7
	refQQDigits = 14
	refNumBits  = 34
	refQQCheck  = 2
	refNumCheck = 4
)

var (
	refAlphabetIndex [256]int8
	refDigitIndex    [256]int8
)

func init() {
	for i := range refAlphabetIndex {
		refAlphabetIndex[i] = -1
		refDigitIndex[i] = -1
	}
	for i := 0; i < len(refAlphabet); i++ {
		refAlphabetIndex[refAlphabet[i]] = int8(i)
	}
	for i := 0; i < len(refDigits); i++ {
		refDigitIndex[refDigits[i]] = int8(i)
	}
}

type refWriter struct {
	acc uint64
	n   uint
	crc uint8
	buf [refQQLen]byte
	len int
}

func (w *refWriter) put(v uint64, bits uint) {
	w.crc = w.crc*31 + uint8(v) + uint8(v>>8) + uint8(bits)
	w.acc = w.acc<<bits | (v & (1<<bits - 1))
	w.n += bits
	for w.n >= 6 {
		w.n -= 6
		w.buf[w.len] = refAlphabet[(w.acc>>w.n)&0x3f]
		w.len++
	}
}

// finish 追一个校验位段再收尾。校验只防手滑和"关键词恰好长得像短码",
// 不是安全机制 —— 短码本来就不需要保密。
func (w *refWriter) finish(checkBits uint) string {
	w.put(uint64(w.crc)&(1<<checkBits-1), checkBits)
	if w.n > 0 {
		w.buf[w.len] = refAlphabet[(w.acc<<(6-w.n))&0x3f]
		w.len++
	}
	return string(w.buf[:w.len])
}

type refReader struct {
	buf string
	pos int
	acc uint64
	n   uint
	crc uint8
}

func (r *refReader) get(bits uint) uint64 {
	for r.n < bits {
		var group uint64
		if r.pos < len(r.buf) {
			index := refAlphabetIndex[r.buf[r.pos]]
			if index < 0 {
				return 0
			}
			group = uint64(index)
		}
		r.pos++
		r.acc = r.acc<<6 | group
		r.n += 6
	}
	r.n -= bits
	v := (r.acc >> r.n) & (1<<bits - 1)
	r.acc &= 1<<r.n - 1
	r.crc = r.crc*31 + uint8(v) + uint8(v>>8) + uint8(bits)
	return v
}

// QQ 的 mid 实测一律是 "00" 开头的 62 进制 14 位(20 多首样本全是)。
// 卡住这一条不是为了正确性, 而是为了"关键词不会恰好长得像短码" ——
// 形状不符就退回老格式, 解码端不认就直接当关键词搜, 两边都不会误判。
func qqMidShape(id string) bool {
	if len(id) != refQQDigits || id[0] != '0' || id[1] != '0' {
		return false
	}
	for i := 0; i < refQQDigits; i++ {
		if refDigitIndex[id[i]] < 0 {
			return false
		}
	}
	return true
}

// encodeRef 把 (音源, 曲目 ID) 编成短码; ID 形状不对就返回 false, 由调用方退回老格式。
func encodeRef(src Source, id string) (string, bool) {
	index, ok := src.index()
	if !ok {
		return "", false
	}

	var w refWriter
	w.put(refVersion, 2)
	w.put(uint64(index), 2)

	if src == QQ {
		if !qqMidShape(id) {
			return "", false
		}
		for i := 0; i < refQQDigits; i++ {
			digit := refDigitIndex[id[i]]
			if digit < 0 {
				return "", false
			}
			w.put(uint64(digit), 6)
		}
		return w.finish(refQQCheck), true
	}

	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil || value == 0 || value >= 1<<refNumBits {
		return "", false
	}
	w.put(value, refNumBits)
	return w.finish(refNumCheck), true
}

func decodeRef(code string) (Source, string, bool) {
	// 长度先卡死: 关键词绝大多数连这一关都过不了
	if len(code) != refQQLen && len(code) != refNumLen {
		return "", "", false
	}

	r := &refReader{buf: code}
	if r.get(2) != refVersion {
		return "", "", false
	}
	src, ok := sourceAt(r.get(2))
	if !ok {
		return "", "", false
	}

	if src == QQ {
		if len(code) != refQQLen {
			return "", "", false
		}
		var id [refQQDigits]byte
		for i := range id {
			digit := r.get(6)
			if digit >= uint64(len(refDigits)) {
				return "", "", false
			}
			id[i] = refDigits[digit]
		}
		want := r.crc & (1<<refQQCheck - 1)
		if uint8(r.get(refQQCheck)) != want {
			return "", "", false
		}
		if !qqMidShape(string(id[:])) {
			return "", "", false
		}
		return src, string(id[:]), true
	}

	if len(code) != refNumLen {
		return "", "", false
	}
	value := r.get(refNumBits)
	want := r.crc & (1<<refNumCheck - 1)
	if uint8(r.get(refNumCheck)) != want || value == 0 {
		return "", "", false
	}
	return src, strconv.FormatUint(value, 10), true
}

func (s Source) index() (int, bool) {
	switch s {
	case QQ:
		return 0, true
	case Kuwo:
		return 1, true
	case Netease:
		return 2, true
	}
	return 0, false
}

func sourceAt(index uint64) (Source, bool) {
	switch index {
	case 0:
		return QQ, true
	case 1:
		return Kuwo, true
	case 2:
		return Netease, true
	}
	return "", false
}

func (s Source) refCode() string {
	switch s {
	case QQ:
		return "q"
	case Kuwo:
		return "k"
	case Netease:
		return "n"
	}
	return ""
}

// legacySource 认老卡片上的音源前缀。
func legacySource(name string) (Source, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "q", "qq":
		return QQ, true
	case "k", "kuwo":
		return Kuwo, true
	case "n", "netease":
		return Netease, true
	}
	return "", false
}
