package source

import (
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

func liveClient() *requests.Client { return requests.Init(20) }

// 联网测试: 默认跑, 加 -short 跳过。
//
// 这几条的价值在于"音源今天还活着吗" —— 上游换接口、加风控我们预先看不出来,
// 只会以"用户点了没反应"的形式暴露。
func liveSources(t *testing.T) []Source {
	t.Helper()
	if testing.Short() {
		t.Skip("联网测试")
	}
	return Defaults(liveClient())
}

func TestTxQQLive(t *testing.T) {
	for _, src := range liveSources(t) {
		if src.Name() != song.QQ {
			continue
		}
		songs, err := src.Search("花海", 5)
		if err != nil {
			t.Fatalf("搜索失败: %v", err)
		}
		if len(songs) == 0 {
			t.Fatal("聚合站的 qq 搜不到东西")
		}
		t.Logf("命中 %d 首, 首条: %s (%s)", len(songs), songs[0].Label(), songs[0].Ref())

		found, err := src.Detail(songs[0].ID)
		if err != nil {
			t.Fatalf("按 id 复取失败: %v", err)
		}
		if found.Audio == "" {
			t.Fatal("复取回来的直链是空的")
		}
		info, err := Inspect(found.Audio)
		if err != nil {
			t.Fatalf("探测直链失败: %v", err)
		}
		if !info.Playable {
			t.Fatalf("聚合站给的直链不是音频: %+v", info)
		}
		t.Logf("直链可播: %d 秒, %d 字节", info.Seconds, info.Size)
	}
}

func TestNeteaseLive(t *testing.T) {
	for _, src := range liveSources(t) {
		if src.Name() != song.Wyy {
			continue
		}
		songs, err := src.Search("花海", 5)
		if err != nil {
			t.Fatalf("搜索失败: %v", err)
		}
		if len(songs) == 0 {
			t.Fatal("网易搜不到东西")
		}
		t.Logf("命中 %d 首, 首条: %s", len(songs), songs[0].Label())

		// 网易付费曲多, 头一条取不到直链不算接口挂了 —— 扫到一条能放的就行。
		var last error
		for _, s := range songs {
			found, err := src.Detail(s.ID)
			if err != nil {
				last = err
				continue
			}
			t.Logf("直链可播: %d 秒, 歌词 %d 字, %s", found.Duration, len(found.Lyrics), found.Audio)
			return
		}
		t.Fatalf("%d 首都没取到直链, 最后一个错误: %v", len(songs), last)
	}
}
