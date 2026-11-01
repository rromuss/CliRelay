// Local stdio MCP tool for the narrowly published CliRelay image operation.
// No listener, dependencies, secret copies, arbitrary URLs or output paths.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { randomUUID } from 'node:crypto';
import { pathToFileURL } from 'node:url';

const BASE_URL = 'https://cliprelay.romus.pro:8443/v1';
const IMAGE_MODEL = 'gemini-3.1-flash-image';
const MAX_IMAGE_BYTES = 20 * 1024 * 1024;
const MAX_BASE64_BYTES = 4 * Math.ceil(MAX_IMAGE_BYTES / 3);
const MAX_RESPONSE_BYTES = MAX_BASE64_BYTES + 65536;
const RATIOS = ['1:1', '16:9', '9:16'];
const TOOL = {
  name: 'imagegen',
  title: 'Nano Banana 2 image generation',
  description: 'Generate a real raster image from a text prompt using Gemini / Nano Banana 2 through the configured CliRelay account. Use for requests to draw or generate pictures. The existing CliRelay credential is used automatically; no OpenAI API key is needed. One 1K image per call. Returns a saved local image and Markdown to display it. Do not substitute SVG drawing for this tool.',
  inputSchema: { type: 'object', properties: { prompt: { type: 'string', minLength: 1, maxLength: 16000, description: 'Describe the image to generate.' }, aspect_ratio: { type: 'string', enum: RATIOS, description: 'Default 1:1; use 16:9 for landscape or 9:16 for portrait.' } }, required: ['prompt'], additionalProperties: false },
  annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: true },
};

class ToolError extends Error {}

