package song

import "testing"

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		want Song
		ok   bool
	}{
		{"qq:002hG4kq0WZBma", Song{Source: QQ, ID: "002hG4kq0WZBma"}, true},
		{"wyy:186016", Song{Source: Wyy, ID: "186016"}, true},
		{"baidu:T10049755016", Song{Source: Baidu, ID: "T10049755016"}, true},
		{"1ting:1219544", Song{Source: Ting1, ID: "1219544"}, true},
		{"kuwo:474678847", Song{}, false}, // 酷我已经不接了
		{"wyy:", Song{}, false},
		{"花海", Song{}, false},
		{"周杰伦:哎哟", Song{}, false},
		{"", Song{}, false},
	}
	for _, c := range cases {
		got, ok := ParseRef(c.in)
		if ok != c.ok || got.Source != c.want.Source || got.ID != c.want.ID {
			t.Errorf("ParseRef(%q) = %+v,%v; want %+v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRefRoundTrip(t *testing.T) {
	for _, s := range []Song{
		{Source: QQ, ID: "002hG4kq0WZBma"},
		{Source: Wyy, ID: "186016"},
		{Source: Baidu, ID: "T10049755016"},
		{Source: Ting1, ID: "1219544"},
	} {
		back, ok := ParseRef(s.Ref())
		if !ok || back.Source != s.Source || back.ID != s.ID {
			t.Fatalf("%+v 往返失败: %q -> %+v,%v", s, s.Ref(), back, ok)
		}
	}
}

func TestLabel(t *testing.T) {
	if got := (Song{Title: "花海", Artist: "周杰伦"}).Label(); got != "花海 - 周杰伦" {
		t.Errorf("Label() = %q", got)
	}
	if got := (Song{Title: "花海"}).Label(); got != "花海" {
		t.Errorf("无歌手时 Label() = %q", got)
	}
}

// 短名表是 -s 和配置项共用的那份, 全名只是顺手认一下。
func TestByName(t *testing.T) {
	for in, want := range map[string]Source{
		"qq": QQ, "QQMusic": QQ, " wyy ": Wyy, "netease": Wyy,
		"baidu": Baidu, "1ting": Ting1, "yiting": Ting1,
	} {
		if got, ok := ByName(in); !ok || got != want {
			t.Errorf("ByName(%q) = %q,%v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"kuwo", "kw", "网易", "", "nope"} {
		if got, ok := ByName(bad); ok {
			t.Errorf("ByName(%q) 不该认, got %q", bad, got)
		}
	}
}

func TestNamesCoversEverySource(t *testing.T) {
	names := Names()
	if len(names) != len(all) {
		t.Fatalf("短名 %v 与音源表 %v 数量对不上", names, all)
	}
	for _, src := range all {
		if _, ok := ByName(string(src)); !ok {
			t.Errorf("音源 %q 不在短名表里, 用户就没法用 -s 点它", src)
		}
	}
}
