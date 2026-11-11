import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { applyProblem, childEnv, normalize, plan, render, retry, settings, unset, wanted } from './configure-pullfrog.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const load = (file) => `text of ${file}\n`;
const noSleep = () => Promise.resolve();

// The console as a map; a key that is not in it reads as unset.
const consoleOf = (values) => async (key) => values[key] ?? 'unset';
const matching = () => {
  const values = {};
  for (const [key, ...rest] of settings) values[key] = wanted(rest, load);
  return values;
};

test('keys are unique, and a key is never both set and unset', () => {
  const keys = settings.map(([key]) => key);
  assert.equal(new Set(keys).size, keys.length);
  assert.deepEqual(unset.filter((key) => keys.includes(key)), []);
});

test('every settings file exists', () => {
  for (const [, ...rest] of settings) {
    if (rest[0] === '--file') assert.ok(existsSync(`${root}${rest[1]}`), rest[1]);
  }
});

test('retry returns after transient failures and waits longer each time', async () => {
  let calls = 0;
  const waits = [];
  const value = await retry(
    () => {
      if (++calls < 3) throw new Error('fetch failed');
      return 'ok';
    },
    { delayMs: 10, sleep: async (ms) => waits.push(ms) },
  );
  assert.equal(value, 'ok');
  assert.equal(calls, 3);
  assert.deepEqual(waits, [10, 20]);
});

test('retry gives up with the last error after the tries are used', async () => {
  let calls = 0;
  await assert.rejects(
    retry(() => { calls++; throw new Error(`failed ${calls}`); }, { tries: 3, sleep: noSleep }),
    /failed 3/,
  );
  assert.equal(calls, 3);
});

test('plan reports nothing when the console matches', async () => {
  assert.deepEqual(await plan({ read: consoleOf(matching()), load }), []);
});

test('plan ignores whitespace and quotes when comparing', async () => {
  const values = matching();
  values.push = '"disabled"\n';
  values.instructions = `  ${values.instructions.trimEnd()}  \n\n`;
  assert.deepEqual(await plan({ read: consoleOf(values), load }), []);
});

test('plan lists only what differs, and reads each key once', async () => {
  const values = { ...matching(), push: 'enabled', 'prompts.review': 'old text', 'prompts.plan': 'left over' };
  delete values['issue.mode'];
  const reads = [];
  const changes = await plan({ read: async (key) => { reads.push(key); return consoleOf(values)(key); }, load });
  assert.deepEqual(changes.map(({ verb, key }) => `${verb} ${key}`).sort(), [
    'set issue.mode',
    'set prompts.review',
    'set push',
    'unset prompts.plan',
  ]);
  assert.equal(new Set(reads).size, reads.length);
  assert.equal(reads.length, settings.length + unset.length);
  const review = changes.find(({ key }) => key === 'prompts.review');
  assert.equal(review.want, 'text of .github/pullfrog/review.md\n');
  assert.deepEqual(review.rest, ['--file', '.github/pullfrog/review.md']);
});

test('plan stops when a read keeps failing, before anything is written', async () => {
  await assert.rejects(plan({ read: async () => { throw new Error('fetch failed'); }, load }), /fetch failed/);
});

test('render shows every setting, file text and the unset keys', () => {
  const text = render(load);
  for (const [key, ...rest] of settings) {
    assert.ok(text.includes(rest[0] === '--file' ? `${key}  (text from ${rest[1]})` : `${key} = ${rest[0]}`), key);
  }
  assert.ok(text.includes('text of .github/pullfrog/review.md'));
  assert.ok(text.includes('Keep unset:'));
  for (const key of unset) assert.ok(text.includes(`  ${key}`), key);
  assert.equal(render(load), text, 'output is stable');
});

test('render carries nothing that looks like a credential', () => {
  assert.doesNotMatch(render(), /ghp_|github_pat_|gho_|Bearer |remix_userkey/);
});

const fakeGit = (answers) => (args) => answers[args.join(' ')] ?? '';

test('apply is refused away from origin/main', () => {
  const problem = applyProblem(fakeGit({ 'rev-parse HEAD': 'aaa', 'rev-parse origin/main': 'bbb' }));
  assert.match(problem, /HEAD is not origin\/main/);
});

test('apply is refused with uncommitted prompt changes', () => {
  const problem = applyProblem(fakeGit({
    'rev-parse HEAD': 'aaa',
    'rev-parse origin/main': 'aaa',
    'status --porcelain -- .github/pullfrog scripts/configure-pullfrog.mjs': ' M .github/pullfrog/review.md',
  }));
  assert.match(problem, /uncommitted changes/);
});

test('apply is refused when the git state cannot be read', () => {
  assert.match(applyProblem(fakeGit({})), /cannot read the git state/);
});

test('apply is allowed on a clean origin/main', () => {
  assert.equal(applyProblem(fakeGit({ 'rev-parse HEAD': 'aaa', 'rev-parse origin/main': 'aaa' })), null);
});

test('a configured proxy is passed on to the CLI unless the caller chose', () => {
  assert.equal(childEnv({ HTTPS_PROXY: 'http://proxy.example:8080' }).NODE_USE_ENV_PROXY, '1');
  assert.equal(childEnv({ http_proxy: 'http://proxy.example:8080' }).NODE_USE_ENV_PROXY, '1');
  assert.equal(childEnv({ HTTPS_PROXY: 'http://proxy.example:8080', NODE_USE_ENV_PROXY: '0' }).NODE_USE_ENV_PROXY, '0');
});

test('without a proxy the environment is left alone', () => {
  const env = { PATH: '/bin' };
  assert.equal(childEnv(env), env);
  assert.equal('NODE_USE_ENV_PROXY' in childEnv({ ALL_PROXY: 'socks5://proxy.example:1080' }), false);
});

test('normalize drops whitespace runs and surrounding quotes', () => {
  assert.equal(normalize('  "a \n b"  '), 'a b');
});
