package source

import (
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 真实响应里 songid 有时是字符串(qq)、有时是数字(一听), 而没结果时 data 是个空串而不是数组。
func TestParseTxQQHandlesBothShapes(t *testing.T) {
	body := []byte(`{
		"data": [
			{"type":"qq","link":"https://y.qq.com/n/yqq/song/002AdZTO4TdIHS.html",
			 "songid":"002AdZTO4TdIHS","title":"花海","author":"龚琳娜",
			 "lrc":"[00:01.00]不要你离开","url":"http://aqqmusic.tc.qq.com/C400002AdZTO4TdIHS.m4a",
			 "pic":"https://y.gtimg.cn/music/photo_new/x.jpg"},
			{"type":"qq","songid":1219544,"title":"花海","author":"一听歌手",
			 "url":"http://cdn.example/1ting.mp3","lrc":null}
		],
		"code": 200, "error": ""
	}`)

	songs, err := parseTxQQ(body, song.QQ, 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(songs) != 2 {
		t.Fatalf("解析出 %d 首, want 2", len(songs))
	}
	if songs[0].ID != "002AdZTO4TdIHS" || songs[1].ID != "1219544" {
		t.Fatalf("ID 解析错: %q / %q", songs[0].ID, songs[1].ID)
	}
	if songs[0].Source != song.QQ || songs[0].Title != "花海" || songs[0].Artist != "龚琳娜" {
		t.Fatalf("首条 = %+v", songs[0])
	}
	if songs[0].Audio == "" || songs[0].Lyrics == "" {
		t.Fatalf("直链/歌词该带上: %+v", songs[0])
	}
}

// 没有直链的行必须丢掉: 列在卡片上只会让用户点了空欢喜。kuwo 实测就是这样, 一行都放不出来。
func TestParseTxQQDropsRowsWithoutAudio(t *testing.T) {
	body := []byte(`{"data":[
		{"songid":"1","title":"有链","url":"http://cdn/a.mp3"},
		{"songid":"2","title":"没链","url":""},
		{"songid":"3","title":"","url":"http://cdn/c.mp3"},
		{"title":"没 id","url":"http://cdn/d.mp3"}
	],"code":200}`)

	songs, err := parseTxQQ(body, song.Baidu, 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(songs) != 1 || songs[0].ID != "1" {
		t.Fatalf("got = %+v", songs)
	}
}

func TestParseTxQQNoResultIsEmptyNotAnError(t *testing.T) {
	for _, body := range []string{
		`{"data":"","code":404,"error":"没有找到相关信息"}`,
		`{"data":[],"code":200}`,
	} {
		songs, err := parseTxQQ([]byte(body), song.QQ, 10)
		if err != nil {
			t.Fatalf("%s 不该报错: %v", body, err)
		}
		if len(songs) != 0 {
			t.Fatalf("%s 该解析成空, got %+v", body, songs)
		}
	}
}

func TestParseTxQQRespectsLimit(t *testing.T) {
	body := []byte(`{"data":[
		{"songid":"1","title":"a","url":"http://cdn/a.mp3"},
		{"songid":"2","title":"b","url":"http://cdn/b.mp3"},
		{"songid":"3","title":"c","url":"http://cdn/c.mp3"}
	],"code":200}`)

	songs, err := parseTxQQ(body, song.QQ, 2)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(songs) != 2 {
		t.Fatalf("got %d 首, want 2", len(songs))
	}
}

func TestParseTxQQReportsBrokenPayload(t *testing.T) {
	if _, err := parseTxQQ([]byte(`<html>502</html>`), song.QQ, 10); err == nil {
		t.Fatal("上游返回一坨 HTML 时该报错, 好让闸门记一笔")
	}
}
