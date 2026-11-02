// This store accepts opaque IDs, never model-supplied source paths. Preparing a
// user's source is a separate CLI operation under Codex's normal file sandbox.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash, randomUUID } from 'node:crypto';

const TTL = 24 * 60 * 60 * 1000;
const MAX_BYTES = 8 * 1024 * 1024;
const ID = /^(ref|out)_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const OUTPUT = /^\d+-[0-9a-f-]{36}\.(png|jpg|webp)$/;
const digest = data => createHash('sha256').update(data).digest('hex');

export function rasterMime(data) {
  if (data.subarray(0, 8).equals(Buffer.from('\x89PNG\r\n\x1a\n', 'latin1'))) return 'image/png';
  if (data.subarray(0, 3).equals(Buffer.from([0xff, 0xd8, 0xff]))) return 'image/jpeg';
  if (data.length >= 12 && data.subarray(0, 4).toString() === 'RIFF' && data.subarray(8, 12).toString() === 'WEBP') return 'image/webp';
  throw new Error('Поддерживаются только PNG, JPEG и WebP.');
}

export function createReferenceStore({ root = `/private/tmp/clirelay-image-inputs-${process.getuid()}`, outputDirectory = path.join(os.homedir(), 'Pictures/CliRelay'), now = Date.now } = {}) {
  function directory() {
    if (!fs.existsSync(root)) fs.mkdirSync(root, { mode: 0o700 });
    const st = fs.lstatSync(root);
    if (!st.isDirectory() || st.isSymbolicLink() || st.uid !== process.getuid() || (st.mode & 0o777) !== 0o700) throw new Error('Каталог референсов должен быть собственным закрытым каталогом (0700).');
  }
  function readPrivate(filename, maxBytes) {
    const fd = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    try {
      const st = fs.fstatSync(fd);
      if (!st.isFile() || st.uid !== process.getuid() || (st.mode & 0o077) || st.size > maxBytes) throw new Error('Недоступный или слишком большой референс.');
      const data = Buffer.alloc(st.size + 1);
      const size = fs.readSync(fd, data, 0, data.length, 0);
      if (size !== st.size) throw new Error('Референс изменился при чтении.');
      return data.subarray(0, size);
    } finally { fs.closeSync(fd); }
  }
  function writePrivate(filename, value) {
    const fd = fs.openSync(filename, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600);
    try { fs.writeFileSync(fd, value); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
  }
  function metadata(id) {
    if (typeof id !== 'string' || !ID.test(id)) throw new Error('Неверный reference_id.');
    const value = JSON.parse(readPrivate(path.join(root, `${id}.json`), 4096));
    if (value.version !== 1 || value.id !== id || !Number.isFinite(value.expires) || !/^[0-9a-f]{64}$/.test(value.sha256) || !['image/png', 'image/jpeg', 'image/webp'].includes(value.mime)) throw new Error('Повреждённый референс.');
    return value;
  }
  function cleanup() {
    directory();
    let count = 0;
    for (const name of fs.readdirSync(root).slice(0, 4096)) {
      if (name.endsWith('.data') && ID.test(name.slice(0, -5)) && name.startsWith('ref_') && !fs.existsSync(path.join(root, `${name.slice(0, -5)}.json`))) {
        const orphan = fs.lstatSync(path.join(root, name));
        if (orphan.isFile() && orphan.uid === process.getuid() && !(orphan.mode & 0o077) && orphan.mtimeMs + TTL < now()) fs.unlinkSync(path.join(root, name));
        continue;
      }
      if (!name.endsWith('.json') || !ID.test(name.slice(0, -5))) continue;
      const id = name.slice(0, -5);
      let meta;
      try { meta = metadata(id); } catch { continue; }
      if (meta.expires > now()) continue;
      // Never delete the user's Pictures output. Only staged copies expire.
      if (id.startsWith('ref_')) {
        try { fs.unlinkSync(path.join(root, `${id}.data`)); } catch (e) { if (e.code !== 'ENOENT') throw e; }
      }
      fs.unlinkSync(path.join(root, name)); count++;
    }
    return count;
  }
  function prepare(source) {
    if (!path.isAbsolute(source)) throw new Error('Нужен абсолютный путь к изображению.');
    cleanup();
    // This function is invoked only by the separate sandboxed preparation CLI.
    const fd = fs.openSync(source, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    let data;
    try {
      const st = fs.fstatSync(fd);
      if (!st.isFile() || st.size < 1 || st.size > MAX_BYTES) throw new Error('Исходное изображение должно быть файлом до 8 MiB.');
      data = Buffer.alloc(st.size + 1);
      const size = fs.readSync(fd, data, 0, data.length, 0);
      if (size !== st.size) throw new Error('Исходный файл изменился при чтении.');
      data = data.subarray(0, size);
    } finally { fs.closeSync(fd); }
    const mime = rasterMime(data), id = `ref_${randomUUID()}`, expires = now() + TTL;
    writePrivate(path.join(root, `${id}.data`), data);
    try { writePrivate(path.join(root, `${id}.json`), JSON.stringify({ version: 1, id, mime, sha256: digest(data), expires })); }
    catch (e) { fs.unlinkSync(path.join(root, `${id}.data`)); throw e; }
    return { reference_id: id, mime_type: mime, bytes: data.length, expires_at: new Date(expires).toISOString() };
  }
  function read(id) {
    cleanup();
    const meta = metadata(id);
    if (meta.expires <= now()) throw new Error('Референс истёк; подготовь файл заново обычным инструментом исполнения.');
    let filename;
    if (id.startsWith('ref_')) filename = path.join(root, `${id}.data`);
    else {
      if (typeof meta.basename !== 'string' || !OUTPUT.test(meta.basename)) throw new Error('Неверный референс результата.');
      const st = fs.lstatSync(outputDirectory);
      if (!st.isDirectory() || st.isSymbolicLink() || st.uid !== process.getuid() || (st.mode & 0o077)) throw new Error('Недоступный каталог результатов.');
      filename = path.join(outputDirectory, meta.basename);
    }
    const data = readPrivate(filename, MAX_BYTES);
    if (digest(data) !== meta.sha256 || rasterMime(data) !== meta.mime) throw new Error('Референс изменился после подготовки.');
    return `data:${meta.mime};base64,${data.toString('base64')}`;
  }
  function registerOutput(filename, mime) {
    directory();
    const basename = path.basename(filename);
    if (path.dirname(filename) !== path.resolve(outputDirectory) || !OUTPUT.test(basename)) throw new Error('Результат находится вне каталога изображений.');
    const data = readPrivate(filename, 20 * 1024 * 1024);
    if (rasterMime(data) !== mime) throw new Error('Неверный формат результата.');
    const id = `out_${randomUUID()}`;
    writePrivate(path.join(root, `${id}.json`), JSON.stringify({ version: 1, id, basename, mime, sha256: digest(data), expires: now() + TTL }));
    return id;
  }
  return { prepare, read, registerOutput, cleanup };
}
