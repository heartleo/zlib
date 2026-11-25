import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const read = (name) => readFileSync(fileURLToPath(new URL(`../.github/workflows/${name}`, import.meta.url)), 'utf8');
const ci = read('ci.yml');
const release = read('release.yml');

const goreleaser = (text) => {
  const m = text.match(/uses: (goreleaser\/goreleaser-action@\S+)[^\n]*\n\s+with:\n((?:\s+[^\n]+\n)+)/);
  assert.ok(m, 'no goreleaser-action step');
  return { action: m[1], version: m[2].match(/version: "([^"]+)"/)?.[1], args: m[2].match(/args: (.+)/)?.[1].trim() };
};

// A GoReleaser config that stops validating shows up as a failed release on the
// tag, after the commit is already on main. Pull requests run `goreleaser check`
// so it shows up there instead.
test('every pull request validates the GoReleaser config', () => {
  assert.equal(goreleaser(ci).args, 'check');
});

// The check is only worth anything when it runs the GoReleaser that publishes.
test('the config check and the release use the same GoReleaser action and version', () => {
  const check = goreleaser(ci);
  const publish = goreleaser(release);
  assert.equal(check.action, publish.action, 'ci.yml and release.yml pin different goreleaser-action versions');
  assert.equal(check.version, publish.version, 'ci.yml checks with a different GoReleaser version than release.yml publishes with');
});

// Stopping GoReleaser halfway leaves a partial draft and package manifests that
// were never published.
test('a running release is never cancelled by another tag', () => {
  const block = release.match(/^concurrency:\n((?:[ ]+[^\n]+\n)+)/m);
  assert.ok(block, 'release.yml has no concurrency group');
  assert.match(block[1], /cancel-in-progress: false/);
});

// The release job first waits for ci-ok, then builds and publishes. A timeout that
// only just covers the wait would cut the release off during the build.
test('the release timeout leaves room for the build after the ci-ok wait', () => {
  const attempts = Number(release.match(/waiting for ci-ok \(\$attempt\/(\d+)\)/)?.[1]);
  const interval = Number(release.match(/sleep (\d+)/)?.[1]);
  const timeout = Number(release.match(/timeout-minutes: (\d+)/)?.[1]);
  assert.ok(attempts > 0 && interval > 0 && timeout > 0, 'could not read the gate or the timeout from release.yml');
  const gateMinutes = (attempts * interval) / 60;
  const buildBudget = 20; // build, tests and publishing together
  assert.ok(timeout >= gateMinutes + buildBudget, `timeout ${timeout} min leaves less than ${buildBudget} min after a ${gateMinutes} min gate`);
});