function readCredential(configPath) {
  let fd;
  try {
    fd = fs.openSync(configPath, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.uid !== process.getuid() || (stat.mode & 0o077) || stat.size > 1024 * 1024) throw new Error();
    const config = fs.readFileSync(fd, 'utf8');
    let active = false, key, base;
    for (const line of config.split('\n')) {
      const section = line.match(/^\s*\[([^\]]+)\]\s*(?:#.*)?$/);
      if (section) { active = section[1] === 'model_providers.clirelay-antigravity'; continue; }
      if (!active) continue;
      const field = line.match(/^\s*(experimental_bearer_token|base_url)\s*=\s*("(?:[^"\\]|\\.)*")\s*(?:#.*)?$/);
      if (!field) continue;
      if (field[1] === 'base_url') { if (base !== undefined) throw new Error(); base = JSON.parse(field[2]); }
      else { if (key !== undefined) throw new Error(); key = JSON.parse(field[2]); }
    }
    if (base !== BASE_URL || typeof key !== 'string' || !/^sk-[A-Za-z0-9_-]{40,100}$/.test(key)) throw new Error();
    return key;
  } catch {
    throw new ToolError('Не удалось прочитать ключ CliRelay из защищённого клиентского конфига (0600).');
  } finally { if (fd !== undefined) fs.closeSync(fd); }
}

function validateArguments(args) {
  if (!args || typeof args !== 'object' || Array.isArray(args) || Object.keys(args).some(key => !['prompt', 'aspect_ratio'].includes(key))) throw new ToolError('Неверные параметры генерации изображения.');
  if (typeof args.prompt !== 'string' || !args.prompt.trim() || Buffer.byteLength(args.prompt) > 16000) throw new ToolError('Нужен текстовый запрос длиной до 16000 байт.');
  const ratio = args.aspect_ratio ?? '1:1';
  if (!RATIOS.includes(ratio)) throw new ToolError('Допустимые пропорции: 1:1, 16:9 и 9:16.');
  return { model: IMAGE_MODEL, prompt: args.prompt, aspect_ratio: ratio, n: 1, response_format: 'b64_json' };
}

function decodeImage(payload) {
  if (!Array.isArray(payload?.data) || payload.data.length !== 1) throw new ToolError('Сервис не вернул одно готовое изображение.');
  const { b64_json: encoded, mime_type: mime } = payload.data[0] ?? {};
  const extension = mime === 'image/png' ? 'png' : mime === 'image/jpeg' ? 'jpg' : mime === 'image/webp' ? 'webp' : undefined;
  if (!extension || typeof encoded !== 'string' || !encoded.length || encoded.length > MAX_BASE64_BYTES || encoded.length % 4 || !/^[A-Za-z0-9+/]+={0,2}$/.test(encoded)) throw new ToolError('Сервис вернул неподдерживаемые данные изображения.');
  const bytes = Buffer.from(encoded, 'base64');
  const signature = mime === 'image/png' ? bytes.subarray(0, 8).equals(Buffer.from('\x89PNG\r\n\x1a\n', 'latin1')) : mime === 'image/jpeg' ? bytes.subarray(0, 3).equals(Buffer.from([0xff, 0xd8, 0xff])) : bytes.length >= 12 && bytes.subarray(0, 4).toString() === 'RIFF' && bytes.subarray(8, 12).toString() === 'WEBP';
  if (!signature || bytes.length > MAX_IMAGE_BYTES || bytes.toString('base64') !== encoded) throw new ToolError('Изображение повреждено или превышает допустимый размер.');
  return { bytes, mime, extension };
}

export function createImageTool({ configPath = path.join(os.homedir(), '.codex/config.toml'), outputDirectory = path.join(os.homedir(), 'Pictures/CliRelay'), fetchImpl = fetch } = {}) {
  return async (args, signal) => {
    const request = validateArguments(args);
    const credential = readCredential(configPath);
    const limit = new AbortController();
    const combined = AbortSignal.any([limit.signal, AbortSignal.timeout(310000), ...(signal ? [signal] : [])]);
    try {
      const response = await fetchImpl(`${BASE_URL}/images/generations`, { method: 'POST', headers: { Authorization: `Bearer ${credential}`, 'Content-Type': 'application/json', Accept: 'application/json' }, body: JSON.stringify(request), redirect: 'error', signal: combined });
      if (!response.ok) {
        await response.body?.cancel();
        throw new ToolError(response.status === 429 ? 'Достигнут лимит CliRelay или квота генерации. Повтори запрос позже.' : `Генерация изображения отклонена сервисом (HTTP ${response.status}).`);
      }
      if (Number(response.headers.get('content-length')) > MAX_RESPONSE_BYTES) throw new ToolError('Ответ с изображением слишком большой.');
      const chunks = []; let size = 0;
      for await (const chunk of response.body) {
        size += chunk.length;
        if (size > MAX_RESPONSE_BYTES) throw new ToolError('Ответ с изображением слишком большой.');
        chunks.push(chunk);
      }
      let payload;
      try { payload = JSON.parse(Buffer.concat(chunks).toString('utf8')); } catch { throw new ToolError('Сервис вернул неверный ответ изображения.'); }
      const image = decodeImage(payload);
      combined.throwIfAborted();
      fs.mkdirSync(outputDirectory, { recursive: true, mode: 0o700 });
      const directory = fs.lstatSync(outputDirectory);
      if (!directory.isDirectory() || directory.isSymbolicLink() || directory.uid !== process.getuid()) throw new ToolError('Каталог изображений недоступен.');
      fs.chmodSync(outputDirectory, 0o700);
      const filename = path.join(outputDirectory, `${Date.now()}-${randomUUID()}.${image.extension}`);
      const fd = fs.openSync(filename, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600);
      try { fs.writeFileSync(fd, image.bytes); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
      return { path: filename, mime_type: image.mime, bytes: image.bytes.length, model: IMAGE_MODEL, markdown: `![Сгенерированное изображение](<${filename}>)` };
    } catch (error) {
      if (error instanceof ToolError) throw error;
      throw new ToolError(combined.aborted ? 'Генерация изображения отменена или превысила время ожидания.' : 'Не удалось получить или сохранить изображение CliRelay.');
    } finally { limit.abort(); }
  };
}

export function runStdio({ input = process.stdin, output = process.stdout, generate = createImageTool() } = {}) {
  const active = new Map(); let initialized = false, buffer = '';
  const send = message => output.write(`${JSON.stringify(message)}\n`);
  const reply = (id, result) => send({ jsonrpc: '2.0', id, result });
  const reject = (id, code, message) => send({ jsonrpc: '2.0', id, error: { code, message } });
  async function dispatch(message) {
    const id = message?.id;
    if (!message || message.jsonrpc !== '2.0' || typeof message.method !== 'string') return reject(id ?? null, -32600, 'Invalid request');
    if (!Object.hasOwn(message, 'id')) {
      if (message.method === 'notifications/cancelled') active.get(message.params?.requestId)?.abort();
      return;
    }
    if (!['string', 'number'].includes(typeof id)) return reject(null, -32600, 'Invalid request id');
    if (message.method === 'initialize') {
      initialized = true;
      const versions = ['2024-11-05', '2025-03-26', '2025-06-18', '2025-11-25'];
      const version = versions.includes(message.params?.protocolVersion) ? message.params.protocolVersion : '2025-11-25';
      return reply(id, { protocolVersion: version, capabilities: { tools: {} }, serverInfo: { name: 'clirelay-images', version: '1.0.0' }, instructions: 'Use imagegen for image generation. It uses CliRelay / Nano Banana 2, saves the raster image, and returns Markdown to display it. No OpenAI key is needed.' });
    }
    if (message.method === 'ping') return reply(id, {});
    if (message.method === 'server/discover') return reject(id, -32601, 'Method not found');
    if (!initialized) return reject(id, -32002, 'Server not initialized');
    if (message.method === 'tools/list') return reply(id, { tools: [TOOL] });
    if (message.method !== 'tools/call') return reject(id, -32601, 'Method not found');
    if (message.params?.name !== 'imagegen') return reject(id, -32602, 'Unknown tool');
    if (active.size) return reply(id, { content: [{ type: 'text', text: 'Уже выполняется генерация изображения. Дождись её завершения.' }], isError: true });
    const controller = new AbortController(); active.set(id, controller);
    try {
      const image = await generate(message.params.arguments, controller.signal);
      reply(id, { content: [{ type: 'text', text: `Изображение готово. Покажи его пользователю с помощью Markdown:\n${image.markdown}` }], structuredContent: image, isError: false });
    } catch (error) {
      reply(id, { content: [{ type: 'text', text: error instanceof ToolError ? error.message : 'Не удалось сгенерировать изображение CliRelay.' }], isError: true });
    } finally { active.delete(id); }
  }
  input.setEncoding('utf8');
  input.on('data', chunk => {
    buffer += chunk;
    if (Buffer.byteLength(buffer) > 128 * 1024) { buffer = ''; reject(null, -32600, 'Request too large'); return; }
    let newline;
    while ((newline = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, newline); buffer = buffer.slice(newline + 1);
      if (!line.trim()) continue;
      let message;
      try { message = JSON.parse(line); } catch { reject(null, -32700, 'Parse error'); continue; }
      void dispatch(message);
    }
  });
  input.on('end', () => { for (const controller of active.values()) controller.abort(); });
  return { abort: () => { for (const controller of active.values()) controller.abort(); } };
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const server = runStdio();
  process.on('SIGTERM', () => { server.abort(); process.stdin.destroy(); });
}
