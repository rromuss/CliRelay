// Run on the authorized client. stdin contains reviewed public tool sources,
// never credentials. Existing provider fields and all tool approvals survive.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

const IMAGE_FILES = ['clirelay-images.mjs', 'image-references.mjs', 'prepare-reference.mjs'];
const SEARCH_FILES = ['ddgs-search.py', 'ddgs-requirements.lock'];
const hash = value => createHash('sha256').update(value).digest('hex');
const headers = text => [...text.matchAll(/^[ \t]*\[([^\]\n]+)\][^\n]*(?:\n|$)/gm)];

export function isCliRelayOnlyCatalog(raw) {
  try {
    const value = JSON.parse(raw);
    return Array.isArray(value.models) && value.models.length === 1 && value.models[0].slug === 'gemini-3.8-flash-tiered';
  } catch { return false; }
}

function sectionRange(text, name) {
  const all = headers(text), matches = all.filter(item => item[1] === name);
  if (matches.length > 1) throw new Error('Duplicate section');
  if (!matches.length) return null;
  const index = all.indexOf(matches[0]);
  return [matches[0].index, all[index + 1]?.index ?? text.length];
}

function table(text, name, values) {
  const range = sectionRange(text, name);
  if (!range) return text.replace(/\s*$/, '') + `\n\n[${name}]\n` + Object.entries(values).map(([key, value]) => `${key} = ${value}\n`).join('');
  let body = text.slice(...range);
  for (const [key, value] of Object.entries(values)) {
    const pattern = new RegExp(`^[ \\t]*${key}[ \\t]*=[^\\n]*(?:\\n|$)`, 'gm');
    const matches = [...body.matchAll(pattern)];
    if (matches.length > 1) throw new Error('Duplicate key');
    if (matches.length) body = body.replace(pattern, `${key} = ${value}\n`);
    else body = body.replace(/\s*$/, '') + `\n${key} = ${value}\n`;
  }
  return text.slice(0, range[0]) + body + text.slice(range[1]);
}

export function updateClientConfig(original, { node, imageModule, python, searchModule }) {
  if (!sectionRange(original, 'model_providers.clirelay-antigravity') || !sectionRange(original, 'mcp_servers.clirelay_images')) throw new Error('Existing CliRelay configuration required');
  const providerRange = sectionRange(original, 'model_providers.clirelay-antigravity');
  const provider = original.slice(...providerRange);
  let updated = table(original, 'mcp_servers.clirelay_images', { command: JSON.stringify(node), args: JSON.stringify([imageModule]), enabled: 'true', enabled_tools: '["imagegen"]', startup_timeout_sec: '15', tool_timeout_sec: '330' });
  updated = table(updated, 'mcp_servers.clirelay_search', { command: JSON.stringify(python), args: JSON.stringify([searchModule]), enabled: 'true', enabled_tools: '["web_search"]', startup_timeout_sec: '15', tool_timeout_sec: '35' });
  if (updated.slice(...sectionRange(updated, 'model_providers.clirelay-antigravity')) !== provider) throw new Error('Provider changed');
  return updated;
}

function checked(command, args, options = {}) {
  const result = spawnSync(command, args, { stdio: 'pipe', timeout: 20000, ...options });
  if (result.status !== 0) throw new Error('Command failed');
  return result;
}

function discover(command, args, tool) {
  const input = [
    { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'clirelay-install-check', version: '1' } } },
    { jsonrpc: '2.0', id: 2, method: 'tools/list' },
  ].map(JSON.stringify).join('\n') + '\n';
  const result = checked(command, args, { input, encoding: 'utf8' });
  const replies = result.stdout.split('\n').flatMap(line => { try { return [JSON.parse(line)]; } catch { return []; } });
  if (!replies.some(value => value.id === 2 && value.result?.tools?.length === 1 && value.result.tools[0].name === tool)) throw new Error('MCP discovery failed');
}

