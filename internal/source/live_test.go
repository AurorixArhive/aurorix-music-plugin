package source

import (
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"
)

func liveClient() *requests.Client { return requests.Init(20) }

func TestKuwoLive(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}

	kuwo := NewKuwo(liveClient())
	songs, err := kuwo.Search("花海", 5)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(songs) == 0 {
		t.Fatal("酷我搜索返回空")
	}
	t.Logf("命中 %d 首, 首条: %s (%s)", len(songs), songs[0].Label(), songs[0].Ref())

	link, err := kuwo.Audio(songs[0])
	if err != nil {
		t.Fatalf("取音频失败: %v", err)
	}
	if link == "" {
		t.Fatal("音频链接为空")
	}
	t.Logf("音频直链: %s", link)
}

func TestNeteaseLive(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}

	netease := NewNetease(liveClient())
	songs, err := netease.Search("花海", 5)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(songs) == 0 {
		t.Fatal("网易云搜索返回空")
	}
	t.Logf("命中 %d 首, 首条: %s (%s)", len(songs), songs[0].Label(), songs[0].Ref())
}

func TestQQLive(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}

	qq := NewQQ(liveClient(), nil)
	songs, err := qq.Search("花海", 10)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(songs) == 0 {
		t.Fatal("QQ 搜索返回空")
	}
	t.Logf("命中 %d 首, 首条: %s (%s)", len(songs), songs[0].Label(), songs[0].Ref())

	// 付费曲目占三成左右, 全取不到才说明接口真的挂了。
	var last error
	for _, s := range songs {
		link, err := qq.Audio(s)
		if err != nil {
			last = err
			continue
		}
		t.Logf("音频直链: %s", link)
		return
	}
	t.Fatalf("%d 首都没取到直链, 最后一个错误: %v", len(songs), last)
}
