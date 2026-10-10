# 点歌

![Go](https://img.shields.io/badge/Go-1.26-blue)
![License](https://img.shields.io/badge/License-MIT-green)

> Aurorix 框架的点歌插件
>
> 在群里发一句歌名，机器人搜歌；三分钟以内发语音，超过发文件。

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
| `/play <歌名>` | 搜歌，返回最多 10 首的卡片；点蓝色歌名直接发送 |
| `/play <歌名> -s <音源>` | 只在指定音源里找 |
| `/play` | 不带参数时回一条用法卡片 |

别名：`/点歌`、`/点播`、`/music`；前缀按实例配置（默认 `/`）。

3 分钟（180 秒）以内发语音，超过发文件；文件按直链直传给 QQ。

## 音源

| 音源 | `-s` |
|---|---|
| QQ音乐 | `qq` |
| 网易云 | `wyy` |
| 百度 | `baidu` |
| 一听 | `1ting` |

默认顺序 `qq,wyy,baidu,1ting`，前一家搜不到就试下一家；用 `-s` 指定后只问那一家。

QQ音乐、百度、一听走 [music.txqq.pro](https://music.txqq.pro/) 聚合站，网易直连官方接口。

## 歌词

音源带歌词时，在歌曲后面补一张歌词图（标题 + 歌手 + 正文，最多 60 行，字体已内嵌）。

开关在管理台「插件 → 点歌 → 发送歌词图」，默认开启。

## 配置

管理台 → 插件 → 点歌，改完立刻生效，不用重启。

| 配置项 | 默认 | 作用 |
|---|---|---|
| 音源优先级 | `qq,wyy,baidu,1ting` | 逗号分隔，前一家搜不到才试下一家 |
| 发送歌词图 | 开 | 关掉就只发歌 |

## 结构

```
music-plugin/
├── plugin.go                装配: 请求客户端 → 音源池 → 点歌指令 → 注册插件
├── templates/markdown/      所有用户可见文案(帮助 / 结果卡片 / 失败提示)
└── internal/
    ├── play/                指令: 参数解析、搜歌、按长度分流语音/文件、歌词图
    ├── source/              音源(聚合站、网易)与直链探测
    ├── lyrics/              LRC 解析、出图、内嵌字体
    ├── song/                歌曲模型与卡片短码
    └── guard/               限速、熔断、并发合并
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
go test -short ./...                            # 离线用例
go test ./...                                   # 加上联网用例
go test -run TestEndToEnd -v ./                  # 端到端: 搜索 → 短码 → 换直链 → 出歌词图
```

## 更新

本插件是标准 Go module，发新版靠 git tag。使用方一行升级：

```bash
aurx update aurorix-music-plugin    # 模块名可以只写一段
```

`aurx update` = `go get -u` + `go mod tidy` + 编译校验；跑完 `aurx run` 重启实例即可。

## 许可

代码采用 [MIT](LICENSE) 协议。内置字体为钉钉进步体，随源码一并分发。

本插件为非官方插件，与各音乐平台无关；音乐版权归各权利人所有。
