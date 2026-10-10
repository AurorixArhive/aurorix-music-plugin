package source

import (
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

func TestParseNeteaseSearch(t *testing.T) {
	body := []byte(`{"result":{"songs":[
		{"id":2659569861,"name":"花海","ar":[{"name":"粉白噪音"}]},
		{"id":477738632,"name":"花海","ar":[{"name":"中孝介"},{"name":"乙"}]},
		{"id":0,"name":"缺 ID","ar":[]}
	]}}`)

	got, err := parseNeteaseSearch(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (脏数据应被跳过)", len(got))
	}
	if got[0].Source != song.Netease || got[0].ID != "2659569861" || got[0].Artist != "粉白噪音" {
		t.Fatalf("got[0] = %+v", got[0])
	}
	if got[1].Artist != "中孝介/乙" {
		t.Fatalf("多歌手应拼接: %q", got[1].Artist)
	}
	// 卡片上给的是不透明短码: 不暴露音源, 也不暴露曲目 ID
	if code := got[0].Ref(); len(code) != 7 {
		t.Fatalf("Ref() = %q, 长度该是 7", code)
	}
	pick, ok := song.ParseRef(got[0].Ref())
	if !ok || pick.Source != song.Netease || pick.ID != "2659569861" {
		t.Fatalf("短码解不回来: %q -> %+v", got[0].Ref(), pick)
	}
}

func TestParseNeteaseSearchEmpty(t *testing.T) {
	got, err := parseNeteaseSearch([]byte(`{"result":{"songs":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v", got)
	}
}

func TestNeteaseAudioURL(t *testing.T) {
	link, err := Netease{}.Audio(song.Song{Source: song.Netease, ID: "2659569861"})
	if err != nil {
		t.Fatal(err)
	}
	want := "http://music.163.com/song/media/outer/url?id=2659569861.mp3"
	if link != want {
		t.Fatalf("link = %q, want %q", link, want)
	}
}
