package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/channel"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// 降级结果是兜底不是失败: 快的渠道先回一份加密文件时不能就此收工 ——
// 后面那家可能给的是能直接播的。
func TestRacePrefersPlayableOverDegraded(t *testing.T) {
	fast := channel.Channel{ID: "fast"}
	slow := channel.Channel{ID: "slow"}

	got, err := race(context.Background(), []channel.Channel{fast, slow}, 50*time.Millisecond,
		func(_ context.Context, c channel.Channel) (song.Song, error) {
			if c.ID == "fast" {
				return song.Song{Grade: quality.Degraded, Audio: "https://x/a.mflac"}, nil
			}
			time.Sleep(80 * time.Millisecond)
			return song.Song{Grade: quality.Usable, Audio: "https://x/a.mp3"}, nil
		})
	if err != nil {
		t.Fatalf("race 报错: %v", err)
	}
	if got.Audio != "https://x/a.mp3" {
		t.Fatalf("该等能直接播的那份, 实得 %+v", got)
	}
}

// 挑不到更好的时候要把降级的那份交出去 —— 一份打不开的文件也比一句"获取失败"强。
func TestRaceFallsBackToDegradedWhenNothingBetter(t *testing.T) {
	dead := channel.Channel{ID: "dead"}
	only := channel.Channel{ID: "only-degraded"}

	got, err := race(context.Background(), []channel.Channel{dead, only}, 20*time.Millisecond,
		func(_ context.Context, c channel.Channel) (song.Song, error) {
			if c.ID == "dead" {
				return song.Song{}, errors.New("不通")
			}
			return song.Song{Grade: quality.Degraded, Audio: "https://x/a.mflac"}, nil
		})
	if err != nil {
		t.Fatalf("全失败时该回落到降级结果, 实得错误: %v", err)
	}
	if got.Grade != quality.Degraded || got.Audio != "https://x/a.mflac" {
		t.Fatalf("回落结果不对: %+v", got)
	}
}

// 兜底候选之间也要挑: 不可用的那份不算数, 别被它顶掉真正能发的。
func TestRaceIgnoresUnusableAsFallback(t *testing.T) {
	junk := channel.Channel{ID: "junk"}
	ok := channel.Channel{ID: "ok"}

	got, err := race(context.Background(), []channel.Channel{junk, ok}, 20*time.Millisecond,
		func(_ context.Context, c channel.Channel) (song.Song, error) {
			if c.ID == "junk" {
				return song.Song{Grade: quality.Unusable, Audio: "https://x/junk"}, nil
			}
			return song.Song{Grade: quality.Degraded, Audio: "https://x/good.mflac"}, nil
		})
	if err != nil {
		t.Fatalf("race 报错: %v", err)
	}
	if got.Audio != "https://x/good.mflac" {
		t.Fatalf("不可用的那份不该被当兜底, 实得 %+v", got)
	}
}

// 真的一条都没有时还是得报错: 别把"网站在报错"包装成一份文件发出去。
func TestRaceStillFailsWhenNothingUsable(t *testing.T) {
	dead := channel.Channel{ID: "dead"}
	_, err := race(context.Background(), []channel.Channel{dead}, 20*time.Millisecond,
		func(context.Context, channel.Channel) (song.Song, error) {
			return song.Song{}, errors.New("不通")
		})
	if err == nil {
		t.Fatal("全都不通时不该假装成功")
	}
}
