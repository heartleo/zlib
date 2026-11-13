import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const script = fileURLToPath(new URL('../install.sh', import.meta.url));
const canRun = process.platform !== 'win32';

function realTool(name) {
  return execFileSync('sh', ['-c', `command -v ${name}`], { encoding: 'utf8' }).trim();
}

// sandbox builds a PATH of fake tools so install.sh never touches the network,
// a real sudo, or anything outside root: curl serves a canned release, and
// mv, mkdir and sudo refuse paths outside root.
function sandbox() {
  const root = mkdtempSync(path.join(tmpdir(), 'zlib-install-'));
  const bin = path.join(root, 'fakebin');
  const work = path.join(root, 'work');
  mkdirSync(bin);
  mkdirSync(path.join(work, 'archive'), { recursive: true });
  mkdirSync(path.join(root, 'tmp'));

  writeFileSync(path.join(work, 'archive', 'zlib'), '#!/bin/sh\necho "zlib version v9.9.9"\n');
  chmodSync(path.join(work, 'archive', 'zlib'), 0o755);
  const archive = path.join(work, 'release.tar.gz');
  execFileSync(realTool('tar'), ['-czf', archive, '-C', path.join(work, 'archive'), 'zlib']);

  const tool = (name, body) => {
    writeFileSync(path.join(bin, name), `#!/bin/sh\n${body}\n`);
    chmodSync(path.join(bin, name), 0o755);
  };
  tool('curl', `case "$*" in
  *api.github.com*) echo '  "tag_name": "v9.9.9",' ;;
  *-o*) while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done; cp "${archive}" "$out" ;;
esac`);
  const guard = (name) => tool(name, `for a in "$@"; do
  case "$a" in
    -*) ;;
    "${root}"/*) ;;
    *) echo "fake ${name}: refusing path outside the sandbox: $a" >&2; exit 99 ;;
  esac
done
exec "${realTool(name)}" "$@"`);
  guard('mv');
  guard('mkdir');
  // sudo leaves a trace and stands in for elevation by unlocking the one
  // directory the privilege test locks; the guarded tools still apply.
  tool('sudo', `echo "$*" >> "${root}/sudo.log"\nchmod u+w "${root}/locked" 2>/dev/null\nexec "$@"`);

  return { root, bin };
}

function install(env, installDir) {
  return spawnSync('sh', [script], {
    encoding: 'utf8',
    env: { ...process.env, PATH: `${env.bin}:${process.env.PATH}`, TMPDIR: path.join(env.root, 'tmp'), ZLIB_INSTALL_DIR: installDir },
  });
}

test('creates a missing install directory', { skip: !canRun }, () => {
  const env = sandbox();
  try {
    const dir = path.join(env.root, 'prefix', 'bin');
    const result = install(env, dir);
    assert.equal(result.status, 0, result.stderr + result.stdout);
    assert.ok(existsSync(path.join(dir, 'zlib')), 'binary was not installed');
    assert.match(result.stdout, /zlib version v9\.9\.9/);
    assert.equal(existsSync(path.join(env.root, 'sudo.log')), false, 'sudo used for a writable parent');
  } finally {
    rmSync(env.root, { recursive: true, force: true });
  }
});

test('installs into an existing directory', { skip: !canRun }, () => {
  const env = sandbox();
  try {
    const dir = path.join(env.root, 'existing');
    mkdirSync(dir);
    const result = install(env, dir);
    assert.equal(result.status, 0, result.stderr + result.stdout);
    assert.ok(existsSync(path.join(dir, 'zlib')));
    assert.equal(existsSync(path.join(env.root, 'sudo.log')), false);
  } finally {
    rmSync(env.root, { recursive: true, force: true });
  }
});

test('uses sudo to create a directory the user cannot write', { skip: !canRun || process.getuid?.() === 0 }, () => {
  const env = sandbox();
  const locked = path.join(env.root, 'locked');
  try {
    mkdirSync(locked);
    chmodSync(locked, 0o555);
    const dir = path.join(locked, 'bin');
    const result = install(env, dir);
    chmodSync(locked, 0o755);
    assert.equal(result.status, 0, result.stderr + result.stdout);
    assert.ok(existsSync(path.join(dir, 'zlib')));
    const log = readFileSync(path.join(env.root, 'sudo.log'), 'utf8');
    assert.match(log, /mkdir -p/);
  } finally {
    chmodSync(locked, 0o755);
    rmSync(env.root, { recursive: true, force: true });
  }
});
