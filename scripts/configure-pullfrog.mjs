import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';

// Prints, checks or applies repository settings only; never installs or pushes the workflow.
//
// Pullfrog keeps settings in its console and offers no bulk apply or export, so this
// script is the single source of them: one `pullfrog config` call per key. It holds no
// credential. The pullfrog CLI acts as the GitHub user signed in to the GitHub CLI.
const root = fileURLToPath(new URL('../', import.meta.url));
const repo = 'heartleo/zlib';
// Pinned: the CLI runs with your GitHub token. The API rejects a CLI whose contract
// version it no longer supports; raise this after reading the release notes.
const cliVersion = '0.1.125';
// Files that decide what the agent is told; --apply refuses to write them unreviewed.
const reviewedPaths = ['.github/pullfrog', 'scripts/configure-pullfrog.mjs'];

export const settings = [
  ['push', 'disabled'],
  ['shell', 'restricted'],
  ['auto-merge', 'false'],
  ['review.approve', 'false'],
  ['address-reviews.enabled', 'false'],
  ['fix-ci.own-prs', 'false'],
  ['fix-ci.reviewed-prs', 'false'],
  ['oss', 'true'],
  ['signed-commits', 'false'],
  ['model', 'deepseek/deepseek-flash'],
  // Flash exposes low/high/max; 0.5 selects high on its three-level ladder.
  ['effort', '0.5'],
  ['instructions', '--file', '.github/pullfrog/instructions.md'],
  ['issue.mode', 'custom'],
  ['issue.instructions', '--file', '.github/pullfrog/issue.md'],
  // The issue forms already apply bug, enhancement and question labels.
  ['label.enabled', 'false'],
  ['review.mode', 'agent'],
  ['prompts.review', '--file', '.github/pullfrog/review.md'],
  ['review.re-review', 'true'],
  ['review.drafts', 'false'],
  ['review.own-prs', 'false'],
  ['review.status-check', 'true'],
  ['review.approval-check', 'false'],
  ['mention.enabled', 'true'],
  ['mention.non-collaborators', 'false'],
  ['progress-comments', 'false'],
  // Enable external issue triage and reviews, not arbitrary external mentions.
  ['issue.non-collaborators', 'true'],
  ['review.non-collaborators', 'true'],
];

// Keys this repository keeps unset, so a console edit cannot linger across
// re-applies. env-allowlist would expose listed values to agent shells.
export const unset = [
  'env-allowlist',
  'hooks.setup',
  'hooks.post-checkout',
  'hooks.pre-push',
  'mention.instructions',
  'label.instructions',
  'prompts.build',
  'prompts.plan',
  'prompts.address-reviews',
  'prompts.fix-ci',
];

