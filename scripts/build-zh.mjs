// Generates the Chinese pages under site/zh/ from the bilingual English ones:
// site/index.html -> site/zh/index.html and
// site/send-to-kindle/index.html -> site/zh/send-to-kindle/index.html.
//
// Each source page is bilingual: every translated string ships as an adjacent
// pair of elements, one `data-i18n="en"` and one `data-i18n="zh"`, and CSS
// reveals whichever matches the `data-lang` on <html>. That is fine for a
// single URL, but it gives search engines one page whose Chinese half is
// display:none, so Chinese queries have nothing to rank. This script emits a
// second URL per page that is Chinese all the way down: the English elements
// are removed outright rather than hidden, links between the pages stay on
// the Chinese side, and each pair points at the other with hreflang.
//
// Run from the repo root: node scripts/build-zh.mjs

import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const ORIGIN = "https://zlib.heartleo.dev";

// Shared by both pages: the og:image is the same picture.
const ZH_OG_IMAGE_ALT = "zlib，终端里的 Z-Library 客户端，图中是它的搜索界面。";

// Chinese head copy. These mirror the zh strings already on each page rather
// than introducing new wording.
const PAGES = [
  {
    src: "site/index.html",
    out: "site/zh/index.html",
    path: "/",
    title: "zlib：终端里的 Z-Library 客户端，也能交给 AI Agent",
    description:
      "搜书、下载、发送到 Kindle 或微信读书。开源的 Z-Library 命令行客户端，自带 MCP 服务器，并提供 Claude Code 和 Codex 插件。",
    ogTitle: "zlib：终端里的 Z-Library 客户端，也能交给 AI Agent",
    ogDescription: "搜书、下载、发送到 Kindle 或微信读书，全程不用离开终端，也可以交给 Claude Code 或 Codex。",
  },
  {
    src: "site/send-to-kindle/index.html",
    out: "site/zh/send-to-kindle/index.html",
    path: "/send-to-kindle/",
    title: "在终端里把书发到 Kindle：zlib",
    description: "一条命令，通过你自己的 SMTP 服务器把 EPUB、PDF、DOCX 文件发到 Kindle。",
    ogTitle: "在终端里把书发到 Kindle",
    ogDescription: "一条命令，通过你自己的 SMTP 服务器把 EPUB、PDF、DOCX 文件发到 Kindle。",
    // The TechArticle's own headline and description, in its JSON-LD.
    ldHeadline: "在终端里把书发到 Kindle",
    ldDescription: "用 zlib 命令行客户端，通过你自己的 SMTP 服务器把 EPUB、PDF、DOCX 文件发到 Kindle 地址。",
  },
];

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
function replaceOnce(html, re, replacement, label, src) {
  if (!re.test(html)) {
    throw new Error(`build-zh: could not find ${label} in ${src}`);
  }
  return html.replace(re, replacement);
}

/**
 * Points links between the site's pages at their Chinese URLs: the landing
 * page and its anchors, and the Kindle guide. Assets such as /style.css and
 * /favicon.svg are not links and keep their paths.
 */
function localizeLinks(html) {
  return html.replace(
    /(<a\b[^>]*\bhref=")\/((?:#[^"]*)?|send-to-kindle\/[^"]*)"/g,
    (_, head, rest) => `${head}/zh/${rest}"`,
  );
}

async function buildPage(page) {
  const src = await readFile(resolve(repoRoot, page.src), "utf8");
  const zhURL = `${ORIGIN}/zh${page.path}`;
  const r = (out, re, replacement, label) => replaceOnce(out, re, replacement, label, page.src);
  let out = src;

  out = r(
    out,
    /<html lang="en" data-theme="mocha" data-lang="en">/,
    '<html lang="zh-Hans" data-theme="mocha" data-lang="zh">',
    "the <html> tag",
  );
  out = r(out, /<title>[^<]*<\/title>/, `<title>${page.title}</title>`, "<title>");
  out = r(
    out,
    /<meta name="description" content="[^"]*">/,
    `<meta name="description" content="${page.description}">`,
    "the description meta tag",
  );

  // The hreflang set is identical on both pages of a pair and already lives
  // in the source, so only the canonical has to move.
  out = r(out, /<link rel="canonical" href="[^"]*">/, `<link rel="canonical" href="${zhURL}">`, "the canonical link");
  out = r(out, /<meta property="og:url" content="[^"]*">/, `<meta property="og:url" content="${zhURL}">`, "og:url");
  out = r(out, /<meta property="og:title" content="[^"]*">/, `<meta property="og:title" content="${page.ogTitle}">`, "og:title");
  out = r(
    out,
    /<meta property="og:description" content="[^"]*">/,
    `<meta property="og:description" content="${page.ogDescription}">`,
    "og:description",
  );
  out = r(out, /<meta property="og:locale" content="[^"]*">/, '<meta property="og:locale" content="zh_CN">', "og:locale");
  out = r(
    out,
    /<meta property="og:locale:alternate" content="[^"]*">/,
    '<meta property="og:locale:alternate" content="en_US">',
    "og:locale:alternate",
  );
  out = r(
    out,
    /<meta property="og:image:alt" content="[^"]*">/,
    `<meta property="og:image:alt" content="${ZH_OG_IMAGE_ALT}">`,
    "og:image:alt",
  );

  if (page.ldHeadline) {
    out = r(out, /"headline": "[^"]*"/, `"headline": "${page.ldHeadline}"`, "the JSON-LD headline");
    out = r(out, /("@type": "TechArticle",[\s\S]*?"description": )"[^"]*"/, `$1"${page.ldDescription}"`, "the JSON-LD description");
    out = r(out, /"url": "[^"]*\/send-to-kindle\/"/, `"url": "${zhURL}"`, "the JSON-LD url");
    out = r(out, /"inLanguage": "en"/, '"inLanguage": "zh-Hans"', "the JSON-LD inLanguage");
  }

  out = localizeLinks(out);

  // A relative stylesheet (the guide's ../style.css) would resolve one level
  // too shallow under /zh/, so every page points at the root copy.
  out = out.replace(/<link rel="stylesheet" href="(?:\.\.\/)+style\.css">/, '<link rel="stylesheet" href="/style.css">');

  // The switcher's EN button has nothing to switch to once the English
  // elements are gone, so it becomes a link to this page's English URL.
  out = r(
    out,
    /<button type="button" data-lang-set="en"[^>]*>EN<\/button>\s*<button type="button" data-lang-set="zh"[^>]*>中<\/button>/,
    `<a href="${page.path}" hreflang="en" aria-label="English">EN</a>` +
      '<button type="button" aria-current="true" aria-label="中文">中</button>',
    "the language switcher",
  );

  out = stripEnglishElements(out);

  // setLang() reads localStorage and navigator.language on load. On this page
  // that is a blank-screen bug: a visitor whose stored preference is "en"
  // would switch to a language whose elements no longer exist. Pin it.
  out = r(
    out,
    /setLang\(savedLang \|\| \(\(navigator\.language \|\| "en"\)\.toLowerCase\(\)\.startsWith\("zh"\) \? "zh" : "en"\)\);/,
    'setLang("zh");',
    "the setLang() call",
  );

  if (/data-i18n="en"/.test(out)) {
    throw new Error(`build-zh: English elements survived the strip in ${page.src}`);
  }

  const outPath = resolve(repoRoot, page.out);
  await mkdir(dirname(outPath), { recursive: true });
  await writeFile(outPath, out, "utf8");
  console.log(`build-zh: wrote ${page.out} (${out.length} bytes, from ${src.length})`);
}

for (const page of PAGES) {
  await buildPage(page);
}
