package source

import (
	"errors"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// "上游正常回答但没这首歌"不是故障: 付费曲、搜不到都不该把音源断掉。
func TestNormalTreatsEmptyResultAsHealthy(t *testing.T) {
	if normal(nil) != nil {
		t.Fatal("成功就是成功")
	}
	if normal(ErrNoResult) != nil {
		t.Fatal("没结果不等于故障, 算成故障会让音源被自己断掉")
	}
	if normal(errors.New("上游 500")) == nil {
		t.Fatal("真故障必须记一笔, 否则熔断永远不会触发")
	}
}

// 每个音源各拿一个闸门: 一个音源被限住或断开, 不能连带拖死其它音源。
func TestEverySourceOwnsItsOwnGate(t *testing.T) {
	http := requests.Init(5)
	gates := map[song.Source]*guard.Gate{
		song.QQ:      NewQQ(http, nil).gate,
		song.Kuwo:    NewKuwo(http).gate,
		song.Netease: NewNetease(http).gate,
	}

	seen := map[*guard.Gate]song.Source{}
	for name, gate := range gates {
		if gate == nil {
			t.Fatalf("%s 没挂闸门: 高并发时会直接把上游打爆", name)
		}
		if other, dup := seen[gate]; dup {
			t.Fatalf("%s 和 %s 共用同一个闸门, 一个挂了会连带拖死另一个", name, other)
		}
		seen[gate] = name
	}
}
