package song

import (
	"math/rand"
	"strings"
	"testing"
)

// 用户真的会发这种长度正好落在短码窗口里的中文关键词 ——
// 索引表以前只有 128 格, 中文的第一个字节就当索引越界 panic 了。
func TestParseRefSurvivesUnicodeKeywords(t *testing.T) {
	keywords := []string{
		"周杰伦的歌",              // 15 字节
		"周杰伦a",               // 7 字节
		"🎵🎵🎵abc",             // 15 字节
		"🎵🎵a",                // 9 字节
		"周杰伦的歌好好听呀",          // 27 字节
		"夜曲",                 // 6 字节
		"abcdefghijklmno",    // 15 个纯字母, 长度正好撞上
		"qwertyuiopasdfg",    // 同上
		"abcdefg",            // 7 个纯字母, 长度正好撞上
		"Taylor Swift",       // 12 字节
		"001122334455667788", // 18 字节
	}
	for _, keyword := range keywords {
		if _, ok := ParseRef(keyword); ok {
			t.Errorf("ParseRef(%q) 把关键词当成了短码", keyword)
		}
	}
}

// 短码要能扛住"长得像但不是"的词: 校验位之外, mid 还必须是 "00" 开头的 14 位。
func TestRefOnlyAcceptsRealQXMids(t *testing.T) {
	real := Song{Source: QQ, ID: "002hG4kq0WZBma"}
	back, ok := ParseRef(real.Ref())
	if !ok || back != real {
		t.Fatalf("真 mid 往返失败: %q -> %+v,%v", real.Ref(), back, ok)
	}

	// 形状不对的 ID 宁可不编码, 退回老格式也照样能点。
	for _, id := range []string{"xE52o4daJJLq0", "A03cI52o4daJJL", "003cI52o4daJJ", "003cI52o4daJJLL"} {
		got := (Song{Source: QQ, ID: id}).Ref()
		if got != "q:"+id {
			t.Errorf("形状不符的 ID %q 应退回老格式, got %q", id, got)
		}
		if back, ok := ParseRef(got); !ok || back.ID != id {
			t.Errorf("老格式 %q 解不回来: %+v,%v", got, back, ok)
		}
	}
}

// 大量真 mid 往返 + 长度/字母表不变量; 顺便锁住"编解码是纯函数"。
func TestRefRoundTripIsStable(t *testing.T) {
	rng := rand.New(rand.NewSource(20261006))
	for i := 0; i < 2000; i++ {
		id := "00"
		for j := 0; j < 12; j++ {
			id += string(refDigits[rng.Intn(len(refDigits))])
		}

		ref := (Song{Source: QQ, ID: id}).Ref()
		if len(ref) != 15 {
			t.Fatalf("QQ 短码长度 = %d (%q)", len(ref), ref)
		}
		for _, c := range ref {
			if !strings.ContainsRune(refAlphabet, c) {
				t.Fatalf("短码 %q 里有字母表外的字符 %q", ref, c)
			}
		}

		back, ok := ParseRef(ref)
		if !ok || back.Source != QQ || back.ID != id {
			t.Fatalf("往返失败: %q -> %+v,%v", ref, back, ok)
		}
	}
}
