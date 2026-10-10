package song

import "strings"

type Source string

const (
	QQ      Source = "qq"
	Kuwo    Source = "kuwo"
	Netease Source = "netease"
)

type Song struct {
	Source   Source
	ID       string
	Title    string
	Artist   string
	Duration int
}

func (s Song) Label() string {
	if s.Artist == "" {
		return s.Title
	}
	return s.Title + " - " + s.Artist
}

// Ref 是卡片上那行可点命令的全部内容: 自包含、无状态, 所以发出去多久都还能点。
// 编不出来(比如 ID 不是预期的形状)就退回老格式 —— 难看一点, 但还能点。
func (s Song) Ref() string {
	if code, ok := encodeRef(s.Source, s.ID); ok {
		return code
	}
	return s.Source.refCode() + ":" + s.ID
}

func ParseRef(ref string) (Song, bool) {
	ref = strings.TrimSpace(ref)
	if src, id, ok := decodeRef(ref); ok {
		return Song{Source: src, ID: id}, true
	}
	// 老卡片上的 q:/kuwo:/netease: 仍然认, 不然它们就失联了
	name, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return Song{}, false
	}
	src, ok := legacySource(name)
	if !ok {
		return Song{}, false
	}
	return Song{Source: src, ID: id}, true
}
