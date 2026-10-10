package source

import (
	"errors"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

var ErrNoResult = errors.New("未找到可播放的结果")

// Source 一家音源。
//
// Search 只负责"有哪些歌", Detail 只负责"这一首怎么放" —— 分开是因为卡片要能
// 放很久: 搜索时拿到的直链带 vkey, 过期就废, 所以卡片上只存 (音源, ID),
// 真播的时候再按 ID 换一条新链。
type Source interface {
	Name() song.Source
	Search(keyword string, limit int) ([]song.Song, error)
	Detail(id string) (song.Song, error)
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

// Detail 按卡片上的 (音源, ID) 换一条当前可用的直链。
func (c Chain) Detail(pick song.Song) (song.Song, error) {
	owner := c.owner(pick.Source)
	if owner == nil {
		return song.Song{}, ErrNoResult
	}
	return owner.Detail(pick.ID)
}

func (c Chain) owner(name song.Source) Source {
	for _, src := range c {
		if src.Name() == name {
			return src
		}
	}
	return nil
}

// normal 把"上游正常回答但没结果"和"上游真出故障"分开 —— 前者不该触发熔断。
// 搜不到歌、付费曲取不到直链都是正常回答, 连着来一百次也不能把音源断掉。
func normal(err error) error {
	if err == nil || errors.Is(err, ErrNoResult) {
		return nil
	}
	return err
}
