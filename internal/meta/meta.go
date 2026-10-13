// Package meta 按「平台 + 歌曲 ID」问一次平台, 把卡片要显示的东西补回来。
//
// 为什么得有这一层: 卡片上那行可点命令只能带得动一个短码(见 internal/song/codec.go),
// 短码里只有平台和歌曲 ID —— 标题、作者、封面在用户点下去的那一刻就已经丢了。所以
// deliver 手里只剩 (平台, ID), 卡片想显示什么, 只能回头问平台自己。
//
// 三条约定:
//   - 只补, 不覆盖: 传进来的字段已经有值就原样留着(搜索结果本来就带全的那条路一次请求都不发)。
//   - 只问公开的详情接口: QQ 用官方的 fcg_play_single_song, 酷狗用官方的 getSongInfo,
//     网易云用官方的 song/detail —— 都跟自己取直链用的那套是同一批接口, 不额外引第三方。
//   - 失败就原样返回: 卡片画不出来不算失败, 音频才是主角。酷我没有公开的按 ID 查详情的接口,
//     它就补不出来, 那时卡片会自己跳过, 而不是发一张空壳出去。
package meta

import (
	"context"
	"net/url"
	"strings"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 地址与客户端写成变量: 测试里指到假服务器上。
var (
	qqDetailURL = "https://c.y.qq.com/v8/fcg-bin/fcg_play_single_song.fcg"
	kgInfoURL   = "https://m.kugou.com/app/i/getSongInfo.php?cmd=playInfo"
	wyDetailURL = "https://music.163.com/api/song/detail"

	// qqCoverBase + 专辑 mid + .jpg: 就是搜索接口自己回的那张封面, 同一个 CDN。
	qqCoverBase = "https://y.gtimg.cn/music/photo_new/T002R500x500M000"
	// kgCoverSize 替掉酷狗封面地址里的 {size} 占位。
	kgCoverSize = "480"
)

const (
	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	// timeout 给得比取直链短: 这一步只为了让卡片好看, 不该拖住后面的音频。
	timeout = 6
)

var (
	qqClient = netx.New(timeout, map[string]string{"User-Agent": browserUA, "Referer": "https://y.qq.com/"})
	kgClient = netx.New(timeout, map[string]string{"User-Agent": browserUA, "Referer": "https://www.kugou.com/"})
	wyClient = netx.New(timeout, map[string]string{"User-Agent": browserUA, "Referer": "https://music.163.com/"})
)

// Fill 补齐 s 缺的标题/作者/封面。已经有标题的那条路直接原样返回, 一次请求都不发。
func Fill(ctx context.Context, s song.Song) song.Song {
	if ctx.Err() != nil {
		return s
	}
	if strings.TrimSpace(s.Title) != "" && strings.TrimSpace(s.Cover) != "" {
		return s
	}
	if strings.TrimSpace(s.ID) == "" {
		return s
	}
	switch s.Platform {
	case song.QQ:
		return fillQQ(s)
	case song.Kugou:
		return fillKugou(s)
	case song.Wyy:
		return fillWyy(s)
	}
	return s
}

type qqSinger struct {
	Name string `json:"name"`
}

type qqDetailBody struct {
	Data []struct {
		Name   string     `json:"name"`
		Singer []qqSinger `json:"singer"`
		Album  struct {
			Mid string `json:"mid"`
		} `json:"album"`
	} `json:"data"`
}

func fillQQ(s song.Song) song.Song {
	query := url.Values{
		"songmid": {s.ID}, "platform": {"yqq.json"}, "format": {"json"},
		"inCharset": {"utf8"}, "outCharset": {"utf-8"}, "needNewCode": {"1"},
	}
	var body qqDetailBody
	if err := qqClient.GetJSON(qqDetailURL+"?"+query.Encode(), &body, nil); err != nil {
		return s
	}
	if len(body.Data) == 0 {
		return s
	}
	info := body.Data[0]
	if s.Title == "" {
		s.Title = strings.TrimSpace(info.Name)
	}
	if s.Artist == "" {
		s.Artist = joinSingers(info.Singer)
	}
	// 封面只有专辑 mid 拼得出来; 歌曲 mid 跟它不是一个东西, 拼不对宁可不带图。
	if s.Cover == "" && strings.TrimSpace(info.Album.Mid) != "" {
		s.Cover = qqCoverBase + strings.TrimSpace(info.Album.Mid) + ".jpg"
	}
	return s
}

func joinSingers(list []qqSinger) string {
	names := make([]string, 0, len(list))
	for _, item := range list {
		if name := strings.TrimSpace(item.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, "、")
}

type kgInfoBody struct {
	SongName   string `json:"songName"`
	SingerName string `json:"singerName"`
	AuthorName string `json:"author_name"`
	AlbumImg   string `json:"album_img"`
}

func fillKugou(s song.Song) song.Song {
	// 酷狗的 ID 是 "hash|album_id"(见 molan 的搜索), 详情接口只认 hash。
	hash, _, _ := strings.Cut(s.ID, "|")
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return s
	}
	var body kgInfoBody
	if err := kgClient.GetJSON(kgInfoURL+"&hash="+url.QueryEscape(hash), &body, nil); err != nil {
		return s
	}
	if s.Title == "" {
		s.Title = strings.TrimSpace(body.SongName)
	}
	if s.Artist == "" {
		s.Artist = strings.TrimSpace(body.SingerName)
		if s.Artist == "" {
			s.Artist = strings.TrimSpace(body.AuthorName)
		}
	}
	if s.Cover == "" {
		s.Cover = strings.Replace(strings.TrimSpace(body.AlbumImg), "{size}", kgCoverSize, 1)
	}
	return s
}

type wyDetailBody struct {
	Songs []struct {
		Name    string `json:"name"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			PicURL string `json:"picUrl"`
		} `json:"album"`
	} `json:"songs"`
}

func fillWyy(s song.Song) song.Song {
	// 官方这个接口要的是 JSON 数组, 所以 ids 得写成 [123]。
	query := url.Values{"ids": {"[" + s.ID + "]"}}
	var body wyDetailBody
	if err := wyClient.GetJSON(wyDetailURL+"?"+query.Encode(), &body, nil); err != nil {
		return s
	}
	if len(body.Songs) == 0 {
		return s
	}
	info := body.Songs[0]
	if s.Title == "" {
		s.Title = strings.TrimSpace(info.Name)
	}
	if s.Artist == "" {
		names := make([]string, 0, len(info.Artists))
		for _, a := range info.Artists {
			if name := strings.TrimSpace(a.Name); name != "" {
				names = append(names, name)
			}
		}
		s.Artist = strings.Join(names, "、")
	}
	if s.Cover == "" {
		s.Cover = strings.TrimSpace(info.Album.PicURL)
	}
	return s
}
