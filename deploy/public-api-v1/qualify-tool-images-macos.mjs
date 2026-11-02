// Additional live vision check, no image generation. The only tool returns a
// synthetic screenshot between two text parts; no browser/session is inspected.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';

if (process.argv[2] !== '--allow-live' || !path.isAbsolute(process.argv[3] ?? '')) throw Error('explicit live check and previous qualification directory required');
const previous = process.argv[3], actual = fs.readFileSync(path.join(os.homedir(), '.codex/config.toml'), 'utf8');
const all = [...actual.matchAll(/^[ \t]*\[([^\]\n]+)\][^\n]*(?:\n|$)/gm)];
const index = all.findIndex(x => x[1] === 'model_providers.clirelay-antigravity');
if (index < 0) throw Error('provider unavailable');
const provider = actual.slice(all[index].index, all[index + 1]?.index ?? actual.length);
let key;
try { key = JSON.parse(provider.match(/^[ \t]*experimental_bearer_token\s*=\s*("(?:[^"\\]|\\.)*")\s*(?:#.*)?$/m)?.[1] ?? 'null'); } catch { throw Error('credential unavailable'); }
if (typeof key !== 'string') throw Error('credential unavailable');
const prior = fs.readFileSync(path.join(previous, 'ocr.jsonl'), 'utf8').split('\n').flatMap(line => { try { return [JSON.parse(line)]; } catch { return []; } });
const expected = prior.filter(x => x.item?.type === 'agent_message').map(x => x.item.text).join('\n').match(/[AKRX2579]{4}/)?.[0];
if (!expected) throw Error('prior checked OCR result missing');
const data = fs.readFileSync(path.join(previous, 'work/original.png')).toString('base64');
const root = fs.mkdtempSync('/private/tmp/clirelay-v3-toolimage-'); fs.chmodSync(root, 0o700);
const home = path.join(root, 'home'), work = path.join(root, 'work'); fs.mkdirSync(home, { mode: 0o700 }); fs.mkdirSync(work, { mode: 0o700 });
const module = path.join(root, 'screenshot-fixture.mjs');
const result = { content: [{ type: 'text', text: 'Browser documentation marker: BROWSER_DOCS_OK. State follows as a screenshot.' }, { type: 'image', data, mimeType: 'image/png' }, { type: 'text', text: 'Fresh state marker: BROWSER_STATE_OK.' }], isError: false };
fs.writeFileSync(module, `import readline from 'node:readline';const payload=${JSON.stringify(result)};const r=readline.createInterface({input:process.stdin});r.on('line',line=>{let m;try{m=JSON.parse(line)}catch{return}if(!Object.hasOwn(m,'id'))return;let result;if(m.method==='initialize')result={protocolVersion:m.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'vision-fixture',version:'1'}};else if(m.method==='tools/list')result={tools:[{name:'take_screenshot',description:'Return a synthetic screenshot and its surrounding text markers.',inputSchema:{type:'object',properties:{},additionalProperties:false},annotations:{readOnlyHint:true,destructiveHint:false}}]};else if(m.method==='tools/call')result=payload;else{console.log(JSON.stringify({jsonrpc:'2.0',id:m.id,error:{code:-32601,message:'Method not found'}}));return}console.log(JSON.stringify({jsonrpc:'2.0',id:m.id,result}))});`, { mode: 0o600 });
const config = path.join(home, 'config.toml');
fs.writeFileSync(config, `model="gemini-3.8-flash-tiered"\nmodel_provider="clirelay-antigravity"\nmodel_reasoning_effort="medium"\nweb_search="disabled"\n[features]\nmulti_agent=false\n${provider}\n[mcp_servers.vision_fixture]\ncommand=${JSON.stringify(process.execPath)}\nargs=${JSON.stringify([module])}\n[mcp_servers.vision_fixture.tools.take_screenshot]\napproval_mode="approve"\n`, { mode: 0o600 });
const environment = { ...process.env, CODEX_HOME: home };
for (const name of ['OPENAI_API_KEY', 'GOOGLE_API_KEY', 'GEMINI_API_KEY']) delete environment[name];
const cli = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
const child = spawn(cli, ['exec', '--skip-git-repo-check', '--sandbox', 'read-only', '--json', '--', '-'], { cwd: work, env: environment, stdio: ['pipe', 'pipe', 'pipe'] });
let stdout = '', stderr = '';
child.stdout.on('data', value => { stdout += value; }); child.stderr.on('data', value => { stderr += value; });
child.stdin.end('Вызови vision_fixture take_screenshot ровно один раз. Прочитай четырёхсимвольный код на полученном изображении и оба текстовых маркера вокруг него. Ответь кодом и маркерами. Не читай файлы, не используй команды или другие инструменты.');
const timer = setTimeout(() => child.kill('SIGTERM'), 180000);
try {
  const exit = await new Promise(resolve => { child.once('exit', resolve); child.once('error', () => resolve(-1)); }); clearTimeout(timer);
  if (stdout.includes(key) || stderr.includes(key)) throw Error('credential diagnostic suppressed');
  fs.writeFileSync(path.join(root, 'events.jsonl'), stdout, { mode: 0o600 });
  const events = stdout.split('\n').flatMap(line => { try { return [JSON.parse(line)]; } catch { return []; } });
  const items = events.filter(x => x.type === 'item.completed').map(x => x.item);
  const text = items.filter(x => x.type === 'agent_message').map(x => x.text).join('\n');
  const calls = items.filter(x => x.type === 'mcp_tool_call');
  const allowed = items.every(x => ['reasoning', 'agent_message', 'mcp_tool_call'].includes(x.type));
  const ok = exit === 0 && calls.length === 1 && String(calls[0].tool).includes('take_screenshot') && allowed && text.includes(expected) && text.includes('BROWSER_DOCS_OK') && text.includes('BROWSER_STATE_OK');
  const proof = { passed: ok, nested_tool_image_read: text.includes(expected), surrounding_text_preserved: text.includes('BROWSER_DOCS_OK') && text.includes('BROWSER_STATE_OK'), only_expected_tool: calls.length === 1 && allowed, image_generation_calls: 0, test_directory: root };
  fs.writeFileSync(path.join(root, 'proof.json'), JSON.stringify(proof), { mode: 0o600 }); console.log(JSON.stringify(proof));
  if (!ok) process.exitCode = 1;
} finally { fs.unlinkSync(config); }
