package song

import "testing"

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		want Song
		ok   bool
	}{
		{"qq:003cI52o4daJJL", Song{Source: QQ, ID: "003cI52o4daJJL"}, true},
		{"kuwo:474678847", Song{Source: Kuwo, ID: "474678847"}, true},
		{"netease:2659569861", Song{Source: Netease, ID: "2659569861"}, true},
		{"kuwo:", Song{}, false},
		{"花海", Song{}, false},
		{"周杰伦:哎哟", Song{}, false},
		{"", Song{}, false},
	}
	for _, c := range cases {
		got, ok := ParseRef(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseRef(%q) = %+v,%v; want %+v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRefRoundTrip(t *testing.T) {
	for _, s := range []Song{
		{Source: QQ, ID: "003cI52o4daJJL"},
		{Source: Kuwo, ID: "440615"},
		{Source: Netease, ID: "2659569861"},
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
