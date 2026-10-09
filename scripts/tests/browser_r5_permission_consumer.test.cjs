const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const d = require('../browser_r5_permission_consumer.cjs');

test('review repair: abort-ignore fault is limited to the two late-response scenarios', () => {
  for (const name of d.SCENARIOS) assert.equal(d.ignorePermissionAbort(name), ['late_tenant', 'late_session'].includes(name));
  assert.equal(d.ignorePermissionAbort('late_unknown'), false);
});
test('review repair: late reply must be HTTP 200 and finish with null, not an Error', async () => {
  await d.assertDeliveredLateReply({ status: () => 200, finished: async () => null });
  await assert.rejects(d.assertDeliveredLateReply({ status: () => 200, finished: async () => new Error('synthetic transport failure') }), { name: 'AssertionError' });
  await assert.rejects(d.assertDeliveredLateReply({ status: () => 200, finished: async () => undefined }), { name: 'AssertionError' });
  await assert.rejects(d.assertDeliveredLateReply({ status: () => 503, finished: async () => null }), { name: 'AssertionError' });
});

test('driver import is dependency-free and scenario worklist is explicit', () => {
  assert.deepEqual(d.SCENARIOS, ['permission_presence', 'two_tab_conflict', 'late_tenant', 'late_session', 'profile_conflict', 'inspection_allowlist']);
});
test('loopback origin rejects credential, public, path/query and non-http scopes', () => {
  assert.equal(d.loopbackOrigin('http://127.0.0.1:3199'), 'http://127.0.0.1:3199');
  for (const u of ['https://127.0.0.1', 'http://public.example', 'http://u:p@localhost', 'http://localhost/api', 'http://localhost/?token=x', 'http://localhost/#x', 'file:///tmp/a']) assert.throws(() => d.loopbackOrigin(u));
});
test('CLI has no implicit service launch, environment fixture or shipping mode', () => {
  assert.throws(() => d.options([]));
  const args = ['--origin', 'http://localhost:3199', '--cold-review', '/tmp/cold.json', '--browser-module', '/tmp/playwright/test', '--receipt', '/tmp/result.json', '--server-receipt', '/tmp/server.json'];
  assert.equal(d.options(args).scenarios.length, 6);
  for (const extension of [['--mode', 'shipping'], ['--scenario', 'SSE'], ['--origin', 'http://localhost:3200']]) assert.throws(() => d.options([...args, ...extension]));
  assert.throws(() => d.options(args.slice(0, -2)));
});
test('synthetic NULL profile presence is not missing and false/zero are literal values', () => {
  const bound = d.snapshot();
  assert.equal(bound.overrides.can_send, false); assert.equal(bound.overrides.daily_send_quota, 0);
  assert.equal(bound.overrides.daily_receive_quota, null); assert.equal(bound.overrides.allowed_zone_ids, null);
  assert.equal(bound.overrides.domain_access.zone_ids, null); assert.equal(bound.overrides.domain_access.mode, 'none');
  assert.equal(bound.revision.user_revision, '9007199254740993');
  const absent = d.snapshot({ presence: 'null' });
  assert.ok(Object.hasOwn(absent, 'profile')); assert.equal(absent.profile, null);
  assert.equal(absent.revision.profile_id, null); assert.equal(absent.revision.profile_revision, null);
  assert.equal(absent.field_sources.daily_receive_quota, 'default');
  assert.equal(Object.hasOwn(d.snapshot({ presence: 'omitted' }), 'profile'), false);
});
test('fixtures are fresh values and contain only synthetic closed inspection keys', () => {
  const first = d.snapshot(); first.revision.user_revision = '21'; assert.equal(d.snapshot().revision.user_revision, '9007199254740993');
  const payload = d.inspection(); assert.deepEqual(Object.keys(payload).sort(), ['job', 'recipients']);
  assert.equal(payload.job.tenant_id, d.IDS.tenant); assert.equal(payload.job.id, d.IDS.job);
  assert.equal(payload.recipients[0].smtp_code, 550); assert.equal(payload.recipients[0].enhanced_code, '5.1.1');
  assert.equal(Object.hasOwn(payload.recipients[0], 'diagnostic'), false); assert.equal(Object.hasOwn(payload.job, 'delivery_token'), false);
});
test('safe events preserve field names but discard token, body, raw exception and PII', () => {
  const call = { method: 'PATCH', path: `/api/v1/admin/users/${d.IDS.user}/permission-editor`, tenant: d.IDS.tenant, body: { patch: { daily_send_quota: 7 }, secret: 'DO_NOT_CAPTURE' }, authorization: 'DO_NOT_CAPTURE', raw_response: 'DO_NOT_CAPTURE' };
  const safe = d.safeCall(call, 409); assert.deepEqual(safe.field_names, ['daily_send_quota']);
  assert.equal(JSON.stringify(safe).includes('DO_NOT_CAPTURE'), false); assert.equal(Object.hasOwn(safe, 'body'), false);
  const receipt = d.receipt({ build_id: 'synthetic' }, 'http://localhost:3199');
  assert.equal(receipt.shipping_Go_PG_SMTP_Docker_passed, false); assert.equal(receipt.services_started_by_driver, false);
  assert.ok(receipt.excluded.includes('complete_P9_130')); assert.match(receipt.auth_boundary, /no login or authz proof/);
});
test('artifact qualification requires cold production output and reports bounded source drift', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'r5-browser-driver-pure-'));
  try {
    const source = path.join(root, 'source'), work = path.join(root, 'work');
    for (const file of d.INPUTS) for (const base of [source, work]) { fs.mkdirSync(path.dirname(path.join(base, file)), { recursive: true }); fs.writeFileSync(path.join(base, file), 'synthetic-input'); }
    fs.mkdirSync(path.join(source, 'web/.next/standalone'), { recursive: true }); fs.mkdirSync(path.join(source, 'web/.next/static'));
    fs.writeFileSync(path.join(source, 'web/.next/standalone/server.js'), '// synthetic test only'); fs.writeFileSync(path.join(source, 'web/.next/BUILD_ID'), 'test-build-id');
    const review = { npm_build_exit: 0, npm_ci_exit: 0, cold_before_install: true, source_identity_kind: 'tracked_web_archive_sha256_and_explicit_17_dirty_delta_receipt', source_identity_sha256: 'a'.repeat(64), source_root: source, tracked_HEAD: 'test-only' };
    const reviewPath = path.join(root, 'review.json'); fs.writeFileSync(reviewPath, JSON.stringify(review));
    assert.deepEqual(d.artifact(reviewPath, work).source_drift, []);
    fs.writeFileSync(path.join(work, d.INPUTS[0]), 'changed'); assert.deepEqual(d.artifact(reviewPath, work).source_drift, [d.INPUTS[0]]);
    review.npm_build_exit = 1; fs.writeFileSync(reviewPath, JSON.stringify(review)); assert.throws(() => d.artifact(reviewPath, work));
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
