import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('../src/utils/time.ts', import.meta.url), 'utf8');
const js = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.ESNext}}).outputText;
const time = await import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));

test('calibrated request timestamps remain integer milliseconds accepted by backend filters', async () => {
  await time.syncServerTime(async () => new Response(JSON.stringify({data: {unix_ms: 1792332000000}})));
  assert.ok(Number.isInteger(time.nowMs()));
});

test('late clock calibration notifies mounted relative-time displays', async () => {
  let notifications = 0;
  const before = time.serverClockVersion();
  const unsubscribe = time.subscribeServerClock(() => notifications++);
  await time.syncServerTime(async () => new Response(JSON.stringify({data: {unix_ms: 1792332000000}})));
  assert.equal(notifications, 1);
  assert.equal(time.serverClockVersion(), before + 1);
  unsubscribe();
  await time.syncServerTime(async () => new Response(JSON.stringify({data: {unix_ms: 1792332000000}})));
  assert.equal(notifications, 1);
});

test('display follows the browser time zone across dates without shifting the instant', async () => {
  const previous = process.env.TZ;
  try {
    for (const [zone, date, clock] of [
      ['UTC', '2026-10-18', '16:02:03'],
      ['America/Los_Angeles', '2026-10-18', '09:02:03'],
      ['Asia/Shanghai', '2026-10-19', '00:02:03'],
      ['Asia/Kathmandu', '2026-10-18', '21:47:03'],
    ]) {
      process.env.TZ = zone;
      // A fresh module models a page opened by a user in this browser time zone.
      const local = await import('data:text/javascript;base64,' + Buffer.from(js).toString('base64') + '#' + encodeURIComponent(zone));
      const at = Date.parse('2026-10-18T16:02:03.123Z');
      assert.equal(local.formatAbsTime(at), clock, zone);
      assert.equal(local.formatAbsTimeMs(at), clock + '.123', zone);
      assert.equal(local.formatAbsDateTime(at), `${date} ${clock}`, zone);
      assert.equal(local.formatIsoDate('2026-10-19T00:02:03.123+08:00'), `${date} ${clock}`, zone);
      assert.equal(local.formatDate(at), date, zone);
      assert.equal(local.formatIsoMinute('2026-10-18T16:02:03Z'), `${date} ${clock.slice(0, 5)}`, zone);
      assert.equal(local.sessionElapsedMs('2026-10-18T16:00:03.123Z', at), 120_000, zone);
    }
  } finally {
    if (previous === undefined) delete process.env.TZ;
    else process.env.TZ = previous;
  }
});

test('log time zone labels distinguish the repeated hour at the daylight-saving transition', async () => {
  const previous = process.env.TZ;
  try {
    process.env.TZ = 'America/Los_Angeles';
    const local = await import('data:text/javascript;base64,' + Buffer.from(js).toString('base64') + '#dst');
    const before = Date.parse('2026-11-01T08:30:00Z');
    const after = Date.parse('2026-11-01T09:30:00Z');
    assert.equal(local.formatAbsDateTime(before), '2026-11-01 01:30:00');
    assert.equal(local.formatAbsDateTime(after), '2026-11-01 01:30:00');
    assert.equal(local.formatTimeZone(before), 'America/Los_Angeles · UTC-07:00');
    assert.equal(local.formatTimeZone(after), 'America/Los_Angeles · UTC-08:00');
    assert.equal(after - before, 3_600_000);
  } finally {
    if (previous === undefined) delete process.env.TZ;
    else process.env.TZ = previous;
  }
});

test('clock calibration uses server instant and monotonic elapsed time, without changing stored instants', () => {
  const clock = new time.ServerClock();
  const server = Date.parse('2026-10-18T13:00:00Z');
  assert.equal(clock.synchronize(server, 100, 200), true);
  assert.equal(clock.now(1200), server + 1050);
  assert.equal(clock.synchronize(NaN, 0, 1), false);
  assert.equal(clock.synchronize(server, 0, 6000), false);
  assert.equal(time.toRelativeMs(server + 3000, server), 3000);
  assert.equal(time.fromRelativeMs(3000, server), server + 3000);
  assert.equal(time.formatRelTime(61_000), '01:01');
  assert.equal(time.sessionElapsedMs('2026-10-18T12:58:00Z', server), 120_000);
  assert.equal(time.sessionElapsedMs(null, server), 0);
});
