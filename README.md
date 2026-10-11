# 点歌

> Aurorix 框架的点歌插件。群里发一句歌名，机器人搜歌；5 分钟以内发语音，超过发文件。

## 安装

```bash
git clone https://github.com/KasumiYuku/Aurorix.git && cd Aurorix
make install && cd ..
aurx new mybot --framework Aurorix
cd mybot && aurx add github.com/AurorixArhive/aurorix-music-plugin
aurx run          # 先把 appid / secret 填进 config.json
```

## 命令

| 命令 | 作用 |
|---|---|
| `/play <歌名>` | 搜歌，点卡片上蓝色歌名直接发送 |
| `/play <歌名> -s qq` | 只在指定音源找：`qq` / `kugou` / `kuwo`，中文名也认 |
| `/play` | 用法 |

别名：`/点歌`、`/点播`、`/music`。

## 音源

| 音源 | 渠道 | 走什么 |
|---|---|---|
| `qq` | `qq.official` | QQ 官方 musicu.fcg；会员曲要填 Cookie |
| `kugou` | `kg.molan` | 海棠湾聚合，拿不到再问酷狗官方 |
| `kuwo` | `kuwo.xinghai` | 酷我官方直连 |
| `wyy` | `wyy.ikun` | 网易云官方搜索 + IKUN(`c.wwwweb.top`)取链 |

不带 `-s` 时按**默认优先级 qq → kugou → kuwo → wyy** 找，前一家没结果才试下一家。

## 配置

管理台 → 插件 → 点歌，改完立刻生效。

| 配置项 | 作用 |
|---|---|
| 音源优先级 | 逗号分隔，改不带 `-s` 时的顺序 |
| 关闭的音源 | 关掉的不进默认顺序，`-s` 仍可用 |
| QQ 音乐 Cookie | 粘 y.qq.com 的 Cookie 才能听会员曲，留空只有免费曲 |

## 加音源

一个源一个包，`init()` 里注册，再到 `internal/channel/all/all.go` 加一行 import：

```go
channel.Register(channel.Channel{
	ID: "qq.mysource", Platform: song.QQ, Label: "我的源",
	Search: search,   // 能搜歌就填，只做直链可以留空
	Resolve: resolve, // 必填
})
```

新平台在 `internal/song/song.go` 的平台表加一行。现成模板看 `internal/channel/`。

## 开发

```bash
go test -short ./...                      # 离线
go test -run TestEndToEnd -v ./           # 真联网端到端
go test -run TestProbeAllChannels -v ./   # 各渠道体检
```

## 许可

[MIT](LICENSE)。本插件为非官方插件，与各音乐平台无关；音乐版权归各权利人所有。
