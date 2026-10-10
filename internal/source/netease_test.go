package source

import (
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

func TestParseNeteaseSearch(t *testing.T) {
	body := []byte(`{"result":{"songs":[
		{"id":2659569861,"name":"花海","duration":269651,"fee":8,
		 "artists":[{"name":"粉白噪音"},{"name":"另一个"}]},
		{"id":186016,"name":"晴天","duration":269000,"fee":0,
		 "artists":[{"name":"周杰伦"}]},
		{"id":347230,"name":"海阔天空","duration":326000,"fee":1,"artists":[{"name":"Beyond"}]},
		{"id":999,"name":"要买专辑的","duration":200000,"fee":4,"artists":[]},
		{"id":0,"name":"没有 id","duration":1,"fee":0,"artists":[]}
	],"songCount":5},"code":200}`)

	songs, err := parseNeteaseSearch(body, 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(songs) != 2 {
		t.Fatalf("解析出 %d 首, want 2(会员与要买专辑的都该滤掉): %+v", len(songs), songs)
	}
	if songs[0].ID != "2659569861" || songs[0].Title != "花海" {
		t.Fatalf("首条 = %+v", songs[0])
	}
	if songs[0].Artist != "粉白噪音/另一个" {
		t.Fatalf("多歌手该用斜杠连起来, got %q", songs[0].Artist)
	}
	// 网易给的是毫秒, 我们一律存秒。
	if songs[0].Duration != 269 {
		t.Fatalf("时长 = %d 秒, want 269", songs[0].Duration)
	}
	if songs[0].Source != song.Wyy {
		t.Fatalf("音源 = %q", songs[0].Source)
	}
}

func TestParseNeteaseSearchReportsBrokenPayload(t *testing.T) {
	if _, err := parseNeteaseSearch([]byte(`not json`), 10); err == nil {
		t.Fatal("解析不了时该报错, 好让闸门记一笔")
	}
}

// 直链是"跳到真文件"的那种, 所以探测既是可用性检查也是拿时长的地方。
func TestNeteaseAudioURL(t *testing.T) {
	got := neteaseAudioURL("186016")
	want := "http://music.163.com/song/media/outer/url?id=186016.mp3"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
