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
		"周杰伦的歌",              // 15 字节, 正好在长度窗口里
		"周杰伦a",               // 7 字节
		"🎵🎵🎵abc",             // 15 字节
		"🎵🎵a",                // 9 字节
		"周杰伦的歌好好听呀",          // 27 字节
		"夜曲",                 // 6 字节
		"abcdefghijklmno",    // 15 个纯字母, 长度正好撞上
		"qwertyuiopasdfg",    // 同上
		"abcdefg",            // 7 个纯字母
		"abcdefgh",           // 8 个, 正好是短码最短那一档
		"Taylor Swift",       // 12 字节
		"001122334455667788", // 18 字节
	}
	for _, keyword := range keywords {
		if _, ok := ParseRef(keyword); ok {
			t.Errorf("ParseRef(%q) 把关键词当成了短码", keyword)
		}
	}
}

// 数字 ID 编成 8 字符, 14 位 mid 编成 17 字符; 长度是接口的一部分(用户可以看一眼觉得"这是个码")。
func TestRefLengthsAreStable(t *testing.T) {
	cases := []struct {
		song Song
		want int
	}{
		{Song{Source: Wyy, ID: "186016"}, 8},
		{Song{Source: Ting1, ID: "1219544"}, 8},
		{Song{Source: QQ, ID: "002hG4kq0WZBma"}, 17},
		{Song{Source: Baidu, ID: "T10049755016"}, 15},
	}
	for _, c := range cases {
		ref := c.song.Ref()
		if len(ref) != c.want {
			t.Errorf("%s 的短码 %q 长度 %d, want %d", c.song.Source, ref, len(ref), c.want)
		}
		for _, r := range ref {
			if !strings.ContainsRune(refAlphabet, r) {
				t.Errorf("短码 %q 里有字母表外的字符 %q", ref, r)
			}
		}
	}
}

// 编不出来(字母表外的字符、超长 ID)就退回明文形式, 那样也照样能点。
func TestRefFallsBackToPlainForm(t *testing.T) {
	for _, id := range []string{"470521|26729106", strings.Repeat("x", refMaxChars+1)} {
		got := (Song{Source: QQ, ID: id}).Ref()
		if !strings.HasPrefix(got, "qq:") {
			t.Errorf("ID %q 应退回明文形式, got %q", id, got)
		}
		if back, ok := ParseRef(got); !ok || back.ID != id {
			t.Errorf("明文形式 %q 解不回来: %+v,%v", got, back, ok)
		}
	}
	// 空 ID 根本不是一首歌, 退回明文形式就好, 解不回来也无所谓。
	if got := (Song{Source: QQ}).Ref(); got != "qq:" {
		t.Errorf("空 ID 的 Ref() = %q", got)
	}
}

// 大量随机 ID 往返 + 长度/字母表不变量; 顺便锁住"编解码是纯函数"。
func TestRefRoundTripIsStable(t *testing.T) {
	rng := rand.New(rand.NewSource(20261011))
	sources := []Source{QQ, Wyy, Baidu, Ting1}

	for i := 0; i < 3000; i++ {
		src := sources[rng.Intn(len(sources))]
		var id string
		switch src {
		case QQ:
			id = "00"
			for j := 0; j < 12; j++ {
				id += string(refDigits[rng.Intn(len(refDigits))])
			}
		default:
			id = ""
			for j := 0; j < 1+rng.Intn(9); j++ {
				id += string('0' + rune(rng.Intn(10)))
			}
			id = strings.TrimLeft(id, "0")
			if id == "" {
				continue
			}
		}

		ref := (Song{Source: src, ID: id}).Ref()
		if len(ref) < refMinLen || len(ref) > refMaxLen {
			t.Fatalf("%s 的短码 %q 长度 %d 超出窗口", src, ref, len(ref))
		}
		back, ok := ParseRef(ref)
		if !ok || back.Source != src || back.ID != id {
			t.Fatalf("往返失败: %q -> %+v,%v (want %s %s)", ref, back, ok, src, id)
		}
	}
}
