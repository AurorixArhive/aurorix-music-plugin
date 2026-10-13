package channel

import (
	"fmt"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/quality"
	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

// Ladder 按顺序把上游的各个档位问一遍, 返回第一份能直接播的。
//
// 档位不是越高越好: 上游给不出高音质时会回退成加密文件 —— 文件是真的, 但谁都打不开;
// 低档位反而常常有明文。所以判断标准是探测结论, 不是档位序号。
// 一个能播的都没有时, 交出最好的那份降级结果当兜底。
func Ladder(levels []string, fetch func(level string) (song.Song, error)) (song.Song, error) {
	var (
		best    song.Song
		hasBest bool
		lastErr error
	)
	for _, level := range levels {
		got, err := fetch(level)
		if err != nil {
			lastErr = err
			continue
		}
		if got.Grade >= quality.Usable {
			return got, nil
		}
		if !hasBest {
			best, hasBest = got, true
		}
	}
	if hasBest {
		return best, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("上游每个档位都没给出直链")
	}
	return song.Song{}, lastErr
}
