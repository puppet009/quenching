import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

async function moduleURL(path) {
  let source = await readFile(new URL(path, import.meta.url), 'utf8');
  if (path.includes('telemetry')) source = source.replace('../utils/time', await moduleURL('../src/utils/time.ts'));
  const js = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022}}).outputText;
  return 'data:text/javascript;base64,' + Buffer.from(js).toString('base64');
}
const { BrowserTelemetry } = await import(await moduleURL('../src/services/telemetry.ts'));

test('browser diagnostics strip URL credentials and have a bounded schema', () => {
  const reports = [];
  const logger = new BrowserTelemetry('web', async value => reports.push(value), () => 0, () => '/training/7?token=secret#private');
  const error = new Error('failed password=secret');
  error.stack = 'Error at https://example/app.js?token=secret#private';
  logger.capture('error', error);
  assert.equal(reports.length, 1);
  assert.equal(reports[0].path, '/training/7');
  assert.ok(!JSON.stringify(reports[0]).includes('secret'));
  assert.deepEqual(Object.keys(reports[0]).sort(), ['app', 'client_time', 'kind', 'message', 'path', 'stack']);
  logger.capture('console', 'caught terminal connection error');
  assert.equal(reports[1].kind, 'console');
});

test('repeated errors and failed transports cannot cause an unbounded reporting loop', async () => {
  let sent = 0, clock = 0;
  const logger = new BrowserTelemetry('admin', async () => { sent++; throw new Error('offline'); }, () => clock, () => '/admin');
  for (let i = 0; i < 20; i++) logger.capture('error', 'repeat');
  assert.equal(sent, 1);
  for (let i = 0; i < 20; i++) logger.capture('error', 'error ' + i);
  assert.equal(sent, 5);
  clock = 61_000;
  logger.capture('error', 'repeat');
  assert.equal(sent, 6);
  await new Promise(resolve => setImmediate(resolve));
});
