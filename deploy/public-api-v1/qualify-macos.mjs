// Bounded qualification on the actual Mac using its installed CLI and provider.
// Copies only this provider into a private disposable test home. No user chats,
// MCP servers, account files or project instructions enter the synthetic test.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';

if (process.argv[2] !== '--allow-live') throw new Error('explicit --allow-live required');
const codex = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
const actual = fs.readFileSync(path.join(os.homedir(), '.codex/config.toml'), 'utf8');
const lines = actual.split('\n');
const start = lines.findIndex(line => line.trim() === '[model_providers.clirelay-antigravity]');
const next = lines.findIndex((line, i) => i > start && /^\s*\[/.test(line));
if (start < 0) throw new Error('configured provider missing');
const provider = lines.slice(start, next < 0 ? lines.length : next).join('\n');
const credential = provider.match(/^experimental_bearer_token\s*=\s*(".*")\s*$/m);
if (!credential) throw new Error('credential field missing');
const secret = Buffer.from(JSON.parse(credential[1]));
const root = fs.mkdtempSync('/private/tmp/clirelay-live166-');
fs.chmodSync(root, 0o700);
const home = path.join(root, 'home'), work = path.join(root, 'work');
fs.mkdirSync(home, { mode: 0o700 }); fs.mkdirSync(work, { mode: 0o700 });
const configPath = path.join(home, 'config.toml');
fs.writeFileSync(configPath, `model = "gemini-3.8-flash-tiered"\nmodel_provider = "clirelay-antigravity"\nmodel_reasoning_effort = "medium"\nweb_search = "disabled"\n${provider}`, { mode: 0o600 });
fs.writeFileSync(path.join(work, 'note.txt'), 'Disposable laptop protocol fixture.\n');
const environment = { ...process.env, CODEX_HOME: home };
for (const key of ['OPENAI_API_KEY', 'GEMINI_API_KEY', 'GOOGLE_API_KEY']) delete environment[key];

async function run(args, name, milliseconds) {
  const child = spawn(codex, args, { cwd: work, env: environment, stdio: ['ignore', 'pipe', 'pipe'] });
  let stdout = '', stderr = '';
  child.stdout.on('data', chunk => { stdout += chunk; });
  child.stderr.on('data', chunk => { stderr += chunk; });
  const timer = setTimeout(() => child.kill('SIGTERM'), milliseconds);
  const exit = await new Promise(resolve => { child.on('exit', resolve); child.on('error', () => resolve(-1)); });
  clearTimeout(timer);
  // Do not persist unexpected credential-bearing diagnostic output.
  if (stdout.includes(secret.toString()) || stderr.includes(secret.toString())) throw new Error('credential diagnostic suppressed');
  fs.writeFileSync(path.join(root, `${name}.jsonl`), stdout, { mode: 0o600 });
  const events = stdout.split('\n').flatMap(line => { try { return [JSON.parse(line)]; } catch { return []; } });
  return { exit, events, primaryMetadataWarning: stderr.includes('Unknown model gemini-3.8-flash-tiered') };
}

try {
  const first = await run(['exec', '--skip-git-repo-check', '--sandbox', 'workspace-write', '--json', 'This is a disposable protocol fixture. Use a local shell to read note.txt. Use apply_patch to append exactly LAPTOP_166_OK on its own line. The native apply_patch format uses a bare @@ line; do not put unified-diff line numbers after @@. Read the file again with a local shell. Reply exactly LAPTOP_166_DONE. Do not access network tools or other files.'], 'first', 180000);
  const changed = fs.readFileSync(path.join(work, 'note.txt'), 'utf8').endsWith('LAPTOP_166_OK\n');
  const cache = JSON.parse(fs.readFileSync(path.join(home, 'models_cache.json'), 'utf8'));
  console.log(JSON.stringify({ phase: 'tools', exit: first.exit, file_changed: changed, catalog_models: cache.models.map(x => x.slug), item_types: first.events.filter(x => x.type === 'item.completed').map(x => x.item?.type), completed: first.events.some(x => x.type === 'turn.completed'), primary_metadata_warning: first.primaryMetadataWarning }));
  if (first.exit !== 0 || !changed) throw new Error('tool qualification failed');
  const thread = first.events.find(x => x.type === 'thread.started')?.thread_id;
  if (!thread) throw new Error('thread missing');
  const follow = await run(['exec', 'resume', thread, '--skip-git-repo-check', '--json', 'Without tools, reply only with the exact line you appended in the preceding turn.'], 'followup', 100000);
  const recalled = follow.events.some(x => x.item?.type === 'agent_message' && x.item.text?.includes('LAPTOP_166_OK'));
  console.log(JSON.stringify({ phase: 'followup', exit: follow.exit, history_recalled: recalled, completed: follow.events.some(x => x.type === 'turn.completed'), test_directory: root }));
  if (follow.exit !== 0 || !recalled) throw new Error('follow-up qualification failed');
} finally {
  fs.unlinkSync(configPath);
  secret.fill(0);
}
