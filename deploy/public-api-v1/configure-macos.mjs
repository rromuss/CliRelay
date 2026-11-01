// Explicit owner-authorized replacement of ModelHub on one macOS client.
// Input JSON with apiKey arrives over SSH stdin; never print or log that input.
// This variant deliberately uses a private TOML credential instead of Keychain.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

let stage = 'input';
try {
  let raw = '';
  for await (const chunk of process.stdin) raw += chunk;
  const { apiKey } = JSON.parse(raw);
  if (typeof apiKey !== 'string' || !/^sk-[A-Za-z0-9_-]{40,100}$/.test(apiKey)) throw new Error();
  const home = os.homedir();
  const codex = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
  const directory = path.join(home, '.codex');
  const config = path.join(directory, 'config.toml');
  const original = fs.readFileSync(config, 'utf8');
  const authPath = path.join(directory, 'auth.json');
  const authBefore = fs.existsSync(authPath) ? fs.readFileSync(authPath) : null;
  stage = 'public-api-check';
  const headers = { Authorization: `Bearer ${apiKey}` };
  for (const endpoint of ['/v1/models', '/codex/models?client_version=0.166.0']) {
    const response = await fetch(`https://cliprelay.romus.pro:8443${endpoint}`, { headers, signal: AbortSignal.timeout(15000) });
    if (response.status !== 200) throw new Error();
    await response.arrayBuffer();
  }
  stage = 'config-shape';
  if (original.includes('[model_providers.clirelay-antigravity]')) throw new Error();
  const lines = original.split('\n');
  const firstTable = lines.findIndex(line => /^\s*\[/.test(line));
  if (firstTable < 0) throw new Error();
  // The former ModelHub context overrides must not supersede Gemini's catalog.
  let root = lines.slice(0, firstTable).join('\n').replace(/^\s*model_(?:context_window|auto_compact_token_limit)\s*=.*$/gm, '');
  for (const [key, value] of Object.entries({ model: 'gemini-3.8-flash-tiered', model_provider: 'clirelay-antigravity', model_reasoning_effort: 'medium', web_search: 'disabled' })) {
    const match = new RegExp(`^\\s*${key}\\s*=.*$`, 'gm');
    const occurrences = root.match(match) ?? [];
    if (occurrences.length > 1) throw new Error();
    root = occurrences.length ? root.replace(match, `${key} = ${JSON.stringify(value)}`) : `${key} = ${JSON.stringify(value)}\n${root}`;
  }
  const start = lines.findIndex(line => /^\s*\[model_providers\.modelhub\]\s*(?:#.*)?$/.test(line));
  if (start < firstTable || lines.some(line => /^\s*\[model_providers\.modelhub\./.test(line))) throw new Error();
  const next = lines.findIndex((line, index) => index > start && /^\s*\[/.test(line));
  const end = next < 0 ? lines.length : next;
  const provider = `[model_providers.clirelay-antigravity]
name = "CliRelay Antigravity"
base_url = "https://cliprelay.romus.pro:8443/v1"
model_catalog_url = "https://cliprelay.romus.pro:8443/codex/models"
wire_api = "responses"
experimental_bearer_token = ${JSON.stringify(apiKey)}
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
`;
  const updated = `${root}\n${lines.slice(firstTable, start).join('\n')}${start > firstTable ? '\n' : ''}${provider}\n${lines.slice(end).join('\n')}`;
  stage = 'native-config-validation';
  const temporary = fs.mkdtempSync('/private/tmp/clirelay-config-check-');
  fs.chmodSync(temporary, 0o700);
  const candidate = path.join(temporary, 'config.toml');
  fs.writeFileSync(candidate, updated, { mode: 0o600, flag: 'wx' });
  const result = spawnSync(codex, ['features', 'list'], { env: { ...process.env, CODEX_HOME: temporary }, encoding: 'utf8', timeout: 20000 });
  fs.unlinkSync(candidate);
  if (result.status !== 0) throw new Error();
  stage = 'atomic-config-write';
  if (fs.readFileSync(config, 'utf8') !== original) throw new Error();
  const backup = fs.mkdtempSync(path.join(directory, 'clirelay-backup-'));
  fs.chmodSync(backup, 0o700);
  fs.writeFileSync(path.join(backup, 'config.toml'), original, { mode: 0o600, flag: 'wx' });
  const pending = path.join(directory, `.config.clirelay-${process.pid}.toml`);
  const fd = fs.openSync(pending, 'wx', 0o600);
  try { fs.writeFileSync(fd, updated); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
  fs.renameSync(pending, config);
  const authAfter = fs.existsSync(authPath) ? fs.readFileSync(authPath) : null;
  if ((authBefore === null) !== (authAfter === null) || (authBefore && !authBefore.equals(authAfter))) throw new Error();
  console.log(JSON.stringify({ ok: true, model: 'gemini-3.8-flash-tiered', provider: 'clirelay-antigravity', public_catalogs: 200, native_config_valid: true, config_mode: (fs.statSync(config).mode & 0o777).toString(8), previous_config_backup: backup, openai_auth_unchanged: true, keychain_used: false }));
} catch {
  console.error(JSON.stringify({ ok: false, stage, details_suppressed: true }));
  process.exitCode = 1;
}
