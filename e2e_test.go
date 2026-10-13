package music

import (
	"context"
	"testing"
	"time"

	_ "github.com/AurorixArhive/aurorix-music-plugin/internal/channel/all"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/schedule"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 端到端: 搜索 → 卡片短码 → 换直链。加 -short 跳过。
//
// 单测各自都过, 但"串起来还能用"是另一回事: 上游今天还认不认这个 id、
// 换回来的直链还算不算音频, 只有真跑一遍才知道。
func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}

	scheduler := schedule.New()
	scheduler.SetPlatformOrder(song.All())

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	found, err := scheduler.Search(ctx, "", "", "花海", 5)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("搜索返回空")
	}
	t.Logf("命中 %d 首, 首条: %s [%s] %s", len(found), found[0].Label(), found[0].Platform, found[0].Ref())

	pick, ok := song.ParseRef(found[0].Ref())
	if !ok {
		t.Fatalf("短码 %q 解不回来", found[0].Ref())
	}
	if pick.Platform != found[0].Platform || pick.ID != found[0].ID {
		t.Fatalf("解成 %+v, want %+v", pick, found[0])
	}

	for _, candidate := range found {
		got, err := scheduler.Resolve(ctx, candidate, "")
		if err != nil {
			t.Logf("跳过 %s: %v", candidate.Label(), err)
			continue
		}
		if got.Audio == "" {
			t.Fatalf("换回来的直链是空的: %+v", got)
		}
		branch := "语音"
		if got.Duration > 300 {
			branch = "文件"
		}
		t.Logf("[%s] %s 时长 %d 秒 → 按%s发, 直链 %s",
			got.Platform, got.Label(), got.Duration, branch, got.Audio)
		return
	}
	t.Fatal("5 首都没换到直链")
}

// 体检: 每条渠道各打一次, 把今天到底哪些渠道还活着打出来。
func TestProbeAllChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}

	scheduler := schedule.New()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	scheduler.ProbeOnce(ctx)

	for _, status := range scheduler.Channels() {
		verdict := "不通"
		if status.OK > 0 && status.Fail == 0 {
			verdict = "通"
		}
		t.Logf("%-22s %-6s %-4s 成功 %d 失败 %d 延迟 %dms %s",
			status.ID, status.Platform, verdict, status.OK, status.Fail, status.LatencyMS, status.LastErr)
	}
}

// 只给加密的付费曲: 上游在高档位会回退成酷我的 .mflac(谁都打不开), 必须靠降档拿到明文。
// 这首歌在腾讯那边只有 128k, 所以高档位全部回退 —— 真踩过。
func TestPaidSongFallsBackToPlayable(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}

	scheduler := schedule.New()
	scheduler.SetPlatformOrder(song.All())

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	got, err := scheduler.Resolve(ctx, song.Song{Platform: song.QQ, ID: "0044t2fe2OMKMr"}, "")
	if err != nil {
		t.Fatalf("换直链失败: %v", err)
	}
	if got.Grade < quality.Usable {
		t.Fatalf("拿到的是%s, 用户打不开: %s", got.Grade, got.Audio)
	}
	t.Logf("%s 时长 %d 秒 [%s] %s", got.Label(), got.Duration, got.Channel, got.Audio)
}
