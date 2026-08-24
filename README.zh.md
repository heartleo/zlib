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

🔍 **交互式搜索** — `↑/↓` 浏览结果，`←/→` 翻页  
📥 **书籍下载** — 通过 ID 或搜索结果直接下载，实时进度显示  
📚 **下载历史** — 分页浏览历史记录，支持重新下载  
📖 **发送到 Kindle** — 通过 SMTP 投递文件到 Kindle  
🕒 **用量查看** — 查看每日下载配额  
🎨 **主题** — auto、mocha、latte、dracula、tokyo、nord、gruvbox  
🌐 **代理和自定义域名** — 支持受限网络环境  

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

目前大多数 Z-Library 域名都对自动请求启用了 DiamWall，推荐使用 EAPI。
先探测候选域名，再把 `ZLIB_DOMAIN` 设为结果为 `healthy` 或 `challenged` 的域名：

```bash
zlib doctor --eapi
zlib login --eapi --domain https://z-lib.gd
# or export ZLIB_DOMAIN=https://z-lib.gd, then:
# zlib login --eapi
zlib search
zlib search "dune"
```

`challenged` 表示该域名返回的是 Z-Library 自带的 JS 计算题页面。这类域名**可以正常使用**，
客户端会自动求解，选它和选 `healthy` 一样稳妥。只有 `diamwall_blocked`、`http_error`、
`redirect_loop`、`network_error` 才是不可用的。

输入账号前，EAPI 登录会再次检查该域名。如果没有可用域名，请参考
[DiamWall 排障](#diamwall-排障issue-16)。

## 命令

### login

![login demo](docs/demo-login.gif)

```bash
zlib login
zlib login --email you@example.com --password secret
zlib login --cookies ~/Downloads/cookies.txt --domain https://z-lib.gd
zlib login --eapi --domain https://z-lib.gd
# 或先设置 ZLIB_DOMAIN=https://z-lib.gd，再执行：
zlib login --eapi
```

会话保存至 `~/.config/zlib/session.json`。
交互式登录会记住并预填最近一次成功登录的邮箱，但不会保存密码。
`--cookies` 用于导入用户主动提供的 Netscape/Mozilla 格式 Cookie 文件；
只有与 `--domain` 匹配、未过期且作用于根路径的 Cookie 才会写入会话。
Cookie 文件包含可复用凭据，请妥善保护导出的文件。

省略 `--domain` 时，HTML 和 EAPI 登录都读取 `ZLIB_DOMAIN`。两者都没有设置，
命令会打印设置方法并退出。EAPI 登录会先检查所选域名，再要求输入账号；检查失败
时会提示更换域名。导入 Cookie 也遵循这套规则。

`--eapi` 选择非官方移动端 API，并把选择写入会话。搜索、profile、history、
图书详情、下载和 Kindle 投递会按需走 EAPI。图书引用格式为 `id:hash`，例如
`zlib download 115066162:e9f13b --send-to-kindle`。端点和稳定性说明见
[EAPI 模式](docs/eapi.md)。

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

### doctor

在不登录、不修改当前域名的情况下检查所有已知域名。
`--proxy` 仅对此次检查生效，并覆盖 `ZLIB_PROXY`。

```bash
zlib doctor
zlib doctor --eapi
zlib doctor --proxy http://127.0.0.1:7890
zlib doctor --proxy socks5://127.0.0.1:9050 --json
```

`--eapi` 检查 `/eapi/info/domains`，不检查首页。网站和 EAPI 的结果可能不同：
首页被 DiamWall 拦截时，EAPI 仍可能可用。

### DiamWall 排障（issue #16）

[Issue #16](https://github.com/heartleo/zlib/issues/16) 中，镜像返回的是
DiamWall 挑战页，CLI 因此拿不到预期的 HTML 或 JSON。错误可能是 HTTP `307`
重定向循环、HTTP `517` 或 `Access Denied | DiamWall`。旧版 CLI 可能只显示
`searchResultBox not found`、`dstats-info not found`，或在解析登录 JSON 时报告
`invalid character '<'`。

1. 分别检查网页和 EAPI：

   ```bash
   zlib doctor --json
   zlib doctor --eapi --json
   ```

2. 将 `ZLIB_DOMAIN` 设为结果为 `healthy` 或 `challenged` 的域名，再用 EAPI 登录：

   ```bash
   export ZLIB_DOMAIN=https://z-lib.gd
   zlib login --eapi
   zlib search "golang"
   ```

3. 网络需要代理时，先通过该代理探测，确认可用后再供其他命令使用：

   ```bash
   zlib doctor --eapi --proxy http://127.0.0.1:7890
   export ZLIB_PROXY=http://127.0.0.1:7890
   ```

`challenged` 表示该域名返回的是 Z-Library 自带的 JS 计算题页面，客户端会自动求解，
因此该域名可用。`diamwall_blocked` 表示 DiamWall 截获了请求，`redirect_loop` 表示
重定向没有到达真实内容，`http_error` 包含 `403` 等响应，`network_error` 包含
DNS、TLS、超时、连接重置和 EOF。更换 User-Agent 或导入登录 Cookie 无法解决
DiamWall 的 JS/TLS 挑战。此时应改用 `healthy` 或 `challenged` 的 EAPI 域名，
或更换网络出口。

### 推荐访问域名

来源：[r/zlibrary 访问 Wiki](https://www.reddit.com/r/zlibrary/wiki/index/access/?screen_view_count=1&ext-referrer=EXTERNAL#wiki_how_to_access_zlibrary_through_your_browser)

| 可用域名       |
| -------------- |
| `z-library.gs` |
| `1lib.sk`      |
| `z-lib.fm`     |
| `z-lib.gd`     |
| `z-lib.gl`     |
| `zliba.ru`     |
| `z-lib.sk`     |
| `z-library.ec` |

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

HTML 和 EAPI 都读取 `ZLIB_DOMAIN`。它的优先级高于 `~/.config/zlib/session.json` 中保存的域名；清空后，CLI 使用 session 域名。新登录省略 `--domain` 时必须设置 `ZLIB_DOMAIN`。EAPI 登录会先检查该域名，再要求输入账号。

| 变量                    | 说明                                       |
| ----------------------- | ------------------------------------------ |
| `ZLIB_DOMAIN`           | HTML/EAPI 共用域名；省略 `--domain` 时使用 |
| `ZLIB_PROXY`            | 代理地址，如 `http://127.0.0.1:7890`       |
| `ZLIB_SMTP_PWD`         | Kindle 投递的 SMTP 密码                    |
| `ZLIB_THEME`            | 覆盖主题，无需修改配置文件                 |
| `ZLIB_DOWNLOAD_RETRIES` | 连接中断与 `502`/`503`/`504` 的重试次数    |

`ZLIB_DOWNLOAD_RETRIES` 默认 `3`。

**编辑全局 env 文件**（`~/.config/zlib/.env`，每行一个 `KEY=value`）：

```bash
mkdir -p ~/.config/zlib && touch ~/.config/zlib/.env && chmod 600 ~/.config/zlib/.env
# 用编辑器打开：
notepad "$env:USERPROFILE\.config\zlib\.env"   # Windows (PowerShell)
open -t ~/.config/zlib/.env                       # macOS
${EDITOR:-nano} ~/.config/zlib/.env               # Linux
```
