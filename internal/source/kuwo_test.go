package source

import (
	"testing"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

func TestParseKuwoSearch(t *testing.T) {
	body := []byte(`{"abslist":[
		{"MUSICRID":"MUSIC_440615","NAME":"花海","ARTIST":"周杰伦"},
		{"MUSICRID":"MUSIC_290849898","NAME":"花海","ARTIST":"龚琳娜&美依礼芽MARiA"},
		{"MUSICRID":"","NAME":"缺 ID","ARTIST":"x"},
		{"MUSICRID":"MUSIC_1","NAME":"","ARTIST":"y"}
	]}`)

	got, err := parseKuwoSearch(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (脏数据应被跳过)", len(got))
	}
	if got[0].Source != song.Kuwo || got[0].ID != "440615" || got[0].Title != "花海" || got[0].Artist != "周杰伦" {
		t.Fatalf("got[0] = %+v", got[0])
	}
	// 卡片上给的是不透明短码: 不暴露音源, 也不暴露曲目 ID
	if code := got[0].Ref(); len(code) != 7 {
		t.Fatalf("Ref() = %q, 长度该是 7", code)
	}
	pick, ok := song.ParseRef(got[0].Ref())
	if !ok || pick.Source != song.Kuwo || pick.ID != "440615" {
		t.Fatalf("短码解不回来: %q -> %+v", got[0].Ref(), pick)
	}
}

func TestParseKuwoSearchEmpty(t *testing.T) {
	got, err := parseKuwoSearch([]byte(`{"abslist":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v", got)
	}
}

// 参数组一旦退化, 酷我会回单引号伪 JSON; 这里要求解析直接失败而不是静默返回空结果。
func TestParseKuwoSearchRejectsPseudoJSON(t *testing.T) {
	if _, err := parseKuwoSearch([]byte(`{'abslist':[]}`)); err == nil {
		t.Fatal("单引号伪 JSON 应当报错")
	}
}

func TestParseKuwoAudio(t *testing.T) {
	link, err := parseKuwoAudio([]byte("https://kw-er.kuwo.cn/x/y.mp3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://kw-er.kuwo.cn/x/y.mp3" {
		t.Fatalf("link = %q", link)
	}
	if _, err := parseKuwoAudio([]byte("res not found")); err == nil {
		t.Fatal("非 URL 应当报错")
	}
}
