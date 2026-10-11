// Package qqsearch 是 QQ 音源共用的搜索: 云汐的搜索模式按关键词回一批歌, 结果里带 songmid。
package qqsearch

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

var (
	searchAPI = "https://a.aa.cab/qq.music"
	client    = netx.New(15, map[string]string{"User-Agent": browserUA})
)

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

type hit struct {
	Song   string `json:"song"`
	Singer string `json:"singer"`
	Cover  string `json:"cover"`
	Mid    string `json:"mid"`
}

type body struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data []hit  `json:"data"`
}

// Search 拿一批歌; 上游不给时长, 时长等取到直链时再补。
func Search(ctx context.Context, keyword string, limit int) ([]song.Song, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	query := url.Values{"msg": {keyword}, "num": {strconv.Itoa(limit)}, "type": {"1"}}
	raw, err := client.Get(searchAPI+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var resp body
	if err := netx.Decode(raw, &resp); err != nil {
		return nil, err
	}
	out := make([]song.Song, 0, len(resp.Data))
	for _, item := range resp.Data {
		mid, title := strings.TrimSpace(item.Mid), strings.TrimSpace(item.Song)
		if mid == "" || title == "" {
			continue
		}
		out = append(out, song.Song{
			Platform: song.QQ,
			ID:       mid,
			Title:    title,
			Artist:   strings.TrimSpace(item.Singer),
			Cover:    strings.TrimSpace(item.Cover),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("QQ 音乐没有搜到 %q: %s", keyword, resp.Msg)
	}
	return out, nil
}
