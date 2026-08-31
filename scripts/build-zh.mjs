// Generates site/zh/index.html from site/index.html.
//
// site/index.html is bilingual: every translated string ships as an adjacent
// pair of elements, one `data-i18n="en"` and one `data-i18n="zh"`, and CSS
// reveals whichever matches the `data-lang` on <html>. That is fine for a
// single URL, but it gives search engines one page whose Chinese half is
// display:none, so Chinese queries have nothing to rank. This script emits a
// second URL that is Chinese all the way down: the English elements are
// removed outright rather than hidden, and the two pages point at each other
// with hreflang.
//
// Run from the repo root: node scripts/build-zh.mjs

import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const srcPath = resolve(repoRoot, "site/index.html");
const outPath = resolve(repoRoot, "site/zh/index.html");

const ORIGIN = "https://zlib.heartleo.dev";

// Chinese head copy. These mirror the zh strings already on the page rather
// than introducing new wording.
const ZH_TITLE = "zlib — 终端里的 Z-Library 客户端，也能交给 AI agent";
const ZH_DESCRIPTION =
  "搜书、下载、发送到 Kindle，全程不用离开终端。开源的 Z-Library 命令行客户端，自带 Claude Code 和 Codex 插件。";
const ZH_OG_DESCRIPTION =
  "搜书、下载、发送到 Kindle，全程不用离开终端，也可以交给 Claude Code 或 Codex。";
const ZH_OG_IMAGE_ALT = "zlib，终端里的 Z-Library 客户端，图中是它的搜索界面。";

/**
 * Removes every element carrying `data-i18n="en"`, including its children.
 *
 * A regex cannot do this alone: the h1's English span wraps another span, so
 * a lazy `<span ...>.*?</span>` would close on the inner tag and leave stray
 * markup. This walks forward from each match counting same-tag opens and
 * closes instead, which is enough because these elements only ever nest
 * inside elements of their own kind.
 */
function stripEnglishElements(html) {
  const open = /<([a-zA-Z][a-zA-Z0-9]*)\b[^>]*\bdata-i18n="en"[^>]*>/;
  let out = html;

  for (;;) {
    const match = open.exec(out);
    if (!match) return out;

    const tag = match[1];
    const start = match.index;
    const scan = new RegExp(`<${tag}\\b[^>]*>|</${tag}\\s*>`, "gi");
    scan.lastIndex = start;

    let depth = 0;
    let end = -1;
    for (let m; (m = scan.exec(out)); ) {
      depth += m[0].startsWith("</") ? -1 : 1;
      if (depth === 0) {
        end = m.index + m[0].length;
        break;
      }
    }
    if (end === -1) {
      throw new Error(`unbalanced <${tag}> for a data-i18n="en" element at offset ${start}`);
    }

    // Also swallow the whitespace the pair sat on, so the Chinese sibling does
    // not inherit a stray leading newline where the English one used to be.
    let from = start;
    while (from > 0 && /[ \t]/.test(out[from - 1])) from--;
    if (out[from - 1] === "\n" && /^\s*$/.test(out.slice(from, start))) from--;

    out = out.slice(0, from) + out.slice(end);
  }
}

/** Replaces the first match of `re`, failing loudly when the anchor is gone. */
function replaceOnce(html, re, replacement, label) {
  if (!re.test(html)) {
    throw new Error(`build-zh: could not find ${label} in site/index.html`);
  }
  return html.replace(re, replacement);
}

const src = await readFile(srcPath, "utf8");
let out = src;

out = replaceOnce(
  out,
  /<html lang="en" data-theme="mocha" data-lang="en">/,
  '<html lang="zh-Hans" data-theme="mocha" data-lang="zh">',
  "the <html> tag",
);

out = replaceOnce(out, /<title>[^<]*<\/title>/, `<title>${ZH_TITLE}</title>`, "<title>");

out = replaceOnce(
  out,
  /<meta name="description" content="[^"]*">/,
  `<meta name="description" content="${ZH_DESCRIPTION}">`,
  "the description meta tag",
);

// The hreflang set is identical on both pages and already lives in the
// source, so only the canonical has to move.
out = replaceOnce(
  out,
  /<link rel="canonical" href="[^"]*">/,
  `<link rel="canonical" href="${ORIGIN}/zh/">`,
  "the canonical link",
);

out = replaceOnce(
  out,
  /<meta property="og:url" content="[^"]*">/,
  `<meta property="og:url" content="${ORIGIN}/zh/">`,
  "og:url",
);
out = replaceOnce(
  out,
  /<meta property="og:title" content="[^"]*">/,
  `<meta property="og:title" content="${ZH_TITLE}">`,
  "og:title",
);
out = replaceOnce(
  out,
  /<meta property="og:description" content="[^"]*">/,
  `<meta property="og:description" content="${ZH_OG_DESCRIPTION}">`,
  "og:description",
);
out = replaceOnce(
  out,
  /<meta property="og:locale" content="[^"]*">/,
  '<meta property="og:locale" content="zh_CN">',
  "og:locale",
);
out = replaceOnce(
  out,
  /<meta property="og:locale:alternate" content="[^"]*">/,
  '<meta property="og:locale:alternate" content="en_US">',
  "og:locale:alternate",
);
out = replaceOnce(
  out,
  /<meta property="og:image:alt" content="[^"]*">/,
  `<meta property="og:image:alt" content="${ZH_OG_IMAGE_ALT}">`,
  "og:image:alt",
);

// The switcher's EN button has nothing to switch to once the English elements
// are gone, so it becomes a link to the English URL.
out = replaceOnce(
  out,
  /<button type="button" data-lang-set="en"[^>]*>EN<\/button>\s*<button type="button" data-lang-set="zh"[^>]*>中<\/button>/,
  '<a href="/" hreflang="en" aria-label="English">EN</a>' +
    '<button type="button" aria-current="true" aria-label="中文">中</button>',
  "the language switcher",
);

out = stripEnglishElements(out);

// setLang() reads localStorage and navigator.language on load. On this page
// that is a blank-screen bug: a visitor whose stored preference is "en" would
// switch to a language whose elements no longer exist. Pin it to Chinese.
out = replaceOnce(
  out,
  /setLang\(savedLang \|\| \(\(navigator\.language \|\| "en"\)\.toLowerCase\(\)\.startsWith\("zh"\) \? "zh" : "en"\)\);/,
  'setLang("zh");',
  "the setLang() call",
);

if (/data-i18n="en"/.test(out)) {
  throw new Error("build-zh: English elements survived the strip");
}

await mkdir(dirname(outPath), { recursive: true });
await writeFile(outPath, out, "utf8");

console.log(
  `build-zh: wrote site/zh/index.html (${out.length} bytes, from ${src.length})`,
);
