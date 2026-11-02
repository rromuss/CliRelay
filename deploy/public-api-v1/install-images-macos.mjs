// Run on the authorized client Mac; stdin is the reviewed MCP module, no secrets.
// Existing model/provider/credential fields are preserved byte-for-byte.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';

let stage = 'preflight';
let installed;
try {
  let source = '';
  for await (const chunk of process.stdin) source += chunk;
  if (!source.includes('export function runStdio') || Buffer.byteLength(source) > 128 * 1024) throw new Error();
  // v1.1 imports a reference store; use the reviewed multi-file v3 installer.
  if (source.includes("'./image-references.mjs'")) throw new Error();
  const home = os.homedir(), directory = path.join(home, '.codex');
  const config = path.join(directory, 'config.toml');
  const original = fs.readFileSync(config, 'utf8');
  if (!original.includes('[model_providers.clirelay-antigravity]') || /\[mcp_servers\.clirelay_images(?:\.|\])/.test(original)) throw new Error();
  const node = path.join(home, '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node');
  const codex = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
  if (!fs.existsSync(node) || !fs.existsSync(codex)) throw new Error();
  const toolDirectory = path.join(directory, 'tools/clirelay-images');
  if (fs.existsSync(toolDirectory)) throw new Error();
  fs.mkdirSync(toolDirectory, { recursive: true, mode: 0o700 });
  const modulePath = path.join(toolDirectory, 'server.mjs');
  installed = modulePath;
  fs.writeFileSync(modulePath, source, { mode: 0o600, flag: 'wx' });
  stage = 'module-validation';
  if (spawnSync(node, ['--check', modulePath], { stdio: 'ignore', timeout: 10000 }).status !== 0) throw new Error();
  const addition = `\n\n[mcp_servers.clirelay_images]\ncommand = ${JSON.stringify(node)}\nargs = [${JSON.stringify(modulePath)}]\nenabled = true\nenabled_tools = ["imagegen"]\nstartup_timeout_sec = 15\ntool_timeout_sec = 330\n`;
  const updated = original.replace(/\s*$/, '') + addition;
  stage = 'config-validation';
  const check = fs.mkdtempSync('/private/tmp/clirelay-image-config-check-');
  fs.chmodSync(check, 0o700);
  const candidate = path.join(check, 'config.toml');
  fs.writeFileSync(candidate, updated, { mode: 0o600, flag: 'wx' });
  const parsed = spawnSync(codex, ['features', 'list'], { env: { ...process.env, CODEX_HOME: check }, stdio: 'ignore', timeout: 20000 });
  fs.unlinkSync(candidate);
  if (parsed.status !== 0 || fs.readFileSync(config, 'utf8') !== original) throw new Error();
  stage = 'config-write';
  const backup = fs.mkdtempSync(path.join(directory, 'clirelay-images-backup-'));
  fs.chmodSync(backup, 0o700);
  fs.writeFileSync(path.join(backup, 'config.toml'), original, { mode: 0o600, flag: 'wx' });
  const pending = path.join(directory, `.config.images-${process.pid}.toml`);
  const fd = fs.openSync(pending, 'wx', 0o600);
  try { fs.writeFileSync(fd, updated); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
  fs.renameSync(pending, config);
  installed = null;
  console.log(JSON.stringify({ ok: true, mcp_server: 'clirelay_images', tool: 'imagegen', previous_config_backup: backup, source_sha256: createHash('sha256').update(source).digest('hex'), config_mode: (fs.statSync(config).mode & 0o777).toString(8), credentials_changed: false }));
} catch {
  if (installed) { try { fs.unlinkSync(installed); } catch {} }
  console.error(JSON.stringify({ ok: false, stage, details_suppressed: true }));
  process.exitCode = 1;
}
