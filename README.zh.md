# zlib

Z-Library 命令行客户端。

![Go version](https://img.shields.io/badge/go-1.25%2B-blue)
[![CI](https://img.shields.io/github/actions/workflow/status/heartleo/zlib/release.yml)](https://github.com/heartleo/zlib/actions)
[![Release](https://img.shields.io/github/v/release/heartleo/zlib)](https://github.com/heartleo/zlib/releases)
[![Downloads](https://img.shields.io/github/downloads/heartleo/zlib/total)](https://github.com/heartleo/zlib/releases)
![License](https://img.shields.io/badge/license-MIT-green)

[English](README.md) | 中文

![search demo](docs/demo-search.gif)

## 功能

- 🔍 **交互式搜索** — `↑/↓` 浏览结果，`←/→` 翻页
- 📥 **书籍下载** — 通过 ID 或搜索结果直接下载，实时进度显示
- 📚 **下载历史** — 分页浏览历史记录，支持重新下载
- 📖 **发送到 Kindle** — 通过 SMTP 投递文件到 Kindle
- 🕒 **用量查看** — 查看每日下载配额
- 🎨 **主题** — auto、mocha、latte、dracula、tokyo、nord、gruvbox
- 🌐 **代理和自定义域名** — 支持受限网络环境

## 安装

**Homebrew**（macOS / Linux）：

```bash
brew install heartleo/tap/zlib
```

**winget**（Windows）：

```powershell
winget install heartleo.zlib
```

**curl**（macOS / Linux）：

```bash
curl -fsSL https://raw.githubusercontent.com/heartleo/zlib/main/install.sh | sh
```

**预编译二进制** — 从 [GitHub Releases](https://github.com/heartleo/zlib/releases) 下载：

| 平台            | 文件                                  |
| --------------- | ------------------------------------- |
| Linux x86\_64   | `zlib_<version>_linux_x86_64.tar.gz`  |
| Linux arm64     | `zlib_<version>_linux_arm64.tar.gz`   |
| macOS x86\_64   | `zlib_<version>_darwin_x86_64.tar.gz` |
| macOS arm64     | `zlib_<version>_darwin_arm64.tar.gz`  |
| Windows x86\_64 | `zlib_<version>_windows_x86_64.zip`   |
| Windows arm64   | `zlib_<version>_windows_arm64.zip`    |

**Go install**（需要 Go 1.25+）：

```bash
go install github.com/heartleo/zlib/cmd/zlib@latest
```

**从源码构建：**

```bash
git clone https://github.com/heartleo/zlib
cd zlib
go build -o zlib ./cmd/zlib
```

### Claude Code 插件

zlib 提供 [Claude Code](https://claude.com/claude-code) 插件，让 Claude 帮你搜书、下书、传书到Kindle。

```
/plugin marketplace add heartleo/zlib
/plugin install zlib@zlib-plugin-cc
```

`/zlib "the pragmatic programmer"` 搜索并下载。  
`/zlib:kindle` 把书发送到你的 Kindle。

## 快速开始

```bash
zlib login
zlib search
zlib search "dune"
```

## 命令

### login

![login demo](docs/demo-login.gif)

```bash
zlib login
zlib login --email you@example.com --password secret
```

会话保存至 `~/.config/zlib/session.json`。

### logout

```bash
zlib logout
```

### search

![search demo static](docs/demo-search-static.gif)

不带参数时进入交互模式：

- 输入关键词并确认
- `↑/↓` 浏览结果
- `←/→` 切换页面
- `Enter` 下载

```bash
zlib search # 交互模式
zlib search "dune" --page 2 # 静态表格
zlib search "dune" --json   # 机器可读输出，供脚本和插件使用
```

用 `--ext`(别名 `--format`)按文件格式过滤,可重复指定。

```bash
zlib search "python crash course" --ext epub --ext pdf
```

使用 `--full-title` 关闭标题字符截断:

```bash
zlib search "civilized to death" --full-title
```

### download

```bash
zlib download Gz31nyAV5E
zlib download Gz31nyAV5E --dir ./books --send-to-kindle
zlib download Gz31nyAV5E --dir "~/Downloads"
```

`--dir` 会自行展开开头的 `~`，加不加引号效果一致。

按 `Ctrl+C` 取消下载，未完成的文件会自动删除。

### history

![history demo](docs/demo-history.gif)

不带参数时进入交互模式：

- `↑/↓` 浏览，`←/→` 翻页
- `Enter` 重新下载

```bash
zlib history
zlib history --download Gz31nyAV5E --dir ./books
zlib history --format epub
zlib history --json   # 机器可读，始终非交互
```

### profile

![profile demo](docs/demo-profile.gif)

```bash
zlib profile
zlib profile --json
```

### kindle

![kindle demo](docs/demo-kindle.gif)

配置 Kindle 投递设置：

- 收件 Kindle 邮箱
- 发件邮箱
- SMTP 服务器和端口

SMTP 密码不会存储在磁盘上，请通过 `ZLIB_SMTP_PWD` 环境变量设置。

```bash
zlib kindle                  # 配置
zlib kindle send             # 交互式选择文件
zlib kindle send ./dune.epub # 发送指定文件
```

支持的格式：`EPUB` `PDF` `MOBI` `TXT` `DOC` `DOCX` `RTF` `HTML`

### theme

```bash
zlib theme           # 查看当前主题
zlib theme auto      # 跟随终端背景
zlib theme nord      # 设置主题
```

可选：`auto` · `mocha` · `latte` · `dracula` · `tokyo` · `nord` · `gruvbox`

## 配置

敏感值（如 `ZLIB_SMTP_PWD`）请写入 `.env` 文件，别用 `export` 内联以免留在 shell 历史里。zlib 读取环境变量的优先级：**真实环境变量 > 工作目录 `.env` > `~/.config/zlib/.env`**。全局文件是机器级配置的推荐存放处。

| 变量            | 说明                                 |
| --------------- | ------------------------------------ |
| `ZLIB_DOMAIN`   | 覆盖默认的 Z-Library 域名            |
| `ZLIB_PROXY`    | 代理地址，如 `http://127.0.0.1:7890` |
| `ZLIB_SMTP_PWD` | Kindle 投递的 SMTP 密码              |
| `ZLIB_THEME`    | 覆盖主题，无需修改配置文件           |

**编辑全局 env 文件**（`~/.config/zlib/.env`，每行一个 `KEY=value`）：

```bash
mkdir -p ~/.config/zlib && touch ~/.config/zlib/.env && chmod 600 ~/.config/zlib/.env
# 用编辑器打开：
notepad "$env:USERPROFILE\.config\zlib\.env"   # Windows (PowerShell)
open -t ~/.config/zlib/.env                       # macOS
${EDITOR:-nano} ~/.config/zlib/.env               # Linux
```
