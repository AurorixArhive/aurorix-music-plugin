package song

import "strconv"

// 卡片上那行可点命令的全部内容。它必须自包含: 服务端不留状态, 所以卡片发出去多久、
// 机器人重启多少次, 点下去都还能找到同一首歌。
//
//	version 2 | mode 1 | platform 4 | 载荷 | check 4
//
// mode 0 纯数字 ID 直接塞 36 位(网易/酷我的 id 都是这个量级);
// mode 1 其余按每字符 6 位塞, 前面带 5 位长度(QQ 的 mid、酷狗的 hash、咪咕 18 位数字版权号)。
const refAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

const refDigits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

const (
	refVersion   = 2
	refNumBits   = 36
	refLenBits   = 5
	refMaxChars  = 31
	refCheckBits = 4
	refMinLen    = 8
	refMaxLen    = 34
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

func encodeRef(platform Platform, id string) (string, bool) {
	index, ok := platform.index()
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

func decodeRef(code string) (Platform, string, bool) {
	if len(code) < refMinLen || len(code) > refMaxLen {
		return "", "", false
	}

	r := &refReader{buf: code}
	if r.get(2) != refVersion {
		return "", "", false
	}
	mode := r.get(1)
	platform, ok := refPlatformAt(r.get(4))
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

	// 校验位要先取出来再读 get: get 自己会更新 crc。
	want := r.crc & (1<<refCheckBits - 1)
	if uint8(r.get(refCheckBits)) != want {
		return "", "", false
	}
	return platform, id, true
}

func (p Platform) index() (int, bool) {
	for i, item := range all {
		if item == p {
			return i, true
		}
	}
	return 0, false
}

func refPlatformAt(index uint64) (Platform, bool) {
	if index >= uint64(len(all)) {
		return "", false
	}
	return all[index], true
}
