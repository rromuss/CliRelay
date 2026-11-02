import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { PassThrough } from 'node:stream';
import { createImageTool, runStdio } from './clirelay-images.mjs';

const key = `sk-${'f'.repeat(48)}`;
const fixture = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aGWkAAAAASUVORK5CYII=', 'base64');
function setup(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'clirelay-image-unit-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const configPath = path.join(root, 'config.toml'), outputDirectory = path.join(root, 'images');
  fs.writeFileSync(configPath, `[model_providers.clirelay-antigravity]\nbase_url = "https://cliprelay.romus.pro:8443/v1"\nexperimental_bearer_token = "${key}"\n`, { mode: 0o600 });
  return { root, configPath, outputDirectory, referenceRoot: path.join(root, 'references') };
}
function imageResponse(extra = {}) {
  return new Response(JSON.stringify({ data: [{ b64_json: fixture.toString('base64'), mime_type: 'image/png', ...extra }] }), { status: 200 });
}

test('image tool uses one fixed endpoint and saves a private raster artifact', async t => {
  const options = setup(t); let calls = 0;
  const generate = createImageTool({ ...options, fetchImpl: async (url, init) => {
    calls++;
    assert.equal(url, 'https://cliprelay.romus.pro:8443/v1/images/generations');
    assert.equal(init.headers.Authorization, `Bearer ${key}`);
    assert.equal(init.redirect, 'error');
    const request = JSON.parse(init.body);
    assert.equal(request.model, 'gemini-3.1-flash-image'); assert.equal(request.n, 1); assert.equal(request.aspect_ratio, '9:16');
    return imageResponse();
  } });
  const result = await generate({ prompt: 'fixture strawberry', aspect_ratio: '9:16' });
  assert.equal(calls, 1); assert.ok(result.path.startsWith(`${options.outputDirectory}${path.sep}`));
  assert.deepEqual(fs.readFileSync(result.path), fixture);
  assert.equal(fs.statSync(result.path).mode & 0o777, 0o600);
  assert.equal(fs.statSync(options.outputDirectory).mode & 0o777, 0o700);
  assert.ok(result.markdown.includes(result.path)); assert.ok(!JSON.stringify(result).includes(key));
});

test('invalid parameters and unsafe credential permissions never reach the API', async t => {
  const options = setup(t); let calls = 0;
  const generate = createImageTool({ ...options, fetchImpl: async () => { calls++; return imageResponse(); } });
  for (const args of [{}, { prompt: ' ' }, { prompt: 'x', output_path: '/tmp/other' }, { prompt: 'x', model: 'other' }, { prompt: 'x', aspect_ratio: '1000:1' }, { prompt: 'x'.repeat(16001) }]) await assert.rejects(generate(args));
  fs.chmodSync(options.configPath, 0o644);
  await assert.rejects(generate({ prompt: 'x' }), /0600/);
  assert.equal(calls, 0);
});

test('unsafe image responses, URLs and symlink output directories are rejected', async t => {
  const options = setup(t);
  for (const item of [{ mime_type: 'image/svg+xml' }, { mime_type: '__proto__' }, { b64_json: 'aGVsbG8=' }, { b64_json: 'https://example.com/image.png' }]) {
    await assert.rejects(createImageTool({ ...options, fetchImpl: async () => imageResponse(item) })({ prompt: 'x' }));
  }
  const elsewhere = path.join(options.root, 'elsewhere'); fs.mkdirSync(elsewhere); fs.symlinkSync(elsewhere, options.outputDirectory);
  await assert.rejects(createImageTool({ ...options, fetchImpl: async () => imageResponse() })({ prompt: 'x' }));
  assert.deepEqual(fs.readdirSync(elsewhere), []);
});

test('errors suppress upstream secrets and do not retry; cancellation creates no image', async t => {
  const options = setup(t); let calls = 0;
  const generate = createImageTool({ ...options, fetchImpl: async () => { calls++; return new Response(key, { status: 429 }); } });
  await assert.rejects(generate({ prompt: 'x' }), error => !error.message.includes(key) && /лимит/.test(error.message));
  assert.equal(calls, 1);
  const abort = new AbortController();
  const wait = createImageTool({ ...options, fetchImpl: (_url, init) => new Promise((_resolve, reject) => { init.signal.addEventListener('abort', () => reject(new Error(key)), { once: true }); }) });
  const pending = wait({ prompt: 'x' }, abort.signal); abort.abort();
  await assert.rejects(pending, error => !error.message.includes(key) && /отменена/.test(error.message));
  assert.equal(fs.existsSync(options.outputDirectory), false);
});

test('MCP stdio framing discovers the image tool and returns text, not image history', async () => {
  const input = new PassThrough(), output = new PassThrough(); let raw = '';
  output.setEncoding('utf8'); output.on('data', chunk => { raw += chunk; });
  runStdio({ input, output, generate: async args => ({ path: '/fixture/image.png', reference_id: 'out-fixture', markdown: '![image](/fixture/image.png)', model: 'gemini-3.1-flash-image', promptNotReturned: args.prompt !== undefined }) });
  for (const message of [
    { jsonrpc: '2.0', id: 0, method: 'server/discover' },
    { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18' } },
    { jsonrpc: '2.0', method: 'notifications/initialized' },
    { jsonrpc: '2.0', id: 2, method: 'tools/list' },
    { jsonrpc: '2.0', id: 3, method: 'tools/call', params: { name: 'imagegen', arguments: { prompt: 'fixture' } } },
  ]) input.write(`${JSON.stringify(message)}\n`);
  await new Promise(resolve => setImmediate(resolve));
  const responses = raw.trim().split('\n').map(line => JSON.parse(line));
  assert.equal(responses.find(x => x.id === 0).error.code, -32601);
  assert.equal(responses.find(x => x.id === 1).result.protocolVersion, '2025-06-18');
  assert.equal(responses.find(x => x.id === 2).result.tools[0].name, 'imagegen');
  const result = responses.find(x => x.id === 3).result;
  assert.equal(result.isError, false); assert.deepEqual(result.content.map(x => x.type), ['text']);
  assert.ok(result.content[0].text.includes('/fixture/image.png')); assert.ok(!raw.includes(key));
  assert.ok(result.content[0].text.includes('out-fixture'), 'reference ID must survive clients that project only text content');
  input.end();
});
