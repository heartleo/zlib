// The plugin package serves Claude Code, Codex and Grok from one directory. These
// checks keep its MCP server registration and its two manifests in step.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const plugin = new URL('../plugins/zlib/', import.meta.url);
const json = path => JSON.parse(readFileSync(fileURLToPath(new URL(path, plugin)), 'utf8'));

test('.mcp.json registers zlib mcp as a stdio server', () => {
  const { mcpServers } = json('.mcp.json');
  assert.deepEqual(Object.keys(mcpServers), ['zlib']);
  // No flags: a user who wants --read-only registers the server by hand.
  assert.deepEqual(mcpServers.zlib, { command: 'zlib', args: ['mcp'] });
});

test('Codex points its manifest at .mcp.json', () => {
  assert.equal(json('.codex-plugin/plugin.json').mcpServers, './.mcp.json');
});

test('the manifests agree on name and version', () => {
  const claude = json('.claude-plugin/plugin.json');
  const codex = json('.codex-plugin/plugin.json');
  assert.equal(claude.name, 'zlib');
  assert.equal(codex.name, claude.name);
  assert.equal(codex.version, claude.version);
  assert.match(claude.version, /^\d+\.\d+\.\d+$/);
});
