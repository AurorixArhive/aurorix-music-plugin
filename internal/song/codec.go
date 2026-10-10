package song

import "strconv"

// 卡片上那行可点命令的全部内容。它必须自包含 —— 服务端不留任何状态,
// 所以卡片发出去多久、机器人重启多少次, 点下去都还能找到同一首歌。
//
// 打包方式是 6 bit 一字符的自定义字母表(64 个字符, 不含 +/=/&/% 这些在 mqqapi
// 链接里要转义、或在输入框里看着别扭的字符):
//
//	version 2 | mode 1 | source 4 | 载荷 | check 4
//
// 载荷两选一, 由 mode 那一位区分:
//
//	mode 0  纯数字 ID 直接塞 36 位 —— 网易/一听/百度的 ID 都是数字, 编出 8 字符
//	mode 1  带字母的 ID(qq 的 14 位 mid)按每字符 6 位塞, 前面加 5 位长度, 编出 17 字符
//
// 刻意不用大数运算: 62^14 装不进 uint64, 硬上 math/big 会慢一个量级。
const refAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// refDigits 是 ID 用的 62 进制数字表, 顺序与 qq 的 mid 一致(id 是 62 进制, 不是自己的随机串)。
const refDigits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

const (
	refVersion   = 2
	refNumBits   = 36
	refLenBits   = 5
	refMaxChars  = 31
	refCheckBits = 4

	// 短码长度落在 [8, 34]: 8 是纯数字那一档, 34 是 31 字符 ID 那一档。
	// 这是个粗筛 —— 关键词绝大多数连这一关都过不了, 过了的交给音源编号和校验位。
	// 音源编号只有 4 位、目前只用了 0-3, 所以"长得像短码"和"真是短码"之间隔得很远。
	refMinLen = 8
	refMaxLen = 34
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
	buf [refMaxLen]byte
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
func (w *refWriter) finish() string {
	w.put(uint64(w.crc)&(1<<refCheckBits-1), refCheckBits)
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

// get 一次取 bits 位。越过末尾的部分按 0 补 —— 写入端也是这么补的, 所以不用先知道总长。
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

// encodeRef 把 (音源, 曲目 ID) 编成短码; 编不出来就返回 false, 由调用方退回明文形式。
func encodeRef(src Source, id string) (string, bool) {
	index, ok := src.index()
	if !ok {
		return "", false
	}

	var w refWriter
	w.put(refVersion, 2)
	if value, err := strconv.ParseUint(id, 10, 64); err == nil && value > 0 && value < 1<<refNumBits {
		w.put(0, 1)
		w.put(uint64(index), 4)
		w.put(value, refNumBits)
		return w.finish(), true
	}

	if len(id) == 0 || len(id) > refMaxChars {
		return "", false
	}
	for i := 0; i < len(id); i++ {
		if refDigitIndex[id[i]] < 0 {
			return "", false
		}
	}
	w.put(1, 1)
	w.put(uint64(index), 4)
	w.put(uint64(len(id)), refLenBits)
	for i := 0; i < len(id); i++ {
		w.put(uint64(refDigitIndex[id[i]]), 6)
	}
	return w.finish(), true
}

func decodeRef(code string) (Source, string, bool) {
	// 长度先卡死: 关键词绝大多数连这一关都过不了
	if len(code) < refMinLen || len(code) > refMaxLen {
		return "", "", false
	}

	r := &refReader{buf: code}
	if r.get(2) != refVersion {
		return "", "", false
	}
	mode := r.get(1)
	src, ok := sourceAt(r.get(4))
	if !ok {
		return "", "", false
	}

	var id string
	if mode == 1 {
		length := r.get(refLenBits)
		if length == 0 || length > refMaxChars {
			return "", "", false
		}
		digits := make([]byte, length)
		for i := range digits {
			digit := r.get(6)
			if digit >= uint64(len(refDigits)) {
				return "", "", false
			}
			digits[i] = refDigits[digit]
		}
		id = string(digits)
	} else {
		value := r.get(refNumBits)
		if value == 0 {
			return "", "", false
		}
		id = strconv.FormatUint(value, 10)
	}

	// 校验位要先取出来再读: get 自己会更新 crc, 写在同一个表达式里两个操作数谁先算就说不准了。
	want := r.crc & (1<<refCheckBits - 1)
	if uint8(r.get(refCheckBits)) != want {
		return "", "", false
	}
	return src, id, true
}

func (s Source) index() (int, bool) {
	for i, src := range all {
		if src == s {
			return i, true
		}
	}
	return 0, false
}

func sourceAt(index uint64) (Source, bool) {
	if index >= uint64(len(all)) {
		return "", false
	}
	return all[index], true
}
