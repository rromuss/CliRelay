// Run through Codex's ordinary execution tool so source access is sandboxed.
import { createReferenceStore } from './image-references.mjs';
try {
  if (process.argv.length !== 3) throw new Error();
  console.log(JSON.stringify(createReferenceStore().prepare(process.argv[2])));
} catch {
  console.error('Не удалось подготовить референс. Проверь разрешение Codex на чтение исходного PNG/JPEG/WebP и размер до 8 MiB.');
  process.exitCode = 1;
}
