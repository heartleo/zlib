import { realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Decides whether a change needs the Go jobs in .github/workflows/ci.yml
// (build, the test matrix, govulncheck). It reads changed paths, one per line,
// on stdin and prints `go=true` or `go=false` for $GITHUB_OUTPUT.
//
// The rule fails safe: the Go jobs are skipped only when every changed file is
// listed here as having no effect on the Go build or tests. A new directory, a
// new workflow or an unknown file runs them. Keep these lists to paths that no
// Go code or test reads; internal/zlib/version_test.go reads .goreleaser.yml,
// so it is not here.

// Directories whose files no Go code or test reads.
export const safePrefixes = [
  'docs/',
  'site/',
  '.agents/',
  'plugins/',
  '.claude-plugin/',
  '.githooks/',
  '.github/ISSUE_TEMPLATE/',
  '.github/pullfrog/',
];

// Single files with no effect on the Go build or tests. The node scripts are
// covered by the `checks` job, which runs on every change.
export const safeFiles = [
  'README.md',
  'README.zh.md',
  'AGENTS.md',
  'LICENSE',
  'Makefile',
  'install.sh',
  '.gitignore',
  '.gitleaks.toml',
  '.gitleaksignore',
  '.github/CONTRIBUTING.md',
  '.github/SECURITY.md',
  '.github/pull_request_template.md',
  '.github/dependabot.yml',
  '.github/workflows/gitleaks.yml',
  '.github/workflows/govulncheck.yml',
  '.github/workflows/pages.yml',
  '.github/workflows/pullfrog.yml',
  '.github/workflows/release.yml',
  'scripts/build-zh.mjs',
  'scripts/check.sh',
  'scripts/check-docs.mjs',
  'scripts/check-site-text.mjs',
  'scripts/configure-pullfrog.mjs',
  'scripts/configure-pullfrog.test.mjs',
  'scripts/sync-plugin-skills.mjs',
];

export function noGoImpact(file) {
  return safeFiles.includes(file) || safePrefixes.some((prefix) => file.startsWith(prefix));
}

// needed is true, with the first file that requires them as the reason, unless
// every file is safe. An empty list means the diff could not be read, so the
// jobs run.
export function goJobsNeeded(files) {
  const changed = files.map((file) => file.trim()).filter(Boolean);
  if (changed.length === 0) return { needed: true, reason: 'no changed files were listed' };
  const first = changed.find((file) => !noGoImpact(file));
  return first ? { needed: true, reason: first } : { needed: false, reason: null };
}

function isEntryPoint() {
  try {
    return Boolean(process.argv[1]) && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}

if (isEntryPoint()) {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const files = input.split('\n');
  const { needed, reason } = goJobsNeeded(files);
  const count = files.filter((file) => file.trim()).length;
  console.error(needed ? `Go jobs run: ${reason}` : `Go jobs skipped: all ${count} changed file(s) are docs, site or tooling`);
  console.log(`go=${needed}`);
}
