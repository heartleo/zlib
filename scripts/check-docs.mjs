// Privacy and language check for the repository's documentation: every
// Markdown file, skill, tape and example, plus local notes that get shared
// (docs/handoff.md, CLAUDE.md). None may leak the author's machine: absolute
// local paths, the current OS username, personal email addresses, private IP
// addresses or credential values. Documents under docs/ must also be English.
// gitleaks (.gitleaks.toml) covers secrets in every file type; this check
// covers what secret scanners do not.
// Rules: AGENTS.md#documentation-privacy. Runs from scripts/check.sh and CI.
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { userInfo } from 'node:os';
import { extname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const textExtensions = new Set(['.md', '.tape', '.example', '.txt']);
// Gitignored, but shared with agents and between machines.
const localNotes = ['docs/handoff.md', 'CLAUDE.md'];
// Generated from site/ and checked there.
const skipped = /^site\/zh\//;

// Only docs/ must be English: repo-readme keeps Chinese README templates.
// site.md quotes Chinese strings from the zh site as line-breaking test data.
const englishOnly = 'docs/';
const cjkAllowed = new Set(['docs/site.md']);
const cjk = /[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uac00-\ud7af\uff00-\uffef]/u;

// Placeholder home directories used by examples and the demo tapes.
const placeholderUsers = new Set(['you', 'me', 'books', 'user', 'name', 'runner']);
const emailDomainsAllowed = /^(example\.(com|org|net)|users\.noreply\.github\.com|github\.com)$/i;
// Generic example addresses on real domains.
const emailsAllowed = new Set(['reader@kindle.com']);

const rules = [
  {
    name: 'absolute Windows path',
    // C:\... or C:/..., unless it is the generic C:\Users\<name> form.
    find: line => [...line.matchAll(/(?<![\w%])[A-Za-z]:[\\/](?!Users[\\/]<)\S*/g)].map(m => m[0]),
  },
  {
    name: 'absolute Unix home path',
    // Must start a token, so URL paths such as https://host/home/... pass.
    find: line => [...line.matchAll(/(?<![\w./-])\/(?:home|Users)\/([^/\s`'")<>]+)/g)]
      .filter(m => !placeholderUsers.has(m[1].toLowerCase()))
      .map(m => m[0]),
  },
  {
    name: 'Git Bash or WSL drive path',
    find: line => [...line.matchAll(/(?:^|[\s`'"(])(\/(?:mnt\/)?[a-z]\/[A-Za-z][^\s`'")]*)/g)].map(m => m[1]),
  },
  {
    name: 'email address',
    find: line => [...line.matchAll(/[\w.+-]+@((?:[\w-]+\.)+[A-Za-z]{2,})\b/g)]
      .filter(m => !emailDomainsAllowed.test(m[1]) && !emailsAllowed.has(m[0].toLowerCase()))
      .map(m => m[0]),
  },
  {
    // Use 127.0.0.1 or the RFC 5737 documentation ranges in examples.
    name: 'private IP address',
    find: line => [...line.matchAll(/\b(?:10|192\.168|172\.(?:1[6-9]|2\d|3[01]))(?:\.\d{1,3}){2,3}\b/g)]
      .filter(m => m[0].split('.').length === 4)
      .map(m => m[0]),
  },
  {
    name: 'credential value',
    find: line => [...line.matchAll(/remix[_-]user(?:id|key)["']?\s*[:=]\s*["']?(?![<$])[\w-]{6,}/gi)].map(m => m[0]),
  },
];

const username = (() => {
  try {
    return userInfo().username;
  } catch {
    return '';
  }
})();
if (username.length >= 3 && !placeholderUsers.has(username.toLowerCase())) {
  const pattern = new RegExp(`\\b${username.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\b`, 'i');
  rules.push({ name: 'local username', find: line => (pattern.test(line) ? [username] : []) });
}

// Tracked files plus untracked ones that are not ignored, i.e. everything a
// commit could pick up.
const listed = execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z'], {
  cwd: root,
  encoding: 'utf8',
}).split('\0');
const files = [...new Set([...listed, ...localNotes])]
  .filter(name => name && textExtensions.has(extname(name)) && !skipped.test(name))
  .filter(name => existsSync(join(root, name)))
  .sort();

const problems = [];
for (const name of files) {
  readFileSync(join(root, name), 'utf8').split('\n').forEach((line, index) => {
    const where = `${name}:${index + 1}`;
    for (const rule of rules) {
      for (const match of rule.find(line)) problems.push(`${where}: ${rule.name}: ${match}`);
    }
    if (name.startsWith(englishOnly) && !cjkAllowed.has(name) && cjk.test(line)) {
      problems.push(`${where}: non-English text`);
    }
  });
}

if (problems.length) {
  console.error(problems.join('\n'));
  console.error(`\n${problems.length} problem(s). Docs must be English and use repository-relative paths or placeholders; see AGENTS.md#documentation-privacy.`);
  process.exit(1);
}
console.log(`docs: ${files.length} files clean (English docs/, no local paths, usernames or credentials).`);
