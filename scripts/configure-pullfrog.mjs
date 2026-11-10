import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Applies or checks repository settings only; never installs or pushes the workflow.
const root = fileURLToPath(new URL('../', import.meta.url));
const repo = 'heartleo/zlib';
const settings = [
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
const unset = [
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

const mode = process.argv[2];
if (process.argv.length !== 3 || (mode !== '--apply' && mode !== '--check')) {
  console.log('Usage: node scripts/configure-pullfrog.mjs --apply | --check');
  console.log('--apply writes Pullfrog settings for heartleo/zlib using GitHub CLI authentication.');
  console.log('--check reads them back and reports differences; it changes nothing.');
  console.log('Neither enables the repository, publishes files or starts agent runs.');
  process.exit(process.argv.length === 2 ? 0 : 1);
}

function pullfrog(args, options) {
  return spawnSync(
    'npx',
    ['--yes', '--package=pullfrog@0.1.125', 'pullfrog', 'config', ...args, '--repo', repo, '--yes'],
    { cwd: root, shell: process.platform === 'win32', ...options },
  );
}

function fail(key, result) {
  console.error(`Stopped at ${key}; check current settings before retrying.`);
  if (result.error) console.error(result.error.message);
  process.exit(result.status || 1);
}

// `config get` prints the bare value, or `unset`. Compare after stripping
// whitespace and quotes.
const normalize = (text) => text.replace(/\s+/g, ' ').replace(/^["'\s]+|["'\s]+$/g, '');

// On Windows the CLI's Node process can abort at exit (a libuv assertion) after
// printing a complete answer, which surfaces as a non-zero status. Trust the
// output when there is some; fail only on an empty read.
function read(key) {
  const result = pullfrog(['get', key], { encoding: 'utf8' });
  if (result.error || (result.status !== 0 && !result.stdout?.trim())) fail(key, result);
  return result.stdout;
}

function wanted(rest) {
  return rest[0] === '--file' ? readFileSync(new URL(`../${rest[1]}`, import.meta.url), 'utf8') : rest[0];
}

function applied(verb, key, rest) {
  const got = normalize(read(key));
  return verb === 'unset' ? got === 'unset' : got === normalize(wanted(rest));
}

if (mode === '--apply') {
  const commands = [
    ...settings.map((setting) => ['set', ...setting]),
    ...unset.map((key) => ['unset', key]),
  ];
  for (const [verb, key, ...rest] of commands) {
    const result = pullfrog([verb, key, ...rest], { stdio: 'inherit' });
    if (result.error) fail(key, result);
    // A non-zero status may be the Windows exit crash after a successful
    // write, so confirm by reading the value back before giving up.
    if (result.status !== 0 && !applied(verb, key, rest)) fail(key, result);
  }
  console.log('Settings saved. Publish the workflow separately, then verify manual installation in the console.');
} else {
  let drift = 0;
  const report = (key, want, got) => {
    drift++;
    console.log(`DIFF ${key}: want ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
  };
  for (const [key, ...rest] of settings) {
    const want = wanted(rest);
    const got = read(key);
    if (normalize(got) !== normalize(want)) report(key, normalize(want).slice(0, 60), normalize(got).slice(0, 60));
  }
  for (const key of unset) {
    const got = normalize(read(key));
    if (got !== 'unset') report(key, 'unset', got.slice(0, 60));
  }
  console.log(drift === 0 ? 'No drift.' : `${drift} setting(s) differ; run --apply to restore them.`);
  process.exit(drift === 0 ? 0 : 1);
}