// `config get` prints the bare value, or `unset`. Compare after stripping
// whitespace and quotes.
export const normalize = (text) => text.replace(/\s+/g, ' ').replace(/^["'\s]+|["'\s]+$/g, '');

const loadFile = (file) => readFileSync(new URL(`../${file}`, import.meta.url), 'utf8');

export function wanted(rest, load = loadFile) {
  return rest[0] === '--file' ? load(rest[1]) : rest[0];
}

// Runs attempt until it succeeds or `tries` is used up, waiting longer each time.
// The CLI makes one network request per call, and a single dropped request must
// not abandon a run of dozens.
export async function retry(attempt, { tries = 5, delayMs = 2000, sleep = (ms) => new Promise((done) => setTimeout(done, ms)), onRetry = () => {} } = {}) {
  for (let n = 1; ; n++) {
    try {
      return await attempt();
    } catch (error) {
      if (n >= tries) throw error;
      onRetry(n, error);
      await sleep(delayMs * n);
    }
  }
}

// Reads every key once and returns only what differs from this file.
export async function plan({ read, load = loadFile }) {
  const changes = [];
  for (const [key, ...rest] of settings) {
    const want = wanted(rest, load);
    const got = await read(key);
    if (normalize(got) !== normalize(want)) changes.push({ verb: 'set', key, rest, want, got });
  }
  for (const key of unset) {
    const got = await read(key);
    if (normalize(got) !== 'unset') changes.push({ verb: 'unset', key, rest: [], want: 'unset', got });
  }
  return changes;
}

// The settings as text, for entering them in the console by hand. Pure: no
// network, no credentials, so the output is safe to keep or share.
export function render(load = loadFile) {
  const lines = [
    `Pullfrog settings for ${repo}, from scripts/configure-pullfrog.mjs.`,
    `Enter them in the console, or: pullfrog config set <key> <value> --repo ${repo}`,
    '',
  ];
  for (const [key, ...rest] of settings) {
    const value = wanted(rest, load);
    if (rest[0] === '--file') {
      lines.push(`${key}  (text from ${rest[1]})`, '<<<', value.replace(/\n+$/, ''), '>>>', '');
    } else {
      lines.push(`${key} = ${value}`);
    }
  }
  if (lines.at(-1) !== '') lines.push('');
  lines.push('Keep unset:', ...unset.map((key) => `  ${key}`), '');
  return lines.join('\n');
}

// Settings written to the console steer an agent that reads public issues, so
// they must come from files that were reviewed and merged, not a working copy.
export function applyProblem(git) {
  const head = git(['rev-parse', 'HEAD']);
  const main = git(['rev-parse', 'origin/main']);
  if (!head || !main) return 'cannot read the git state; run it inside the repository checkout';
  if (head !== main) {
    return 'HEAD is not origin/main. Settings must come from merged files: run "git fetch", then "git switch main" and pull';
  }
  const dirty = git(['status', '--porcelain', '--', ...reviewedPaths]);
  if (dirty) return `uncommitted changes in ${reviewedPaths.join(' or ')}; commit or discard them first`;
  return null;
}

function usage() {
  console.log('Usage: node scripts/configure-pullfrog.mjs --print | --check | --apply');
  console.log('--print  shows the settings as text for the console; no network, no login.');
  console.log('--check  reads the console and reports differences; it changes nothing.');
  console.log(`--apply  writes only the differing settings for ${repo}, from merged main, using GitHub CLI login.`);
  console.log('None of them enables the repository, publishes files or starts agent runs.');
}

// The CLI's own message is the only clue to why a call failed. It is read from
// stderr, which never holds a token, and trimmed to its first line.
const failure = (key, result) => {
  const reason = result.error?.message ?? `exit status ${result.status}`;
  const said = result.stderr?.toString().split('\n').map((line) => line.trim()).find(Boolean);
  return new Error(`${key}: ${said ? `${reason}: ${said.slice(0, 120)}` : reason}`);
};

// Node's fetch ignores HTTP(S)_PROXY unless NODE_USE_ENV_PROXY is set, so behind
// a proxy the CLI connects directly and its requests time out in bursts. Opt in
// when a proxy is configured and the caller has not chosen. Older Node ignores
// the variable.
export function childEnv(env) {
  const proxied = ['HTTPS_PROXY', 'https_proxy', 'HTTP_PROXY', 'http_proxy'].some((name) => env[name]);
  return proxied && env.NODE_USE_ENV_PROXY === undefined ? { ...env, NODE_USE_ENV_PROXY: '1' } : env;
}

function pullfrog(args, options) {
  return spawnSync(
    'npx',
    ['--yes', `--package=pullfrog@${cliVersion}`, 'pullfrog', 'config', ...args, '--repo', repo, '--yes'],
    { cwd: root, shell: process.platform === 'win32', env: childEnv(process.env), ...options },
  );
}

const onRetry = (n, error) => console.error(`${error.message}; retrying (${n})`);

// On Windows the CLI's Node process can abort at exit (a libuv assertion) after
// printing a complete answer, which surfaces as a non-zero status. Trust the
// output when there is some; fail only on an empty read.
const read = (key) =>
  retry(() => {
    const result = pullfrog(['get', key], { encoding: 'utf8' });
    if (result.error || (result.status !== 0 && !result.stdout?.trim())) throw failure(key, result);
    return result.stdout;
  }, { onRetry });

async function write({ verb, key, rest, want }) {
  await retry(async () => {
    const result = pullfrog([verb, key, ...rest], { stdio: 'inherit' });
    if (result.error) throw failure(key, result);
    // A non-zero status may be the Windows exit crash after a successful
    // write, so confirm by reading the value back before giving up.
    if (result.status !== 0 && normalize(await read(key)) !== normalize(want)) throw failure(key, result);
  }, { onRetry });
}

const git = (args) => {
  const result = spawnSync('git', args, { cwd: root, encoding: 'utf8' });
  return result.status === 0 ? result.stdout.trim() : '';
};

function describe(change) {
  return `${change.key}: want ${JSON.stringify(normalize(change.want).slice(0, 60))}, got ${JSON.stringify(normalize(change.got).slice(0, 60))}`;
}

async function main(mode) {
  if (mode === '--print') {
    process.stdout.write(render());
    return 0;
  }
  if (mode === '--apply') {
    const problem = applyProblem(git);
    if (problem) {
      console.error(`Refusing to apply: ${problem}.`);
      return 1;
    }
  }

  let changes;
  try {
    changes = await plan({ read });
  } catch (error) {
    console.error(`Could not read the current settings (${error.message}); nothing was written. Try again.`);
    return 1;
  }

  if (mode === '--check') {
    for (const change of changes) console.log(`DIFF ${describe(change)}`);
    console.log(changes.length === 0 ? 'No drift.' : `${changes.length} setting(s) differ; run --apply from main to write them.`);
    return changes.length === 0 ? 0 : 1;
  }

  if (changes.length === 0) {
    console.log('No drift; nothing to write.');
    return 0;
  }
  let done = 0;
  for (const change of changes) {
    try {
      await write(change);
      done++;
    } catch (error) {
      console.error(`Stopped at ${error.message}. ${done} of ${changes.length} written; run --apply again to write the rest.`);
      return 1;
    }
  }
  console.log(`${done} of ${changes.length} setting(s) written. Run --check to confirm; publish the workflow separately.`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const mode = process.argv[2];
  if (process.argv.length !== 3 || !['--print', '--check', '--apply'].includes(mode)) {
    usage();
    process.exit(process.argv.length === 2 ? 0 : 1);
  }
  process.exit(await main(mode));
}
