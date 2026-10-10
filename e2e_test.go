package music

import (
	"bytes"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/lyrics"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/source"
)

// 端到端走一遍真实链路: 搜索 → 卡片短码 → 按 id 换直链 → 出歌词图。
//
// 单测各自都过, 但"串起来还能用"是另一回事 —— 上游今天能不能按 id 复取、
// 直链还算不算音频、歌词排不排得出图, 只有真跑一遍才知道。加 -short 跳过。
//
// 想留下那张图看看排版就把 MUSIC_E2E_OUT 指到一个目录。
func TestEndToEndAgainstLiveSources(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}

	pool := source.NewPool(source.Defaults(requests.Init(20))...)

	songs, err := pool.Search("花海", 5)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(songs) == 0 {
		t.Fatal("搜索返回空")
	}
	t.Logf("命中 %d 首, 首条: %s [%s] %s", len(songs), songs[0].Label(), songs[0].Source, songs[0].Ref())

	// 卡片上那行命令得能解回同一首歌 —— 这是"点蓝色歌名"的全部依据。
	pick, ok := song.ParseRef(songs[0].Ref())
	if !ok {
		t.Fatalf("短码 %q 解不回来", songs[0].Ref())
	}
	if pick.Source != songs[0].Source || pick.ID != songs[0].ID {
		t.Fatalf("解成 %+v, want %+v", pick, songs[0])
	}

	found, err := pool.Detail(pick)
	if err != nil {
		t.Fatalf("按 id 换直链失败: %v", err)
	}
	if found.Audio == "" {
		t.Fatal("换回来的直链是空的")
	}
	t.Logf("直链: %s", found.Audio)

	// 时长决定发语音还是发文件, 所以它要么来自搜索(网易), 要么来自探测。
	if found.Duration == 0 {
		t.Fatalf("没能拿到时长, 分不清该发语音还是文件: %s %s", found.Label(), found.Audio)
	}
	branch := "语音"
	if found.Duration > 180 {
		branch = "文件"
	}
	t.Logf("时长 %d 秒 -> 按%s发", found.Duration, branch)

	if found.Lyrics == "" {
		t.Skip("这首歌没有歌词, 不验歌词图")
	}
	img, err := lyrics.Render(found.Title, found.Artist, found.Lyrics)
	if err != nil {
		t.Fatalf("歌词出图失败: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(img)); err != nil {
		t.Fatalf("歌词图不是合法 JPEG: %v", err)
	}
	t.Logf("歌词图 %d 字节", len(img))

	if dir := os.Getenv("MUSIC_E2E_OUT"); dir != "" {
		out := filepath.Join(dir, "lyrics.jpg")
		if err := os.WriteFile(out, img, 0o644); err != nil {
			t.Fatalf("写图失败: %v", err)
		}
		t.Logf("歌词图已写到 %s", out)
	}
}
