package channel

import (
	"context"
	"sort"

	"github.com/AurorixArhive/aurorix-music-plugin/internal/song"
)

type Searcher func(ctx context.Context, keyword string, limit int) ([]song.Song, error)

type Resolver func(ctx context.Context, pick song.Song) (song.Song, error)

type Channel struct {
	ID       string
	Platform song.Platform
	Label    string
	Search   Searcher
	Resolve  Resolver
}

var registry []Channel

func Register(list ...Channel) { registry = append(registry, list...) }

func All() []Channel { return append([]Channel(nil), registry...) }

func For(platform song.Platform) []Channel {
	out := make([]Channel, 0, 4)
	for _, c := range registry {
		if c.Platform == platform {
			out = append(out, c)
		}
	}
	return out
}

func Searchable(platform song.Platform) []Channel { return filter(platform, true) }

func Resolvable(platform song.Platform) []Channel { return filter(platform, false) }

func filter(platform song.Platform, needSearch bool) []Channel {
	out := make([]Channel, 0, 4)
	for _, c := range registry {
		if c.Platform != platform {
			continue
		}
		if needSearch && c.Search == nil {
			continue
		}
		if !needSearch && c.Resolve == nil {
			continue
		}
		out = append(out, c)
	}
	return out
}

func Platforms() []song.Platform {
	seen := map[song.Platform]bool{}
	out := make([]song.Platform, 0, len(registry))
	for _, c := range registry {
		if !seen[c.Platform] {
			seen[c.Platform] = true
			out = append(out, c.Platform)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
