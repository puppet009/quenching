import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import ts from 'typescript';

// Use the real Axios transport with a short ordinary-request deadline. Compile
// only this TS module; no browser or workspace-wide build is required.
const require = createRequire(import.meta.url);
const axiosURL = pathToFileURL(require.resolve('axios')).href;
const sharedURL = 'data:text/javascript;base64,' + Buffer.from(`
  import axios from ${JSON.stringify(axiosURL)};
  export const http = axios.create({ timeout: 20 });
  http.interceptors.response.use(response => response.data);
  export const tokenStore = { get: () => null };
`).toString('base64');
const source = await readFile(new URL('../src/services/sandbox.ts', import.meta.url), 'utf8');
const output = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText.replaceAll('"@quenching/shared"', JSON.stringify(sharedURL));
const service = await import('data:text/javascript;base64,' + Buffer.from(output).toString('base64'));
const { http } = await import(sharedURL);

test('sandbox deployment may outlast the ordinary HTTP deadline', async () => {
  const received = [];
  const server = createServer(async (req, res) => {
    let body = '';
    for await (const chunk of req) body += chunk;
    received.push({ method: req.method, url: req.url, body });
    setTimeout(() => {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ session_id: 42, status: 'ready' }));
    }, 80);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  http.defaults.baseURL = `http://127.0.0.1:${server.address().port}`;
  try {
    const payload = { session_id: 42, project_id: 0, template_slug: 'mall' };
    const result = await service.createSandbox(payload);
    assert.equal(result.status, 'ready');
    assert.deepEqual(JSON.parse(received[0].body), payload);
    assert.equal(received[0].method, 'POST');
    assert.equal(received[0].url, '/sandboxes');
    // Slow ordinary reads still fail; the shared deadline must not be widened.
    await assert.rejects(service.fetchSandboxProgress(42), error => error.code === 'ECONNABORTED');
    assert.equal(http.defaults.timeout, 20);
  } finally {
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
});
