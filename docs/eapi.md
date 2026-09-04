# EAPI 模式

部分 Z-Library 域名会向非浏览器请求返回 DiamWall 重定向循环，但这些域名的
移动端 EAPI 仍可能可用。EAPI 模式直接读取结构化 JSON，不处理 DiamWall 挑战。

## 快速使用

```bash
# 先检查候选域名的 EAPI 健康状态
zlib doctor --eapi

# 显式指定域名
zlib login --eapi --domain https://z-lib.gd

# 或写入 shell / ~/.config/zlib/.env 后省略 --domain
export ZLIB_DOMAIN=https://z-lib.gd
zlib login --eapi

# 会话已经记住 eapi 模式，后续无需重复传参
zlib search "golang"
zlib search "golang" --json
```

你也可以导入自己导出的 Netscape Cookie 文件：

```bash
zlib login --eapi --cookies ./cookies.txt --domain https://z-lib.gd
```

会话只保存服务端返回或导入的 Cookie，不保存密码。Cookie 文件和
`~/.config/zlib/session.json` 都应按账号密码同等保护。

## 当前范围

| 功能 | 状态 | 端点 |
| --- | --- | --- |
| 登录 | 支持 | `POST /eapi/user/login`，被拒时回退 `POST /rpc.php` |
| 静态和交互式搜索 | 支持 | `POST /eapi/book/search` |
| 域名诊断 | 支持 | `GET /eapi/info/domains` |
| profile 下载配额 | 支持 | `GET /eapi/user/profile` |
| history | 支持 | `GET /eapi/user/book/downloaded` |
| 图书详情 | 支持 | `GET /eapi/book/{id}/{hash}` |
| 下载链接 | 支持 | `GET /eapi/book/{id}/{hash}/file` |
| 下载后 Kindle 投递 | 支持 | EAPI 下载后使用本地 SMTP |

EAPI 搜索和历史结果会保留数字 ID 与 hash。命令行引用写成 `id:hash`：

```bash
zlib download 115066162:e9f13b
zlib download 115066162:e9f13b --send-to-kindle
zlib history --download 115066162:e9f13b --send-to-kindle
```

## 登录端点的单独管控

Z-Library 把 `/eapi/user/login` 与 EAPI 的其余部分分开管控。2026-09-04 对一个真实
账号实测：该端点返回 `400 {"success":0,"error":"Authorization failed"}`，而同一账号
用 `/rpc.php` 下发的 key 访问 `/eapi/user/profile`、`/eapi/book/search`、
`/eapi/user/book/downloaded` 全部返回 `success:1`。z-lib.gd、z-lib.gl、
z-library.ec、article.sk、zlib.bz 五个域名表现一致，因此不是域名问题。

被拒的不是密码：密码错误时该端点返回的是 `Incorrect email or password`。

EAPI 的鉴权只依赖 `remix_userid`/`remix_userkey`，而 `/rpc.php` 会把这一对作为
Cookie 下发，所以 `LoginEAPI` 在原生端点被拒时回退到 HTML 表单取 key，然后保持
EAPI 模式。回退得到的会话与原生端点得到的等价。

若 HTML 表单也拒绝，报错以表单返回的消息为准 —— 真正校验密码的是它。

## 域名白名单

CLI 只接受 `1lib.sk`、`article.sk`、`articles.sk`、`z-lib.fm`、`z-lib.gd`、
`z-lib.gl`、`z-lib.sk`、`z-library.ec`、`zlib.bz` 和 `zliba.ru`。这些域名的子域名
也可以使用。配置值必须是 HTTPS 源站，不能包含端口、路径、查询或凭据。

2026-09-04 实测：`z-lib.gd`、`z-lib.gl`、`z-library.ec`、`article.sk`、
`articles.sk`、`zlib.bz` 的 EAPI 可用；`1lib.sk`、`z-lib.fm`、`z-lib.sk` 被
DiamWall 拦截；`zliba.ru` 已变为跳转到 `zlib.bz` 的纯 redirector，`login --eapi`
会跟随并改用最终域名登录。`z-library.gs` 因 TLS 握手失败已从白名单移除。

意大利、法国或西班牙可尝试 `z-library.ec`。意大利、法国、西班牙、英国、
德国、印度、丹麦和中国的政府或 ISP 可能屏蔽这些域名。浏览器无法加载时，法国
以外的部分地区可以尝试更换 DNS。也可以使用 VPN，或通过 Tor Browser 访问
onion 站点。

新登录优先使用 `--domain`；省略时，EAPI 和 HTML 登录都读取 `ZLIB_DOMAIN`。
如果两者都没有设置，命令会打印设置方法并退出。EAPI 登录还会检查
`/eapi/info/domains`；检查失败时，命令会提示运行 `zlib doctor --eapi` 选择
健康域名。若该检查跳转到了另一台主机，登录会改用最终域名，并对它单独做一次
白名单校验。其他命令优先使用环境变量，没有环境变量时才读取 session 域名 ——
遗留的 `ZLIB_DOMAIN` 因此会让刚刚登录成功的域名失效，是"登录成功但后续命令
报错"的常见原因。

## 稳定性

EAPI 没有公开、稳定的官方授权契约。接口定义参考
[baroxyton/zlibrary-eapi-documentation](https://github.com/baroxyton/zlibrary-eapi-documentation)
记录的移动端接口，开发时通过实时健康端点和搜索响应验证。上游可能修改字段、
鉴权方式或关闭域名。使用前应先运行 `zlib doctor --eapi`，不要依赖某个域名
长期可用。
