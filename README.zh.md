<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark-theme.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/logo-light-theme.svg">
  <img src="docs/logo-light-theme.svg" alt="zlib" width="260">
</picture>

Z-Library 命令行工具，你和 AI Agent 都能用。

<a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/heartleo/zlib" alt="Go version" height="20"/></a> <a href="https://github.com/heartleo/zlib/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/heartleo/zlib/ci.yml" alt="CI" height="20"/></a> <a href="https://github.com/heartleo/zlib/releases/latest"><img src="https://img.shields.io/github/v/release/heartleo/zlib" alt="Release" height="20"/></a> <a href="https://github.com/heartleo/zlib/releases"><img src="https://img.shields.io/github/downloads/heartleo/zlib/total" alt="Downloads" height="20"/></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License" height="20"/></a>

<a href="https://trendshift.io/repositories/135168?utm_source=repository-badge&amp;utm_medium=badge&amp;utm_campaign=badge-repository-135168" target="_blank" rel="noopener noreferrer"><img src="https://trendshift.io/api/badge/repositories/135168" alt="heartleo%2Fzlib | Trendshift" width="250" height="55"/></a>

[English](README.md) | 简体中文

[安装](#安装) · [快速开始](#快速开始) · [终端](#终端) · [常用命令](#常用命令) · [Agent 接入](#agent-接入) · [文档](#文档)

</div>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/search.gif">
  <source media="(prefers-color-scheme: light)" srcset="docs/search-light.gif">
  <img src="docs/search.gif" width="100%" alt="终端搜书演示">
</picture>

## 功能

🔍 **搜索** — 按年份、语言和格式筛选，结果可排序\
📚 **浏览** — 收藏、推荐、热门图书和下载历史\
📥 **下载** — 查看书籍详情，选择下载目录\
📖 **发送到 Kindle** — 把文件投递到你的 [Kindle 邮箱](docs/kindle.md)\
🕒 **用量查看** — 查看每日下载配额\
🎨 **主题** — 用 `/theme` 切换配色\
🌐 **网络** — 用 `zlib doctor` 找到可用镜像，可[添加自己信任的镜像](docs/configuration.md#your-own-mirrors)，也支持代理\
🤖 **AI Agent 集成** — 通过 [Agent 插件](#agent-接入)集成 Claude Code 和 Codex

## 安装

macOS / Linux，使用 Homebrew：

```bash
brew install heartleo/tap/zlib
```

macOS / Linux，使用安装脚本（安装到 `/usr/local/bin`，可能需要 `sudo`）：

```bash
curl -fsSL https://raw.githubusercontent.com/heartleo/zlib/main/install.sh | sh
```

Windows，使用 winget：

```powershell
winget install --id heartleo.zlib --exact
```

也可以从 [Releases](https://github.com/heartleo/zlib/releases/latest) 下载程序，
放入 `PATH` 目录。Go 安装、源码编译和更新方式见[安装指南](docs/installation.md)。

## 快速开始

使用前需要一个 Z-Library 账号。先检测当前网络可用的域名：

```sh
zlib doctor
```

选择结果中的域名，将下方示例 URL 替换为检测结果中的地址。
运行后在表单中输入邮箱和密码：

```sh
zlib login --domain https://z-lib.gd
zlib tui
```

按 `/`，选择 `/search`，输入书名、作者或 ISBN，按 `Ctrl+S` 搜索。
也可以直接输入 `/search dune` 后按 `Enter`。选中图书后按 `Enter`，确认后开始下载。

默认下载到当前目录，也可以在下载前选择其他目录。
登录状态保存在本地，后续直接运行 `zlib tui` 即可。

没有可用域名或登录失败时，参阅[排障指南](docs/troubleshooting.md)。
已有浏览器会话也可以通过[导入 Cookie](docs/troubleshooting.md#login-returns-authorization-failed)登录。

## 终端

`zlib tui` 打开一个常驻的终端界面。每个页面底部都会列出可用按键，
第一次使用记住下面这些就够了：

| 按键           | 作用                                             |
| -------------- | ------------------------------------------------ |
| `/`            | 打开命令列表：搜索、收藏、历史、账号、设置、主题 |
| `↑/↓` 或 `j/k` | 选择图书                                         |
| `←/→`          | 上一页或下一页结果                               |
| `Enter`        | 下载选中的图书（先确认）                         |
| `Ctrl+F`       | 修改当前搜索的查询词和过滤条件                   |
| `Esc`          | 返回                                             |
| `/quit`        | 退出                                             |

设置在 `/config` 下（网络、下载、Kindle）。全部页面和按键见[终端界面说明](docs/tui.md)。

## 常用命令

带查询词时输出表格，加上 `--json` 则输出供脚本使用的 JSON：

```sh
zlib search "dune" --ext epub
zlib search "dune" --ext epub --json --count 10
```

| 操作                            | 命令                           |
| ------------------------------- | ------------------------------ |
| 浏览收藏                        | `zlib favorites`               |
| 浏览推荐                        | `zlib recommended`             |
| 浏览热门图书                    | `zlib most-popular`            |
| 浏览下载历史                    | `zlib history`                 |
| 查看账号与配额                  | `zlib profile`                 |
| 发送到 [Kindle](docs/kindle.md) | `zlib kindle send ./book.epub` |

下载时，将 `id:hash` 替换为结果中书籍的完整 `id`：

```sh
zlib download "id:hash"
```

运行 `zlib <command> --help` 查看参数。
过滤条件、分页和下载选项见[使用指南](docs/usage.md)。

## Agent 接入

可安装 Agent 插件，或直接注册 MCP 服务（`zlib mcp`）。没有 `zlib` 时，插件首次使用会自行安装。登录请自己运行 `zlib login`，密码不要发到对话里。

### Claude Code

安装插件：

```text
/plugin marketplace add heartleo/zlib
/plugin install zlib@zlib-plugin-cc
```

### Codex

安装插件：

```sh
codex plugin marketplace add heartleo/zlib
codex plugin add zlib@zlib
```

### MCP 服务

把 `zlib mcp` 注册为本地 stdio 服务。需已安装 `zlib` 并完成登录：

```sh
# Claude Code
claude mcp add -s user zlib -- zlib mcp

# Codex
codex mcp add zlib -- zlib mcp
```

在 `mcp` 后加 `--read-only` 可禁用下载、删除历史和发送到 Kindle。其他客户端、参数和工具列表见
[MCP 服务](docs/mcp.md)。

## 文档

| 指南                                | 内容                                |
| ----------------------------------- | ----------------------------------- |
| [安装指南](docs/installation.md)    | 安装方式、源码编译、`zlib update`   |
| [使用指南](docs/usage.md)           | 搜索、下载、收藏、历史              |
| [终端界面](docs/tui.md)             | 命令和快捷键                        |
| [配置指南](docs/configuration.md)   | 域名、代理、目录、配置路径          |
| [主题指南](docs/themes.md)          | 配色列表和预览操作                  |
| [Kindle](docs/kindle.md)            | SMTP 配置与发送                     |
| [MCP 服务](docs/mcp.md)             | `zlib mcp` 的客户端配置、参数和工具 |
| [排障指南](docs/troubleshooting.md) | 登录和网络问题                      |

遇到问题请在 [Issues](https://github.com/heartleo/zlib/issues) 附上
`zlib version`、操作系统、所用命令和报错。请勿附带密码、Cookie 或会话文件。
安全漏洞请按 [SECURITY.md](.github/SECURITY.md) 私下报告，不要公开提 issue。

## Star History

<a href="https://www.star-history.com/?repos=heartleo%2Fzlib&type=date&logscale=&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=heartleo/zlib&type=date&theme=dark&logscale&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=heartleo/zlib&type=date&logscale&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=heartleo/zlib&type=date&logscale&legend=top-left" />
 </picture>
</a>
