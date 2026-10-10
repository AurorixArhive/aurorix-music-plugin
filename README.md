# 点歌

![Go](https://img.shields.io/badge/Go-1.26-blue)
![License](https://img.shields.io/badge/License-MIT-green)

> Aurorix 框架的点歌插件
>
> 在群里发一句歌名，机器人搜歌，并以 QQ 语音发出来 —— 三个音源自动降级，结果是一张能点的卡片。

## 快速开始

本插件运行在 [Aurorix](https://github.com/KasumiYuku/Aurorix) 实例中，请先按框架 README 完成安装：

```bash
git clone https://github.com/KasumiYuku/Aurorix.git
cd Aurorix
make install          # 安装脚手架 aurx（Windows 用 go install ./tools/aurx）
cd ..
aurx new mybot --framework Aurorix
```

然后在实例中导入本插件：

```bash
cd mybot
aurx add github.com/AurorixArhive/aurorix-music-plugin
```

本地开发时把本目录放进实例的 `plugins/`，再 `aurx add <实例module>/plugins/music-plugin`。

把机器人的 `appid` / `secret` 填入 `config.json` 后启动：

```bash
aurx run
```

启动后，在群里 @ 机器人发 `/play help` 看用法，或直接 `/play 歌名` 点歌。

## 命令

| 命令 | 作用 |
|---|---|
| `/play <歌名>` | 搜歌，返回最多 10 首的卡片；点蓝色歌名直接发语音 |
| `/play <歌名> -s <音源>` | 只在指定音源里找：`wyy` 网易云 / `kw` 酷我 / `qq` QQ 音乐 |
| `/play` | 不带参数时回一条用法卡片 |

别名：`/点歌`、`/点播`、`/music`；前缀按实例配置（默认 `/`）。

卡片上只列 5 分钟（300 秒）以内的歌 —— QQ 语音的硬上限就是 300 秒，超了平台会连正文一起打回。

## 音源

默认按 **QQ 音乐 → 酷我 → 网易云** 的顺序降级：前一家搜不到就试下一家。用 `-s` 指定后只在那一家里找，在那家没有就是没有，不会偷偷换别家。

| 音源 | `-s` | 说明 |
|---|---|---|
| QQ 音乐 | `qq` | 官方接口，不需要密钥；免费曲直接能放，配上 Cookie 可解锁付费曲与 320k 音质 |
| 酷我 | `kw` | 移动端公开接口 |
| 网易云 | `wyy` | 公开接口，兜底的一环 |

同一首歌被很多人同时点时，并发取直链会合并成一次，不重复打上游；上游抽风时按限速与熔断自保。

## 配置

管理台 → 插件 → 点歌，改完立刻生效，不用重启。

| 配置项 | 默认 | 作用 |
|---|---|---|
| QQ 音乐 Cookie | 空 | 可选。不填也能搜、能放免费曲；想放付费曲或用 320k 音质，登录 [y.qq.com](https://y.qq.com/) 后把完整 Cookie（含 `uin` / `qqmusic_key` / `qm_keyst`）填这里 |

## 结构

```
music-plugin/
├── plugin.go                装配: 请求客户端 → 音源链 → 点歌指令 → 注册插件
├── templates/markdown/      所有用户可见文案(帮助 / 结果卡片 / 失败提示)
└── internal/
    ├── play/                指令: 参数解析、搜歌、发语音、错误文案
    ├── source/              三个音源各一个文件, 共用一套降级链
    ├── song/                歌曲模型, 与卡片短码的编解码
    └── guard/               限速与熔断, 以及合并并发取直链的 flight
```

## 开发

源码依赖框架，按 `go.mod` 里的相对引用检出：

```text
Projects/
└── Okamikko/            # go.work 工作区
    ├── Aurorix/         # github.com/KasumiYuku/Aurorix
    └── music-plugin/    # 本仓
```

```bash
go test ./...                                   # 全部离线用例: 音源解析、短码、限速熔断、指令流程
go test ./internal/source/ -run Live -v         # 真实网络用例, 需联网
```

## 更新

本插件是标准 Go module，发新版靠 git tag。使用方一行升级：

```bash
aurx update aurorix-music-plugin    # 模块名可以只写一段
```

`aurx update` = `go get -u` + `go mod tidy` + 编译校验；跑完 `aurx run` 重启实例即可。

## 许可

代码采用 [MIT](LICENSE) 协议。

本插件为非官方插件，与各音乐平台无关；音乐版权归各权利人所有。