export function installMultimodal(payload, { home = os.homedir() } = {}) {
  let stage = 'preflight', backup, pending, pendingCreated = false, configWritten = false;
  const created = [];
  try {
    const names = [...IMAGE_FILES, ...SEARCH_FILES];
    if (!payload || payload.version !== 'v0.4.6-public-api.3' || !payload.files || Object.keys(payload.files).sort().join() !== names.toSorted().join() || names.some(name => typeof payload.files[name] !== 'string' || Buffer.byteLength(payload.files[name]) > 256 * 1024)) throw new Error();
    const codexHome = path.join(home, '.codex'), config = path.join(codexHome, 'config.toml');
    const stat = fs.lstatSync(config);
    if (!stat.isFile() || stat.isSymbolicLink() || stat.uid !== process.getuid() || (stat.mode & 0o077)) throw new Error();
    const original = fs.readFileSync(config, 'utf8');
    const cachePath = path.join(codexHome, 'models_cache.json');
    let cache;
    if (fs.existsSync(cachePath)) {
      const entry = fs.lstatSync(cachePath);
      if (entry.isFile() && !entry.isSymbolicLink() && entry.uid === process.getuid() && entry.size < 4 * 1024 * 1024) {
        const raw = fs.readFileSync(cachePath, 'utf8');
        if (isCliRelayOnlyCatalog(raw)) cache = raw;
      }
    }
    const existingSearch = sectionRange(original, 'mcp_servers.clirelay_search');
    if (existingSearch && !original.slice(...existingSearch).includes('/clirelay-search/releases/')) throw new Error('Unmanaged search configuration');
    const node = path.join(home, '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node');
    const python = path.join(home, '.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3');
    const codex = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
    if (![node, python, codex].every(name => fs.existsSync(name))) throw new Error();
    const revision = hash(names.map(name => `${name}\0${payload.files[name]}`).join('\0')).slice(0, 16);
    const imageDirectory = path.join(codexHome, 'tools/clirelay-images/releases', revision);
    const searchDirectory = path.join(codexHome, 'tools/clirelay-search/releases', revision);
    stage = 'tool-sources';
    for (const [directory, files] of [[imageDirectory, IMAGE_FILES], [searchDirectory, SEARCH_FILES]]) {
      if (fs.existsSync(directory)) throw new Error('Release already exists');
      fs.mkdirSync(path.dirname(directory), { recursive: true, mode: 0o700 });
      fs.mkdirSync(directory, { mode: 0o700 }); created.push(directory);
      for (const name of files) fs.writeFileSync(path.join(directory, name), payload.files[name], { mode: 0o600, flag: 'wx' });
    }
    stage = 'source-validation';
    for (const name of IMAGE_FILES) checked(node, ['--check', path.join(imageDirectory, name)]);
    stage = 'search-environment';
    const venv = path.join(searchDirectory, '.venv');
    checked(python, ['-m', 'venv', venv], { timeout: 60000 });
    const searchPython = path.join(venv, 'bin/python3');
    checked(searchPython, ['-m', 'pip', '--isolated', '--disable-pip-version-check', 'install', '--only-binary=:all:', '--no-cache-dir', '--index-url', 'https://pypi.org/simple', '--require-hashes', '-r', path.join(searchDirectory, 'ddgs-requirements.lock')], { timeout: 180000 });
    checked(searchPython, ['-c', 'import ddgs, primp, lxml.etree']);
    const imageModule = path.join(imageDirectory, 'clirelay-images.mjs'), searchModule = path.join(searchDirectory, 'ddgs-search.py');
    stage = 'mcp-discovery';
    discover(node, [imageModule], 'imagegen');
    discover(searchPython, [searchModule], 'web_search');
    stage = 'config-validation';
    const updated = updateClientConfig(original, { node, imageModule, python: searchPython, searchModule });
    const candidateHome = fs.mkdtempSync('/private/tmp/clirelay-multimodal-config-');
    fs.chmodSync(candidateHome, 0o700);
    try {
      fs.writeFileSync(path.join(candidateHome, 'config.toml'), updated, { mode: 0o600, flag: 'wx' });
      checked(codex, ['features', 'list'], { env: { ...process.env, CODEX_HOME: candidateHome } });
    } finally { fs.rmSync(candidateHome, { recursive: true, force: true }); }
    if (fs.readFileSync(config, 'utf8') !== original) throw new Error('Config changed concurrently');
    stage = 'config-backup';
    backup = fs.mkdtempSync(path.join(codexHome, 'clirelay-multimodal-backup-'));
    fs.chmodSync(backup, 0o700);
    fs.writeFileSync(path.join(backup, 'config.toml'), original, { mode: 0o600, flag: 'wx' });
    if (cache !== undefined) fs.writeFileSync(path.join(backup, 'models_cache.json'), cache, { mode: 0o600, flag: 'wx' });
    fs.writeFileSync(path.join(backup, 'release.json'), JSON.stringify({ revision, imageDirectory, searchDirectory }), { mode: 0o600, flag: 'wx' });
    pending = path.join(codexHome, `.config.multimodal-${process.pid}.toml`);
    const fd = fs.openSync(pending, 'wx', 0o600);
    pendingCreated = true;
    try { fs.writeFileSync(fd, updated); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
    stage = 'config-write';
    if (fs.readFileSync(config, 'utf8') !== original) throw new Error('Config changed concurrently');
    fs.renameSync(pending, config);
    configWritten = true;
    stage = 'catalog-cache';
    let catalogCacheInvalidated = false;
    if (cache !== undefined && fs.existsSync(cachePath)) {
      const entry = fs.lstatSync(cachePath);
      if (entry.isFile() && !entry.isSymbolicLink() && entry.uid === process.getuid() && fs.readFileSync(cachePath, 'utf8') === cache) {
        fs.unlinkSync(cachePath); catalogCacheInvalidated = true;
      }
    }
    return { ok: true, version: payload.version, revision, backup, imageModule, searchModule, credentials_changed: false, tool_approvals_preserved: true, existing_sources_preserved: true, catalog_cache_invalidated: catalogCacheInvalidated };
  } catch {
    if (pendingCreated && !configWritten) { try { fs.unlinkSync(pending); } catch {} }
    if (!configWritten) for (const directory of created) { try { fs.rmSync(directory, { recursive: true, force: true }); } catch {} }
    return { ok: false, stage, backup, details_suppressed: true };
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  let raw = '';
  try {
    for await (const chunk of process.stdin) { raw += chunk; if (Buffer.byteLength(raw) > 1024 * 1024) throw new Error(); }
    const result = installMultimodal(JSON.parse(raw));
    console.log(JSON.stringify(result));
    if (!result.ok) process.exitCode = 1;
  } catch { console.error(JSON.stringify({ ok: false, stage: 'input', details_suppressed: true })); process.exitCode = 1; }
}
