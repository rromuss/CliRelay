// Actual installed client/MCP qualification, capped at one image API operation.
// Test config is private and removed; no user chats/other MCP servers are loaded.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

if (process.argv[2] !== '--allow-live-one-image') throw new Error('explicit one-image authorization required');
const codex = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
const realModule = path.join(os.homedir(), '.codex/tools/clirelay-images/server.mjs');
const actual = fs.readFileSync(path.join(os.homedir(), '.codex/config.toml'), 'utf8');
const lines = actual.split('\n');
const start = lines.findIndex(line => line.trim() === '[model_providers.clirelay-antigravity]');
const next = lines.findIndex((line, index) => index > start && /^\s*\[/.test(line));
if (start < 0 || !fs.existsSync(realModule)) throw new Error('configured image module missing');
const provider = lines.slice(start, next < 0 ? lines.length : next).join('\n');
const credential = provider.match(/^experimental_bearer_token\s*=\s*(".*")\s*$/m);
if (!credential) throw new Error('credential unavailable');
const key = JSON.parse(credential[1]);
const root = fs.mkdtempSync('/private/tmp/clirelay-image-live-'); fs.chmodSync(root, 0o700);
const home = path.join(root, 'home'), work = path.join(root, 'work'); fs.mkdirSync(home, { mode: 0o700 }); fs.mkdirSync(work, { mode: 0o700 });
const resultPath = path.join(root, 'image-result.json'), countPath = path.join(root, 'image-count.json');
const wrapper = path.join(root, 'one-image-mcp.mjs');
fs.writeFileSync(wrapper, `import fs from 'node:fs';\nimport {createImageTool,runStdio} from ${JSON.stringify(pathToFileURL(realModule).href)};\nlet attempts=0,requests=0;const generate=createImageTool();\nrunStdio({generate:async(args,signal)=>{attempts++;if(requests){fs.writeFileSync(${JSON.stringify(countPath)},JSON.stringify({attempts,requests}),{mode:0o600});throw Error('one-image test limit');}requests++;fs.writeFileSync(${JSON.stringify(countPath)},JSON.stringify({attempts,requests}),{mode:0o600});const result=await generate(args,signal);fs.writeFileSync(${JSON.stringify(resultPath)},JSON.stringify(result),{mode:0o600});return result;}});\n`, { mode: 0o600 });
const config = path.join(home, 'config.toml');
// `codex exec` defaults to approval=never. The owner explicitly authorized this
// one operation, so approve only this bounded test tool, not the real client.
fs.writeFileSync(config, `model="gemini-3.8-flash-tiered"\nmodel_provider="clirelay-antigravity"\nmodel_reasoning_effort="medium"\nweb_search="disabled"\n${provider}\n[mcp_servers.clirelay_images]\ncommand=${JSON.stringify(process.execPath)}\nargs=[${JSON.stringify(wrapper)}]\nstartup_timeout_sec=15\ntool_timeout_sec=330\n[mcp_servers.clirelay_images.tools.imagegen]\napproval_mode="approve"\n`, { mode: 0o600 });
const environment = { ...process.env, CODEX_HOME: home };
for (const name of ['OPENAI_API_KEY', 'GOOGLE_API_KEY', 'GEMINI_API_KEY']) delete environment[name];
async function run(args, name, timeout) {
  const child = spawn(codex, args, { cwd: work, env: environment, stdio: ['ignore', 'pipe', 'pipe'] });
  let stdout = '', stderr = '';
  child.stdout.on('data', part => { stdout += part; }); child.stderr.on('data', part => { stderr += part; });
  const timer = setTimeout(() => child.kill('SIGTERM'), timeout);
  const exit = await new Promise(resolve => { child.on('exit', resolve); child.on('error', () => resolve(-1)); }); clearTimeout(timer);
  if (stdout.includes(key) || stderr.includes(key)) throw new Error('credential diagnostic suppressed');
  fs.writeFileSync(path.join(root, `${name}.jsonl`), stdout, { mode: 0o600 });
  const events = stdout.split('\n').flatMap(line => { try { return [JSON.parse(line)]; } catch { return []; } });
  return { exit, events };
}
try {
  const version = spawnSync(codex, ['--version'], { encoding: 'utf8' }).stdout.trim();
  const first = await run(['exec', '--skip-git-repo-check', '--sandbox', 'workspace-write', '--json', 'Use the clirelay_images imagegen MCP tool exactly once to generate a square photorealistic ripe strawberry cut in half on a clean white background, no lettering. Do not use other image providers, shell commands or credential files. After the tool returns, reply in Russian and display the exact Markdown image it returned. Remember the marker IMAGE_CHECK_OK for the next question.'], 'image-turn', 420000);
  const count = fs.existsSync(countPath) ? JSON.parse(fs.readFileSync(countPath, 'utf8')) : { attempts: 0, requests: 0 };
  const image = fs.existsSync(resultPath) ? JSON.parse(fs.readFileSync(resultPath, 'utf8')) : null;
  const messages = first.events.filter(e => e.item?.type === 'agent_message').map(e => e.item.text ?? '').join('\n');
  const imageExists = Boolean(image && fs.existsSync(image.path));
  const metadata = imageExists ? { bytes: fs.statSync(image.path).size, mode: (fs.statSync(image.path).mode & 0o777).toString(8), mime_type: image.mime_type } : null;
  console.log(JSON.stringify({ version, first_exit: first.exit, image_api_requests: count.requests, tool_attempts: count.attempts, image_saved: imageExists, artifact: image?.path, metadata, markdown_in_final: Boolean(image && messages.includes(image.path)), event_item_types: first.events.filter(e => e.type === 'item.completed').map(e => e.item?.type), test_directory: root }));
  if (first.exit !== 0 || !imageExists || count.requests !== 1) throw new Error('one-image qualification failed');
  const thread = first.events.find(e => e.type === 'thread.started')?.thread_id;
  const follow = await run(['exec', 'resume', thread, '--skip-git-repo-check', '--json', 'Without tools or generating another image, reply with the marker I asked you to remember.'], 'followup', 100000);
  const recalled = follow.events.some(e => e.item?.type === 'agent_message' && e.item.text?.includes('IMAGE_CHECK_OK'));
  const after = JSON.parse(fs.readFileSync(countPath, 'utf8'));
  console.log(JSON.stringify({ followup_exit: follow.exit, history_recalled: recalled, image_api_requests: after.requests }));
  if (follow.exit !== 0 || !recalled || after.requests !== 1) throw new Error('image follow-up qualification failed');
} finally { fs.unlinkSync(config); }
