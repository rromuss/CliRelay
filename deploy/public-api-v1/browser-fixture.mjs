// Temporary, loopback-only browser acceptance page. No files, auth or external
// requests are exposed. Run on the client during the separately approved QA.
import http from 'node:http';
import { randomUUID } from 'node:crypto';

const marker = randomUUID().slice(0, 8);
const top = `<meta charset="utf-8"><title>CliRelay browser check</title><style>body{font:20px system-ui;max-width:760px;margin:48px auto;padding:24px}button,input,a{font:inherit;padding:12px;margin:8px 0}section{margin-bottom:32px}</style>`;
const pages = {
  '/': `${top}<h1>Проверка встроенного браузера</h1><p>Код страницы: <strong>${marker}</strong></p><label for="name">Контрольный текст</label><br><input id="name" maxlength="32"><br><button id="confirm">Подтвердить</button><p id="result" role="status">Ожидаю ввод</p><p><a href="/next">Перейти на второй экран</a></p><script>document.querySelector('#confirm').addEventListener('click',()=>{document.querySelector('#result').textContent='Принято: '+document.querySelector('#name').value})</script>`,
  '/next': `${top}<h1>Второй экран</h1><p>Код страницы: <strong>${marker}</strong></p><p>Прокрути вниз и нажми нижнюю кнопку.</p><div style="height:1800px;background:linear-gradient(white,#e8eef8)"></div><button id="finish">Завершить проверку</button><p id="result" role="status">Проверка не завершена</p><script>document.querySelector('#finish').addEventListener('click',()=>{document.querySelector('#result').textContent='Браузер работает: ${marker}'})</script>`,
};
const server = http.createServer((request, response) => {
  const body = request.method === 'GET' ? pages[request.url] : null;
  response.writeHead(body ? 200 : 404, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store', 'Content-Security-Policy': "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'" });
  response.end(body ?? 'Not found');
});
server.listen(0, '127.0.0.1', () => console.log(JSON.stringify({ url: `http://127.0.0.1:${server.address().port}/`, expires_in_seconds: 1800, marker })));
const timeout = setTimeout(() => server.close(), 1800000); timeout.unref();
process.on('SIGTERM', () => server.close());
