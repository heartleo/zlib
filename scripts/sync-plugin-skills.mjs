// .agents/skills is the source; plugins/zlib/skills is the distributable copy.
// Only the skills in pluginSkills ship with the plugin. Everything else under
// .agents/skills (repo-readme, zlib-tui, ...) is repository-level maintainer
// tooling and must not reach users.
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const source = join(root, '.agents', 'skills');
const target = join(root, 'plugins', 'zlib', 'skills');
const pluginSkills = ['kindle', 'metadata', 'zlib'];
const args = process.argv.slice(2);
if (args.some(arg => arg !== '--check')) {
  throw new Error('Usage: node scripts/sync-plugin-skills.mjs [--check]');
}
const check = args.includes('--check');

function filesUnder(directory, base = directory) {
  if (!existsSync(directory)) return [];
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`Skill bundles must contain regular files: ${path}`);
    return entry.isDirectory() ? filesUnder(path, base) : [relative(base, path)];
  }).sort();
}

const sourceFiles = pluginSkills.flatMap(name => {
  const files = filesUnder(join(source, name)).map(file => join(name, file));
  if (!files.length) throw new Error(`Plugin skill not found: .agents/skills/${name}`);
  return files;
});
const sourceSet = new Set(sourceFiles);
const extra = filesUnder(target).filter(file => !sourceSet.has(file));
if (extra.length) {
  throw new Error(`Remove obsolete generated files from plugins/zlib/skills: ${extra.join(', ')}`);
}
const changed = sourceFiles.filter(file => {
  const destination = join(target, file);
  return !existsSync(destination) ||
    !readFileSync(join(source, file)).equals(readFileSync(destination));
});
if (check && changed.length) {
  console.error(`Plugin skills are out of sync: ${changed.join(', ')}`);
  console.error('Run node scripts/sync-plugin-skills.mjs and include the generated files.');
  process.exitCode = 1;
} else if (check) {
  console.log(`Plugin skills match .agents/skills (${sourceFiles.length} files).`);
} else {
  for (const file of changed) {
    const destination = join(target, file);
    mkdirSync(dirname(destination), { recursive: true });
    copyFileSync(join(source, file), destination);
  }
  console.log(`Synced ${changed.length} plugin skill files from .agents/skills.`);
}
