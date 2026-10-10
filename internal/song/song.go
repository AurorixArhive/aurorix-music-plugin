package song

import "strings"

// Source 既是内部标识, 也是用户 -s 后面写的名字。
// 它的取值会进卡片短码的索引表, 所以只许往后追加, 不许改已有的值。
type Source string

const (
	QQ    Source = "qq"
	Wyy   Source = "wyy"
	Baidu Source = "baidu"
	Ting1 Source = "1ting"
)

// all 是唯一的音源清单: 短名表、短码索引表、报错里列的那串名字全从它派生,
// 免得加一家音源要记得改四个地方。
var all = []Source{QQ, Wyy, Baidu, Ting1}

// Song 是一次搜索或一次详情拿到的曲目。
//
// Audio/Lyrics 只在"刚拿到"的那一刻有意义: 直链里带 vkey, 过一阵就废。
// 所以卡片上只放 Ref()(音源 + 曲目 ID), 真要播的时候再按 ID 换一条新链 —— 见 source.Detail。
type Song struct {
	Source   Source
	ID       string
	Title    string
	Artist   string
	Duration int    // 秒; 0 表示上游没说
	Audio    string // 直链
	Lyrics   string // 原始 LRC 文本
}

// Label 是给人看的一行: 歌名 - 歌手。
func (s Song) Label() string {
	if s.Artist == "" {
		return s.Title
	}
	return s.Title + " - " + s.Artist
}

// Ref 是卡片上那行可点命令的全部内容: 自包含、无状态, 所以发出去多久都还能点。
// 编不出来(比如 ID 里有字母表外的字符)就退回 "音源:ID" 的明文形式 —— 难看一点, 但还能点。
func (s Song) Ref() string {
	if code, ok := encodeRef(s.Source, s.ID); ok {
		return code
	}
	return string(s.Source) + ":" + s.ID
}

// ParseRef 认两种形状: 上面的短码, 以及短码编不出来时退回的明文形式。
// 都不像就说"这不是短码", 由上层当关键词去搜 —— 两边都不会误判。
func ParseRef(ref string) (Song, bool) {
	ref = strings.TrimSpace(ref)
	if src, id, ok := decodeRef(ref); ok {
		return Song{Source: src, ID: id}, true
	}
	name, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return Song{}, false
	}
	src, ok := ByName(name)
	if !ok {
		return Song{}, false
	}
	return Song{Source: src, ID: id}, true
}

// ByName 把短名或全名翻成音源; 大小写、首尾空格都不计较。
func ByName(name string) (Source, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "qq", "qqmusic":
		return QQ, true
	case "wyy", "netease", "wangyi":
		return Wyy, true
	case "baidu":
		return Baidu, true
	case "1ting", "yiting":
		return Ting1, true
	}
	return "", false
}

// Names 按声明顺序给出全部短名, 报错信息里列给用户看。
func Names() []string {
	out := make([]string, 0, len(all))
	for _, src := range all {
		out = append(out, string(src))
	}
	return out
}

// CN 是音源的中文名, 说给用户听的时候用。
func (s Source) CN() string {
	switch s {
	case QQ:
		return "QQ音乐"
	case Wyy:
		return "网易云"
	case Baidu:
		return "百度"
	case Ting1:
		return "一听"
	}
	return string(s)
}
