// Run only on the authorized client after deploy. Two image API calls maximum;
// temporary profiles stay on that client, never print/copy the real credential.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { randomBytes, createHash } from 'node:crypto';
import { deflateSync } from 'node:zlib';
import { spawn } from 'node:child_process';
import { pathToFileURL } from 'node:url';

if (process.argv[2] !== '--allow-live-two-edits') throw Error('explicit two-edit authorization required');
const cli = '/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex';
const actual = fs.readFileSync(path.join(os.homedir(), '.codex/config.toml'), 'utf8');
function section(name) {
  const all = [...actual.matchAll(/^[ \t]*\[([^\]\n]+)\][^\n]*(?:\n|$)/gm)], index = all.findIndex(x => x[1] === name);
  if (index < 0) throw Error('required client section missing');
  return actual.slice(all[index].index, all[index + 1]?.index ?? actual.length);
}
const provider = section('model_providers.clirelay-antigravity');
let key;
try { key = JSON.parse(provider.match(/^[ \t]*experimental_bearer_token\s*=\s*("(?:[^"\\]|\\.)*")\s*(?:#.*)?$/m)?.[1] ?? 'null'); }
catch { throw Error('credential unavailable'); }
if (typeof key !== 'string') throw Error('credential unavailable');
const imageArgs = JSON.parse(section('mcp_servers.clirelay_images').match(/^args\s*=\s*(\[[^\n]*\])\s*$/m)[1]);
const imageModule = imageArgs[0];
if (imageArgs.length !== 1 || !fs.existsSync(imageModule)) throw Error('installed image MCP missing');
const root = fs.mkdtempSync('/private/tmp/clirelay-v3-qualification-'); fs.chmodSync(root, 0o700);
const home = path.join(root, 'home'), work = path.join(root, 'work');
fs.mkdirSync(home, { mode: 0o700 }); fs.mkdirSync(work, { mode: 0o700 });
const config = path.join(home, 'config.toml'), counter = path.join(root, 'image-calls.json');
const hash = value => createHash('sha256').update(value).digest('hex');

const font = { A: ['01110','10001','10001','11111','10001','10001','10001'], K: ['10001','10010','10100','11000','10100','10010','10001'], R: ['11110','10001','10001','11110','10100','10010','10001'], X: ['10001','10001','01010','00100','01010','10001','10001'], 2: ['01110','10001','00001','00010','00100','01000','11111'], 5: ['11111','10000','11110','00001','00001','10001','01110'], 7: ['11111','00001','00010','00100','01000','01000','01000'], 9: ['01110','10001','10001','01111','00001','00010','11100'] };
function nonce() { return [...randomBytes(4)].map(n => Object.keys(font)[n % Object.keys(font).length]).join(''); }
function crc32(bytes) { let c = 0xffffffff; for (const b of bytes) { c ^= b; for (let i = 0; i < 8; i++) c = (c >>> 1) ^ ((c & 1) ? 0xedb88320 : 0); } return (c ^ 0xffffffff) >>> 0; }
function png(code, color, circle) {
  const width = 760, height = 400, pixels = Buffer.alloc(width * height * 4, 255);
  const point = (x, y, rgb) => { const i = (y * width + x) * 4; pixels[i] = rgb[0]; pixels[i + 1] = rgb[1]; pixels[i + 2] = rgb[2]; };
  for (let y = 35; y < 115; y++) for (let x = 35; x < 115; x++) if (!circle || (x - 75) ** 2 + (y - 75) ** 2 < 40 ** 2) point(x, y, color);
  const scale = 23;
  [...code].forEach((letter, k) => font[letter].forEach((row, y) => [...row].forEach((bit, x) => {
    if (bit === '1') for (let a = 0; a < scale; a++) for (let b = 0; b < scale; b++) point(100 + (k * 6 + x) * scale + b, 160 + y * scale + a, [0, 0, 0]);
  })));
  const raw = Buffer.alloc((width * 4 + 1) * height);
  for (let y = 0; y < height; y++) pixels.copy(raw, y * (width * 4 + 1) + 1, y * width * 4, (y + 1) * width * 4);
  function chunk(type, data) { const t = Buffer.from(type), out = Buffer.alloc(data.length + 12); out.writeUInt32BE(data.length); t.copy(out, 4); data.copy(out, 8); out.writeUInt32BE(crc32(Buffer.concat([t, data])), data.length + 8); return out; }
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(width); ihdr.writeUInt32BE(height, 4); ihdr[8] = 8; ihdr[9] = 6;
  return Buffer.concat([Buffer.from('\x89PNG\r\n\x1a\n', 'latin1'), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}
const a = nonce(); let b = nonce(); while (a === b) b = nonce();
const left = path.join(work, 'original.png'), right = path.join(work, 'comparison.png');
fs.writeFileSync(left, png(a, [20, 85, 235], true), { mode: 0o600 });
fs.writeFileSync(right, png(b, [245, 125, 20], false), { mode: 0o600 });
const originalHash = hash(fs.readFileSync(left));
fs.writeFileSync(path.join(work, 'patch-check.txt'), 'before\n', { mode: 0o600 });
fs.writeFileSync(counter, JSON.stringify({ requests: 0, attempts: 0, results: [] }), { mode: 0o600 });

const wrapper = path.join(root, 'bounded-images.mjs');
const referenceModule = path.join(path.dirname(imageModule), 'image-references.mjs');
fs.writeFileSync(wrapper, `import fs from 'node:fs';import {createHash} from 'node:crypto';\nimport {createImageTool,runStdio} from ${JSON.stringify(pathToFileURL(imageModule).href)};\nimport {createReferenceStore} from ${JSON.stringify(pathToFileURL(referenceModule).href)};\nconst file=${JSON.stringify(counter)},original=${JSON.stringify(originalHash)},real=createImageTool(),refs=createReferenceStore();\nrunStdio({generate:async(args,signal)=>{let s=JSON.parse(fs.readFileSync(file,'utf8'));s.attempts++;fs.writeFileSync(file,JSON.stringify(s),{mode:0o600});if(s.requests>=2)throw Error('two-edit cap');if(args.reference_ids?.length!==1)throw Error('one prepared reference required');const id=args.reference_ids[0];if(s.requests===0){const data=Buffer.from(refs.read(id).split(',')[1],'base64');if(createHash('sha256').update(data).digest('hex')!==original)throw Error('only the synthetic original is allowed')}else if(id!==s.results[0]?.reference_id)throw Error('second edit must reference first result');s.requests++;fs.writeFileSync(file,JSON.stringify(s),{mode:0o600});const result=await real(args,signal);s.results.push(result);fs.writeFileSync(file,JSON.stringify(s),{mode:0o600});return result;}});\n`, { mode: 0o600 });
const baseConfig = `model="gemini-3.8-flash-tiered"\nmodel_provider="clirelay-antigravity"\nmodel_reasoning_effort="medium"\n[features]\nmulti_agent=false\n${provider}\n[mcp_servers.clirelay_images]\ncommand=${JSON.stringify(process.execPath)}\nargs=${JSON.stringify([wrapper])}\nstartup_timeout_sec=15\ntool_timeout_sec=330\n[mcp_servers.clirelay_images.tools.imagegen]\napproval_mode="approve"\n`;
const searchConfig = section('mcp_servers.clirelay_search') + '\n[mcp_servers.clirelay_search.tools.web_search]\napproval_mode="approve"\n';
fs.writeFileSync(config, baseConfig + searchConfig, { mode: 0o600 });
const environment = { ...process.env, CODEX_HOME: home };
for (const name of ['OPENAI_API_KEY', 'GOOGLE_API_KEY', 'GEMINI_API_KEY']) delete environment[name];
let stage = 'start';
const proofs = [];
const note = value => { proofs.push(value); console.log(JSON.stringify(value)); };

async function run(name, prompt, { images = [], resume, timeout = 180000 } = {}) {
  stage = name;
  const args = ['exec', ...(resume ? ['resume', resume] : []), '--skip-git-repo-check', ...(!resume ? ['--sandbox', 'workspace-write'] : []), '--json'];
  for (const image of images) args.push('--image', image);
  args.push('--', '-');
  const child = spawn(cli, args, { cwd: work, env: environment, stdio: ['pipe', 'pipe', 'pipe'] });
  let stdout = '', stderr = '', expired = false;
  child.stdout.on('data', value => { stdout += value; }); child.stderr.on('data', value => { stderr += value; }); child.stdin.end(prompt);
  const timer = setTimeout(() => { expired = true; child.kill('SIGTERM'); }, timeout);
  const exit = await new Promise(resolve => { child.once('exit', resolve); child.once('error', () => resolve(-1)); }); clearTimeout(timer);
  if (stdout.includes(key) || stderr.includes(key)) throw Error('credential diagnostic suppressed');
  fs.writeFileSync(path.join(root, `${name}.jsonl`), stdout, { mode: 0o600 });
  const events = stdout.split('\n').flatMap(line => { try { return [JSON.parse(line)]; } catch { return []; } });
  const text = events.filter(e => e.item?.type === 'agent_message').map(e => e.item.text ?? '').join('\n');
  const items = events.filter(e => e.type === 'item.completed').map(e => e.item);
  if (exit !== 0 || expired) throw Error(`client turn failed at ${name}`);
  return { text, items, events, thread: events.find(e => e.type === 'thread.started')?.thread_id };
}
const assert = (condition, message) => { if (!condition) throw Error(message); };
const noTools = result => result.items.every(item => ['agent_message', 'reasoning'].includes(item.type));
async function cooldown() { note({ stage: 'rate-limit-cooldown', seconds: 60 }); await new Promise(resolve => setTimeout(resolve, 60000)); }

try {
  const first = await run('ocr', 'Прочитай четырёхсимвольный код на приложенной картинке. Ответь только кодом. Не используй инструменты, команды, OCR-программы или файлы: смотри на само изображение.', { images: [left] });
  assert(first.text.includes(a) && noTools(first), 'native vision OCR failed or used a tool shortcut');
  note({ stage: 'ocr', passed: true, tool_shortcuts: false });
  const comparison = await run('comparison', 'Сравни две приложенные картинки: назови код и цвет фигуры на каждой по порядку. Никаких инструментов и команд.', { images: [left, right] });
  assert(comparison.text.includes(a) && comparison.text.includes(b) && noTools(comparison), 'two-image comparison failed');
  note({ stage: 'two-images', passed: true });
  assert(comparison.thread, 'comparison thread missing');
  const follow = await run('vision-followup', 'Какой код был на первой картинке? Ответь кодом, без инструментов.', { resume: comparison.thread });
  assert(follow.text.includes(a) && noTools(follow), 'image history replay failed');
  note({ stage: 'image-history', passed: true });
  const patch = await run('patch', 'В файле patch-check.txt замени before на after штатным apply_patch. Меняй только этот файл. После успешной правки ответь PATCH_OK.');
  assert(fs.readFileSync(path.join(work, 'patch-check.txt'), 'utf8').trim() === 'after' && patch.items.some(x => x.type === 'file_change'), 'native patch regression failed');
  note({ stage: 'patch', passed: true });
  await cooldown();
  const edit1 = await run('edit-original', `Отредактируй изображение ${left}: замени белый фон на красный, сохрани код и синюю фигуру. Используй подключённый imagegen с референсом, подготовленным его штатным helper через обычный инструмент исполнения. Не пересохраняй и не меняй оригинал. Покажи точный Markdown результата. Ровно одна генерация, никаких повторов и других провайдеров.`, { timeout: 420000 });
  let state = JSON.parse(fs.readFileSync(counter, 'utf8'));
  assert(state.requests === 1 && state.results.length === 1 && fs.existsSync(state.results[0].path) && edit1.text.includes(state.results[0].path), 'first reference edit did not produce/display a real file');
  note({ stage: 'edit-original', passed: true, image_api_calls: state.requests, artifact: state.results[0].path });
  assert(edit1.thread, 'edit thread missing');
  const edit2 = await run('edit-previous-result', 'Теперь в предыдущем результате замени красный фон на зелёный, сохрани остальные элементы. Передай imagegen reference_id предыдущего результата. Не подготавливай заново файл и не создавай изображение с нуля. Ровно одна правка, без повторов. Покажи точный Markdown результата.', { resume: edit1.thread, timeout: 420000 });
  state = JSON.parse(fs.readFileSync(counter, 'utf8'));
  assert(state.requests === 2 && state.results.length === 2 && fs.existsSync(state.results[1].path) && edit2.text.includes(state.results[1].path), 'second edit did not reuse the first result');
  assert(hash(fs.readFileSync(left)) === originalHash && state.results[0].path !== state.results[1].path, 'original or prior output was overwritten');
  note({ stage: 'edit-previous-result', passed: true, image_api_calls: state.requests, original_unchanged: true, artifact: state.results[1].path });
  await cooldown();
  const searched = await run('search', 'Найди в интернете официальную документацию OpenAI по Responses API. Используй подключённый поиск, без shell/curl. Верни две найденные ссылки, без пересказа непрочитанных страниц.');
  const calls = searched.items.filter(x => x.type === 'mcp_tool_call' && String(x.tool ?? x.name ?? '').includes('web_search'));
  assert(calls.length > 0, 'separate search tool was not called');
  note({ stage: 'separate-search', tool_called: true, links_in_answer: /https:\/\//.test(searched.text), browser_source_reading_still_requires_gui: true });
  fs.writeFileSync(path.join(root, 'proofs.json'), JSON.stringify({ passed: true, proofs, image_api_calls: state.requests, browser_gui: 'pending' }, null, 2), { mode: 0o600 });
  console.log(JSON.stringify({ completed: true, image_api_calls: state.requests, test_directory: root, browser_gui: 'pending' }));
} catch (error) {
  const state = JSON.parse(fs.readFileSync(counter, 'utf8'));
  const failure = { completed: false, stage, diagnostic: error.message, image_api_calls: state.requests, test_directory: root };
  fs.writeFileSync(path.join(root, 'proofs.json'), JSON.stringify({ ...failure, proofs }, null, 2), { mode: 0o600 });
  console.log(JSON.stringify(failure)); process.exitCode = 1;
} finally { fs.unlinkSync(config); }
