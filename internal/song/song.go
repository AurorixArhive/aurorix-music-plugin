package song

import (
	"strings"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
)

type Platform string

const (
	QQ    Platform = "qq"
	Kugou Platform = "kugou"
	Kuwo  Platform = "kuwo"
	Wyy   Platform = "wyy"
)

// all 的顺序就是默认优先级, 管理台里可以改。
var all = []Platform{QQ, Kugou, Kuwo, Wyy}

type PlatformInfo struct {
	Platform Platform
	CN       string
	Aliases  []string
}

var infos = []PlatformInfo{
	{QQ, "QQ音乐", []string{"qq", "qqmusic", "tencent", "tx", "腾讯", "qq音乐"}},
	{Kugou, "酷狗", []string{"kugou", "kg", "酷狗", "酷狗音乐"}},
	{Kuwo, "酷我", []string{"kuwo", "kw", "酷我", "酷我音乐"}},
	{Wyy, "网易云", []string{"wyy", "wy", "netease", "wangyi", "163", "网易", "网易云", "网易云音乐"}},
}

func All() []Platform { return append([]Platform(nil), all...) }

func Info(p Platform) PlatformInfo {
	for _, info := range infos {
		if info.Platform == p {
			return info
		}
	}
	return PlatformInfo{Platform: p, CN: string(p)}
}

func CN(p Platform) string { return Info(p).CN }

func ByName(name string) (Platform, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", false
	}
	for _, info := range infos {
		for _, alias := range info.Aliases {
			if alias == name {
				return info.Platform, true
			}
		}
	}
	return "", false
}

// Suggest 给写错的名字猜一个最接近的音源, 猜不出就返回空 —— 猜错比不猜更烦人。
func Suggest(name string) Platform {
	name = strings.ToLower(strings.TrimSpace(name))
	best, bestScore := Platform(""), 0
	for _, info := range infos {
		for _, alias := range info.Aliases {
			if score := commonPrefix(alias, name); score > bestScore {
				best, bestScore = info.Platform, score
			}
		}
	}
	if bestScore >= 2 {
		return best
	}
	return ""
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

type Song struct {
	Platform Platform
	ID       string
	Title    string
	Artist   string
	Duration int
	Audio    string
	Cover    string
	Channel  string
	// Grade 是这条直链的可用程度(见 internal/quality): 调度按它挑渠道, 发送按它定投递方式。
	Grade quality.Grade
}

// Usability 让调度不必认识 song 的字段: 它只问"这份结果有多可用"。
func (s Song) Usability() int { return int(s.Grade) }

func (s Song) Label() string {
	if s.Artist == "" {
		return s.Title
	}
	return s.Title + " - " + s.Artist
}

func (s Song) Ref() string {
	if code, ok := encodeRef(s.Platform, s.ID); ok {
		return code
	}
	return string(s.Platform) + ":" + s.ID
}

func ParseRef(ref string) (Song, bool) {
	ref = strings.TrimSpace(ref)
	if platform, id, ok := decodeRef(ref); ok {
		return Song{Platform: platform, ID: id}, true
	}
	name, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return Song{}, false
	}
	platform, ok := ByName(name)
	if !ok {
		return Song{}, false
	}
	return Song{Platform: platform, ID: id}, true
}
