import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { createReferenceStore } from './image-references.mjs';
import { createImageTool } from './clirelay-images.mjs';

const image = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aGWkAAAAASUVORK5CYII=', 'base64');
const hash = data => createHash('sha256').update(data).digest('hex');
function setup(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'clirelay-reference-test-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const referenceRoot = path.join(root, 'refs'), outputDirectory = path.join(root, 'output'), source = path.join(root, 'source.png');
  fs.writeFileSync(source, image, { mode: 0o600 });
  return { root, source, referenceRoot, outputDirectory };
}

test('sandboxed preparation copies source; opaque refs and generated outputs support sequential edits', async t => {
  const options = setup(t), store = createReferenceStore({ root: options.referenceRoot, outputDirectory: options.outputDirectory });
  const before = hash(fs.readFileSync(options.source));
  const prepared = store.prepare(options.source);
  assert.match(prepared.reference_id, /^ref_/);
  assert.equal(store.read(prepared.reference_id), `data:image/png;base64,${image.toString('base64')}`);
  assert.equal(fs.statSync(options.referenceRoot).mode & 0o777, 0o700);
  assert.equal(fs.statSync(path.join(options.referenceRoot, `${prepared.reference_id}.data`)).mode & 0o777, 0o600);
  const configPath = path.join(options.root, 'config.toml');
  fs.writeFileSync(configPath, `[model_providers.clirelay-antigravity]\nbase_url = "https://cliprelay.romus.pro:8443/v1"\nexperimental_bearer_token = "sk-${'x'.repeat(48)}"\n`, { mode: 0o600 });
  let calls = 0;
  const generate = createImageTool({ ...options, configPath, fetchImpl: async (_url, init) => {
    calls++;
    const request = JSON.parse(init.body);
    assert.deepEqual(request.reference_images, [`data:image/png;base64,${image.toString('base64')}`]);
    return new Response(JSON.stringify({ data: [{ mime_type: 'image/png', b64_json: image.toString('base64') }] }));
  } });
  const first = await generate({ prompt: 'edit one', reference_ids: [prepared.reference_id] });
  const second = await generate({ prompt: 'edit result', reference_ids: [first.reference_id] });
  assert.equal(calls, 2); assert.notEqual(first.path, second.path);
  assert.match(first.reference_id, /^out_/); assert.ok(fs.existsSync(second.path));
  assert.equal(hash(fs.readFileSync(options.source)), before);
});

test('reference IDs cannot address source paths, traversal, symlinks, stale or altered data', t => {
  const options = setup(t), store = createReferenceStore({ root: options.referenceRoot, outputDirectory: options.outputDirectory });
  const prepared = store.prepare(options.source);
  for (const id of [options.source, '../../source.png', 'file:///private/source.png', 'ref_invalid']) assert.throws(() => store.read(id));
  const staged = path.join(options.referenceRoot, `${prepared.reference_id}.data`);
  fs.unlinkSync(staged); fs.symlinkSync(options.source, staged);
  assert.throws(() => store.read(prepared.reference_id));
  fs.unlinkSync(staged); fs.writeFileSync(staged, Buffer.concat([image, Buffer.from('changed')]), { mode: 0o600 });
  assert.throws(() => store.read(prepared.reference_id));
  const link = path.join(options.root, 'source-link.png'); fs.symlinkSync(options.source, link);
  assert.throws(() => store.prepare(link));
});

test('24-hour cleanup removes staged copies but preserves generated files and originals', t => {
  const options = setup(t); let clock = 1000;
  const store = createReferenceStore({ root: options.referenceRoot, outputDirectory: options.outputDirectory, now: () => clock });
  const prepared = store.prepare(options.source);
  fs.mkdirSync(options.outputDirectory, { mode: 0o700 });
  const output = path.join(options.outputDirectory, '1000-12345678-1234-4123-8123-123456789abc.png');
  fs.writeFileSync(output, image, { mode: 0o600 });
  const result = store.registerOutput(output, 'image/png');
  clock += 24 * 60 * 60 * 1000 + 1;
  assert.equal(store.cleanup(), 2);
  assert.throws(() => store.read(prepared.reference_id)); assert.throws(() => store.read(result));
  assert.ok(fs.existsSync(options.source)); assert.ok(fs.existsSync(output));
  assert.equal(fs.readdirSync(options.referenceRoot).length, 0);
  const orphan = store.prepare(options.source).reference_id;
  fs.unlinkSync(path.join(options.referenceRoot, `${orphan}.json`));
  const staged = path.join(options.referenceRoot, `${orphan}.data`);
  fs.utimesSync(staged, 1, 1);
  store.cleanup();
  assert.equal(fs.existsSync(staged), false);
  assert.ok(fs.existsSync(options.source));
});

test('invalid references and oversized combined edits never call the provider', async t => {
  const options = setup(t), store = createReferenceStore({ root: options.referenceRoot, outputDirectory: options.outputDirectory });
  const prepared = store.prepare(options.source); let calls = 0;
  const generate = createImageTool({ ...options, fetchImpl: async () => { calls++; throw new Error('must not call'); } });
  for (const reference_ids of [[options.source], Array(15).fill(prepared.reference_id), ['ref_invalid']]) await assert.rejects(generate({ prompt: 'edit', reference_ids }));
  const large = path.join(options.root, 'large.png');
  fs.writeFileSync(large, Buffer.concat([image, Buffer.alloc(4 * 1024 * 1024)]), { mode: 0o600 });
  const ref = store.prepare(large).reference_id;
  await assert.rejects(generate({ prompt: 'edit', reference_ids: [ref, ref] }), /8 MiB/);
  assert.equal(calls, 0);
});
