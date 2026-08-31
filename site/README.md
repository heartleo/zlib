# zlib.heartleo.dev

The landing page for [zlib](https://github.com/heartleo/zlib). It is one
self-contained `index.html` with no build step and no dependencies. The only
external request it makes is for the Google Fonts stylesheet.

## Deploy

GitHub Pages serves this directory, and Cloudflare only holds the DNS record.
It is the same arrangement as
[hn-cli](https://github.com/heartleo/hn-cli) uses for hndigest.heartleo.dev.

`.github/workflows/pages.yml` deploys on every push to `main` that touches
`site/**`. The first run also switches the repository's Pages source to
"GitHub Actions", so there is nothing to set up in Settings beforehand.

### Cloudflare DNS

Add one record under `heartleo.dev`:

| Field  | Value                |
| ------ | -------------------- |
| Type   | `CNAME`              |
| Name   | `zlib`               |
| Target | `heartleo.github.io` |
| Proxy  | **DNS only** (grey cloud) |
| TTL    | Auto                 |

The proxy has to stay off. GitHub issues the certificate for the custom
domain itself, and it can only do that when the record resolves to GitHub's
own addresses. Orange-cloud it and the cert never provisions.

Check it with `dig +short zlib.heartleo.dev`. The answer should be
`heartleo.github.io.` followed by four `185.199.*.153` addresses.

### GitHub

`site/CNAME` sets the custom domain, so the first deploy fills in
Settings > Pages > Custom domain by itself. Once the DNS check passes, tick
**Enforce HTTPS** there. The certificate takes a few minutes.

## Preview locally

```bash
python3 -m http.server 8000 --directory site
# http://localhost:8000
```

## Notes

- Colors are Catppuccin Latte (light) and Mocha (dark), the same two
  palettes the CLI ships as `zlib theme latte|mocha`, so the site and the
  tool match. Terminal blocks stay Mocha in both modes.
- The hero is a replica of the `zlib search` picker. Key handling mirrors
  `internal/cli/search_interactive.go`: `↑/↓` `j/k` select, `←/→` `h/l`
  page, `enter` download, `q` quit. Column widths match the Go source.
- Copy is bilingual. Each string is a sibling pair of
  `data-i18n="en"` / `data-i18n="zh"` elements; CSS shows one based on
  `<html data-lang>`. Add a language by adding a third value.
- Book listings and `doctor` output are illustrative sample data, not live
  results.
- `og.png` is the social card (1200x630). It is a screenshot of a small
  standalone HTML card, not an export from a design tool, so regenerating it
  means re-rendering that card at 1200x630 and saving the PNG here. The
  `og:image` and `twitter:image` tags point at it by absolute URL, which is
  what the scrapers require.
- The page carries `SoftwareApplication` JSON-LD. Every field in it is
  checked against the README and `go.mod`; update it when the supported
  platforms or the Go version change.
