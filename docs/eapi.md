# EAPI

[README](../README.md) · [Configuration](configuration.md) · [Troubleshooting](troubleshooting.md)

Some Z-Library domains answer non-browser requests with a DiamWall redirect loop,
while the mobile EAPI on those domains may still work. The CLI reads structured
JSON directly and does not handle DiamWall challenges.

## Quick use

```bash
# Check the EAPI health of candidate domains first
zlib doctor

# Name the domain explicitly
zlib login --domain https://z-lib.gd

# Or set it in the shell / ~/.config/zlib/.env and omit --domain
export ZLIB_DOMAIN=https://z-lib.gd
zlib login

# Later commands use the EAPI directly
zlib search "golang"
zlib search "golang" --json
```

You can also import a Netscape Cookie file you exported yourself:

```bash
zlib login --cookies ./cookies.txt --domain https://z-lib.gd
```

The session stores only the Cookies the server returned or you imported, never
the password. Protect the Cookie file and `~/.config/zlib/session.json` as you
would the account password.

## Current scope

| Feature | Status | Endpoint |
| --- | --- | --- |
| Login | Supported | `POST /eapi/user/login`; Cookie file import |
| Plain and interactive search | Supported | `POST /eapi/book/search` |
| Recommended books | Supported | `GET /eapi/user/book/recommended` |
| Most popular books | Supported | `GET /eapi/book/most-popular` |
| Domain diagnostics | Supported | `GET /eapi/info/domains` |
| `profile` account details and download quota | Supported | `GET /eapi/user/profile` |
| `history` | Supported | `GET /eapi/user/book/downloaded` |
| Favorites (`favorites`) | Supported | `GET /eapi/user/book/saved` |
| Book details | Supported | `GET /eapi/book/{id}/{hash}` |
| Download link | Supported | `GET /eapi/book/{id}/{hash}/file` |
| Kindle delivery after download | Supported | Local SMTP after the EAPI download |

Full-text search on the website has no confirmed EAPI equivalent;
`FullTextSearch` returns an explicit unsupported error.

EAPI search and history results keep the numeric ID and the hash. On the command
line a reference is written `id:hash`:

```bash
zlib download 115066162:e9f13b
zlib download 115066162:e9f13b --send-to-kindle
zlib history --download 115066162:e9f13b --send-to-kindle
```

When a book is selected, the TUI requests `/eapi/book/{id}/{hash}` on demand to
fill in the details card and caches it by `id:hash`. The Android `ZLibBook`
model explicitly includes `edition`, `pages` and `identifier`; categories and
separate ISBN 10/13 fields are not guaranteed by the APK model alone. The client
shows only the categories present in the response, and derives ISBN numbers only
from a valid ISBN.

## Separate controls on the login endpoint

Z-Library controls `/eapi/user/login` separately from the rest of the EAPI. A
measurement with a real account on 2026-09-04: the endpoint returned
`400 {"success":0,"error":"Authorization failed"}`, while the same account, using
a key issued by `/rpc.php`, got `success:1` from `/eapi/user/profile`,
`/eapi/book/search` and `/eapi/user/book/downloaded`. z-lib.gd, z-lib.gl,
z-library.ec, article.sk and zlib.bz all behaved the same, so it is not a domain
problem.

It is not the password being refused: with a wrong password the endpoint returns
`Incorrect email or password`.

EAPI requests use `remix_userid`/`remix_userkey`. If the login endpoint refuses
the account, the CLI returns the server's error; you can import a Netscape Cookie
file exported from a browser and keep using the EAPI.

## Domain allowlist

The CLI accepts `1lib.sk`, `article.sk`, `articles.sk`, `z-lib.by`, `z-lib.fm`,
`z-lib.gd`, `z-lib.gl`, `z-lib.sk`, `z-library.ec`, `z-library.im`,
`z-library.qa`, `z-library.sk`, `z-library.website` and `zlib.bz` out of the box.
Subdomains of these domains also work. The configured value must be an HTTPS
origin with no port, path, query or credentials.

Measured on 2026-09-04: the EAPI worked on `z-lib.gd`, `z-lib.gl`,
`z-library.ec`, `article.sk`, `articles.sk` and `zlib.bz`; `1lib.sk`, `z-lib.fm`
and `z-lib.sk` were blocked by DiamWall; `zliba.ru` had become a pure redirector
to `zlib.bz`, which login follows and then switches to the final domain.
`z-library.gs` was removed from the allowlist after TLS handshakes failed.

Measured `/eapi/info/domains` on 2026-10-25 against the October list from
r/zlibrary: `z-lib.by`, `z-library.qa` and `z-library.website` returned EAPI
JSON directly; `z-library.im` and `z-library.sk`, like `z-lib.sk`, `z-lib.fm`
and `1lib.sk`, 307-redirected back to themselves (DiamWall), but they are
official primary domains, so they are on the allowlist too and work directly once
unblocked. `zliba.ru` is not on the list; it only redirects to `zlib.bz`, has been
removed from the allowlist, and you should use `zlib.bz` directly. Domains not in
the built-in list can be trusted with `zlib mirrors add`.

In Italy, France or Spain, try `z-library.ec`. Governments or ISPs in Italy,
France, Spain, the United Kingdom, Germany, India, Denmark and China may block
these domains. When a browser cannot load them, changing DNS may help in some
regions other than France. A VPN, or Tor Browser with the onion site, also works.

A new login uses `--domain` first and falls back to `ZLIB_DOMAIN`. If neither is
set, the command prints how to set one and exits. EAPI login also checks
`/eapi/info/domains`; when that check fails, the command suggests running
`zlib doctor` to pick a healthy domain. If the check redirects to another host,
login switches to the final domain and validates it against the allowlist on its
own. Other commands prefer the environment variable and read the session domain
only when it is unset, so a leftover `ZLIB_DOMAIN` overrides the domain you just
logged in to; this is a common cause of "login succeeded but later commands
fail".

## Stability

The EAPI has no public, stable, officially authorized contract. The endpoint
definitions follow the mobile endpoints documented in
[baroxyton/zlibrary-eapi-documentation](https://github.com/baroxyton/zlibrary-eapi-documentation)
and were verified during development against live health endpoints and search
responses. Upstream may change fields or authentication, or shut domains down.
Run `zlib doctor` before use, and do not rely on any one domain staying available.

## Android API research

The [initial APK analysis](android-api-research.md) records the reverse
engineering method; the [deep dive and measurements](android-eapi-deep-dive.md)
compares the Android endpoint declarations, server responses and the current CLI,
and lists commands that could be added. Read the conclusions within the dates and
test scope stated in each document.
