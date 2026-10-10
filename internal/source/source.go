package source

import (
	"errors"
	"strings"
	"unicode"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

var ErrNoResult = errors.New("未找到可播放的结果")

type Source interface {
	Name() song.Source
	Search(keyword string, limit int) ([]song.Song, error)
	Audio(pick song.Song) (string, error)
}

type Chain []Source

func (c Chain) Search(keyword string, limit int) ([]song.Song, error) {
	failed := error(ErrNoResult)
	for _, src := range c {
		songs, err := src.Search(keyword, limit)
		if err != nil {
			failed = err
			continue
		}
		if len(songs) > 0 {
			return songs, nil
		}
	}
	return nil, failed
}

// SearchOn 只在指定音源里搜; only 为空时退回默认的降级链。
// 用户用 -s 指定了音源就该尊重它 —— 在这家没找到就是没找到, 不悄悄换别家。
// 没结果时返回空列表而不是 ErrNoResult, 好让调用方说清"是哪一家没有"。
func (c Chain) SearchOn(only song.Source, keyword string, limit int) ([]song.Song, error) {
	if only == "" {
		return c.Search(keyword, limit)
	}
	owner := c.owner(only)
	if owner == nil {
		return nil, nil
	}
	return owner.Search(keyword, limit)
}

func (c Chain) Audio(pick song.Song) (string, error) {
	owner := c.owner(pick.Source)
	if owner == nil {
		return "", ErrNoResult
	}
	link, err := owner.Audio(pick)
	if err == nil {
		return link, nil
	}
	if alt, ok := c.match(pick); ok {
		return alt, nil
	}
	return "", err
}

func (c Chain) owner(name song.Source) Source {
	for _, src := range c {
		if src.Name() == name {
			return src
		}
	}
	return nil
}

func (c Chain) match(pick song.Song) (string, bool) {
	title := normalize(pick.Title)
	if title == "" {
		return "", false
	}
	artist := normalize(pick.Artist)
	var loose *song.Song

	for _, src := range c {
		if src.Name() == pick.Source {
			continue
		}
		songs, err := src.Search(pick.Title, 10)
		if err != nil {
			continue
		}
		for _, cand := range songs {
			if normalize(cand.Title) != title {
				continue
			}
			if artist != "" && normalize(cand.Artist) != artist {
				if loose == nil {
					fallback := cand
					loose = &fallback
				}
				continue
			}
			if link, err := src.Audio(cand); err == nil {
				return link, true
			}
		}
	}

	if loose == nil {
		return "", false
	}
	if src := c.owner(loose.Source); src != nil {
		if link, err := src.Audio(*loose); err == nil {
			return link, true
		}
	}
	return "", false
}

func normalize(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("()（）[]【】-—_·,，.。!！?？&+/、'\"“”", r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
}

// normal 把"上游正常回答但没结果"和"上游真出故障"分开 —— 前者不该触发熔断。
// 搜不到歌、付费曲取不到直链都是正常回答, 连着来一百次也不能把音源断掉。
func normal(err error) error {
	if err == nil || errors.Is(err, ErrNoResult) {
		return nil
	}
	return err
}
