import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { goJobsNeeded, noGoImpact, safeFiles, safePrefixes } from './ci-changes.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const tracked = execFileSync('git', ['ls-files'], { cwd: root, encoding: 'utf8' }).split('\n').filter(Boolean);

test('docs, site, skills and plugin files skip the Go jobs', () => {
  for (const file of [
    'README.md',
    'docs/usage.md',
    'docs/search.gif',
    'site/index.html',
    'site/covers/115066162.jpg',
    '.agents/skills/zlib/SKILL.md',
    'plugins/zlib/.claude-plugin/plugin.json',
    '.github/CONTRIBUTING.md',
    '.github/ISSUE_TEMPLATE/bug_report.yml',
    '.github/workflows/pages.yml',
    'scripts/check-docs.mjs',
  ]) assert.equal(noGoImpact(file), true, file);
});

test('Go code, modules and what the Go tests read run the Go jobs', () => {
  for (const file of [
    'cmd/zlib/main.go',
    'internal/cli/download.go',
    'internal/cli/testdata/demo-covers/115066162.jpg',
    'go.mod',
    'go.sum',
    // internal/zlib/version_test.go reads it.
    '.goreleaser.yml',
    // Changing CI or this filter must exercise the whole pipeline.
    '.github/workflows/ci.yml',
    'scripts/ci-changes.mjs',
    'scripts/ci-changes.test.mjs',
  ]) assert.equal(noGoImpact(file), false, file);
});

test('anything not known to be safe runs the Go jobs', () => {
  for (const file of ['new-dir/file.txt', '.github/workflows/new.yml', 'scripts/new.sh', 'docs', 'site']) {
    assert.equal(noGoImpact(file), false, file);
  }
});

test('one Go-relevant file among docs is enough to run the Go jobs', () => {
  assert.deepEqual(goJobsNeeded(['docs/usage.md', 'internal/cli/root.go', 'site/index.html']), {
    needed: true,
    reason: 'internal/cli/root.go',
  });
  assert.deepEqual(goJobsNeeded(['docs/usage.md', 'site/index.html']), { needed: false, reason: null });
});

test('an empty or unreadable change list runs the Go jobs', () => {
  assert.equal(goJobsNeeded([]).needed, true);
  assert.equal(goJobsNeeded(['', '  ']).needed, true);
});

test('paths are trimmed, so CRLF output from git still matches', () => {
  assert.equal(goJobsNeeded(['docs/usage.md\r', ' site/index.html ']).needed, false);
});

test('recent changes are classified as they should have been', () => {
  // 53b79a8 and 3b08045 changed no Go and ran the full pipeline; f33cba0 and
  // 4fb13bf touched internal/cli and must keep doing so.
  assert.equal(goJobsNeeded(['.github/workflows/pages.yml', 'site/README.md']).needed, false);
  assert.equal(goJobsNeeded(['docs/versioning.md', 'plugins/zlib/.claude-plugin/plugin.json', 'plugins/zlib/.codex-plugin/plugin.json']).needed, false);
  assert.equal(goJobsNeeded(['internal/cli/demo_test.go', 'site/README.md', 'site/covers/README.md']).needed, true);
});

test('every listed safe file exists, so a typo cannot hide a rule', () => {
  for (const file of safeFiles) assert.ok(tracked.includes(file), `${file} is not tracked`);
  for (const prefix of safePrefixes) assert.ok(tracked.some((file) => file.startsWith(prefix)), `${prefix} matches nothing`);
});

test('no file the Go build or tests use is treated as safe', () => {
  const goSide = tracked.filter((file) => /^(cmd|internal)\/|\.go$|^go\.(mod|sum)$/.test(file) || file === '.goreleaser.yml');
  assert.ok(goSide.length > 0);
  assert.deepEqual(goSide.filter(noGoImpact), []);
});
