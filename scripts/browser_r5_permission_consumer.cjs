#!/usr/bin/env node
// Real production-rendered UI, synthetic wire faults only. This is NOT the
// Go+PG+SMTP+Docker shipping journey and never starts/builds those services.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const IDS = Object.freeze({ tenant: '10000000-0000-4000-8000-000000000001', otherTenant: '10000000-0000-4000-8000-000000000002', user: '20000000-0000-4000-8000-000000000001', otherUser: '20000000-0000-4000-8000-000000000002', admin: '20000000-0000-4000-8000-000000000003', profile: '40000000-0000-4000-8000-000000000001', job: '50000000-0000-4000-8000-000000000001' });
const DATE = '2026-10-01T00:00:00Z';
const SCENARIOS = ['permission_presence', 'two_tab_conflict', 'late_tenant', 'late_session', 'profile_conflict', 'inspection_allowlist'];
const INPUTS = ['web/features/company/user-management.tsx', 'web/features/company/profile-management.tsx', 'web/app/(dashboard)/company/recovery/page.tsx', 'web/lib/api/permissions.ts', 'web/lib/api/permission-editor-types.ts', 'web/lib/outbound-inspection-types.ts', 'web/lib/session.ts', 'web/contexts/auth-context.tsx', 'web/locales/en.json'];
function loopbackOrigin(raw) {
  const u = new URL(raw);
  if (u.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(u.hostname) || u.username || u.password || u.pathname !== '/' || u.search || u.hash) throw new Error('Loopback-only origin required');
  return u.origin;
}
function options(argv) {
  const result = {};
  const allowed = ['origin', 'cold-review', 'browser-module', 'chromium', 'receipt', 'scenario', 'server-receipt'];
  for (let i = 0; i < argv.length; i += 2) {
    if (!argv[i]?.startsWith('--') || !allowed.includes(argv[i].slice(2)) || !argv[i + 1] || argv[i + 1].startsWith('--') || Object.hasOwn(result, argv[i].slice(2))) throw new Error('Invalid or duplicate option');
    result[argv[i].slice(2)] = argv[i + 1];
  }
  if (!result.origin || !result['cold-review'] || !result['browser-module'] || !result.receipt || !result['server-receipt']) throw new Error('Required: --origin --cold-review --browser-module --receipt --server-receipt');
  result.origin = loopbackOrigin(result.origin);
  result.scenarios = result.scenario ? [result.scenario] : [...SCENARIOS];
  if (result.scenarios.some(x => !SCENARIOS.includes(x))) throw new Error('Unknown scenario');
  return result;
}
function artifact(reviewPath, workspace = path.resolve(__dirname, '..')) {
  const review = JSON.parse(fs.readFileSync(reviewPath, 'utf8'));
  assert.equal(review.npm_build_exit, 0);
  assert.equal(review.npm_ci_exit, 0);
  assert.equal(review.cold_before_install, true);
  assert.equal(review.source_identity_kind, 'tracked_web_archive_sha256_and_explicit_17_dirty_delta_receipt');
  assert.match(review.source_identity_sha256, /^[0-9a-f]{64}$/);
  const source = fs.realpathSync(review.source_root);
  const buildID = fs.readFileSync(path.join(source, 'web/.next/BUILD_ID'), 'utf8').trim();
  assert.match(buildID, /^[A-Za-z0-9_-]{1,100}$/);
  assert.ok(fs.statSync(path.join(source, 'web/.next/standalone/server.js')).isFile());
  assert.ok(fs.statSync(path.join(source, 'web/.next/static')).isDirectory());
  const drift = INPUTS.filter(file => !fs.readFileSync(path.join(source, file)).equals(fs.readFileSync(path.join(workspace, file))));
  return { source_root: source, source_identity_sha256: review.source_identity_sha256, tracked_HEAD: review.tracked_HEAD, build_id: buildID, observed_input_count: INPUTS.length, source_drift: drift };
}
function effective() { return { can_send: false, daily_send_quota: 0, daily_receive_quota: 100, max_mailboxes: 2, max_domains: 3, allowed_zone_ids: [], domain_access_mode: 'none', can_create_domains: false, can_create_routes: false, can_create_api_keys: false }; }
function profile(tenant = IDS.tenant) { return { ...effective(), id: IDS.profile, tenant_id: tenant, name: 'Synthetic restricted profile', description: 'Synthetic profile description', revision: '9007199254740995', is_system: false, created_at: DATE, updated_at: DATE }; }
function snapshot({ tenant = IDS.tenant, user = IDS.user, revision = '9007199254740993', presence = 'bound' } = {}) {
  const p = presence === 'null' ? null : profile(tenant);
  const s = { user_id: user, tenant_id: tenant, profile: p, overrides: { can_send: false, daily_send_quota: 0, daily_receive_quota: null, max_mailboxes: null, max_domains: null, allowed_zone_ids: null, can_create_domains: null, can_create_routes: null, can_create_api_keys: null, domain_access: { mode: 'none', zone_ids: null } }, effective: effective(), field_sources: {}, revision: { user_id: user, tenant_id: tenant, user_revision: revision, profile_id: p?.id ?? null, profile_revision: p?.revision ?? null }, capabilities: { patch: true, assign_profile: false } };
  for (const k of ['can_send', 'daily_send_quota', 'daily_receive_quota', 'max_mailboxes', 'max_domains', 'can_create_domains', 'can_create_routes', 'can_create_api_keys', 'domain_access']) s.field_sources[k] = ['can_send', 'daily_send_quota', 'domain_access'].includes(k) ? 'override' : p ? 'profile' : 'default';
  if (presence === 'omitted') delete s.profile;
  return s;
}
function inspection() { return { job: { id: IDS.job, tenant_id: IDS.tenant, state: 'sent', status: 'partially_accepted', created_at: DATE, updated_at: DATE, mail_from: 'sender@fixture.invalid', to: ['to@fixture.invalid'], cc: [], bcc: ['bcc@fixture.invalid'], subject: 'Synthetic audited subject', text_body: 'Synthetic audited body', html_body: '<img src="https://blocked.fixture.invalid/track" onerror="window.inspectionExecuted=true">', headers: { From: 'sender@fixture.invalid', 'Content-Type': 'text/plain' } }, recipients: [{ address: 'bcc@fixture.invalid', kind: 'bcc', state: 'permanent', attempts: 1, smtp_code: 550, enhanced_code: '5.1.1', diagnostic_class: 'permanent', updated_at: DATE }] }; }
function deferred() { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; }
function ignorePermissionAbort(name) { return name === 'late_tenant' || name === 'late_session'; }
async function assertDeliveredLateReply(response) {
  assert.equal(response.status(), 200, 'Late fixture reply must be HTTP 200');
  assert.equal(await response.finished(), null, 'Late fixture reply must finish without a transport error');
}
function safeCall(call, status) { return { method: call.method, path: call.path, status, tenant: call.tenant, field_names: Object.keys(call.body?.patch ?? call.body ?? {}).sort() }; }
function receipt(provenance, origin) { return { schema: 1, evidence_layer: 'real_browser_production_UI_synthetic_wire_faults', shipping_Go_PG_SMTP_Docker_passed: false, services_started_by_driver: false, auth_boundary: 'synthetic localStorage actors; real AuthProvider/session transport; no login or authz proof', network_boundary: 'allowlisted browser HTTP replies; no Go/PostgreSQL/SMTP', provenance, origin, scenarios: [], safe_events: [], excluded: ['shipping_image', 'real_API_authz_and_transactions', 'SSE_multi_instance', 'complete_P9_130', 'G0_G9'] }; }
async function run(config) {
  const provenance = artifact(config['cold-review']);
  const server = JSON.parse(fs.readFileSync(config['server-receipt'], 'utf8'));
  assert.equal(server.deployment_kind, 'native_standalone');
  assert.equal(server.origin, config.origin); assert.equal(server.build_id, provenance.build_id);
  assert.equal(fs.realpathSync(server.source_root), provenance.source_root);
  assert.equal(fs.realpathSync(server.server_entry), path.join(provenance.source_root, 'web/.next/standalone/server.js'));
  const result = receipt(provenance, config.origin);
  result.server_boundary = 'executor-owned native standalone receipt; not a Docker image';
  const { chromium, expect } = require(config['browser-module']);
  if (!expect) throw new Error('Browser module must export chromium and expect (playwright/test supported)');
  const browser = await chromium.launch({ headless: true, executablePath: config.chromium || undefined });
  try {
    for (const name of config.scenarios) {
      const entry = { name, status: 'running', step: 'isolated_context', ignores_permission_transport_abort: ignorePermissionAbort(name) }; result.scenarios.push(entry);
      const state = { current: snapshot(), profile: profile(), presence: 'bound', conflict: false, profileConflict: false, pending: null, inspection: inspection(), calls: [], violations: [] };
      const context = await browser.newContext({ serviceWorkers: 'block' });
      await context.addInitScript(({ ids, ignoreAbort }) => {
        localStorage.setItem('tabmail-locale', 'en');
        if (!localStorage.getItem('tabmail_user')) {
          localStorage.setItem('tabmail_access_token', 'synthetic-browser-fixture-not-a-JWT');
          localStorage.setItem('tabmail_user', JSON.stringify({ id: ids.admin, tenant_id: ids.tenant, email: 'admin@fixture.invalid', display_name: 'Synthetic admin', role: 'super_admin' }));
          localStorage.setItem('tabmail_tenant_id', ids.tenant);
          localStorage.setItem('tabmail_session_epoch', 'synthetic-initial-epoch');
        }
        // Fault injection deliberately ignores transport abort for late-reply
        // cases; application session/epoch fences must reject the response.
        if (ignoreAbort) {
          const fetch = window.fetch.bind(window);
          window.fetch = (url, init) => /permission-editor/.test(String(url)) ? fetch(url, { ...init, signal: undefined }) : fetch(url, init);
        }
      }, { ids: IDS, ignoreAbort: ignorePermissionAbort(name) });
      await context.route('**/*', async route => {
        const request = route.request(), url = new URL(request.url());
        if (url.origin !== config.origin) { state.violations.push('foreign-origin'); await route.abort(); return; }
        if (!url.pathname.startsWith('/api/v1/')) { await route.continue(); return; }
        const call = { path: url.pathname, method: request.method(), body: request.postData() ? request.postDataJSON() : null, tenant: request.headers()['x-tenant-id'] ?? null };
        state.calls.push(call);
        let status = 200, data;
        const editor = call.path.match(/^\/api\/v1\/admin\/users\/([^/]+)\/permission-editor$/);
        if (editor) {
          if (state.pending && call.method === 'GET') { const gate = state.pending; state.pending = null; data = await gate.promise; }
          else if (call.method === 'PATCH') {
            assert.equal(call.tenant, state.current.tenant_id);
            if (state.conflict) { status = 409; data = { error: { code: 'CONFLICT', message: 'Synthetic stale version' } }; }
            else { assert.deepEqual(call.body.expected_revision, state.current.revision); state.current = structuredClone(state.current); state.current.revision.user_revision = String(BigInt(state.current.revision.user_revision) + 1n); Object.assign(state.current.overrides, call.body.patch); Object.assign(state.current.effective, call.body.patch); data = state.current; }
          } else { data = structuredClone(state.current); if (state.presence === 'omitted') delete data.profile; }
          if (call.method === 'GET' && !data) data = state.current;
        } else if (call.path === '/api/v1/admin/users') data = [{ id: state.current.user_id, tenant_id: state.current.tenant_id, email: 'employee@fixture.invalid', display_name: 'Synthetic employee', role: 'user', is_active: true, created_at: DATE, updated_at: DATE }];
        else if (call.path === '/api/v1/admin/permissions') data = [state.profile];
        else if (call.path === `/api/v1/admin/permissions/${IDS.profile}` && call.method === 'PATCH') { assert.equal(typeof call.body.expected_revision, 'string'); if (state.profileConflict) { status = 409; data = { error: { code: 'CONFLICT', message: 'Synthetic stale profile' } }; } else data = { ...state.profile, ...call.body, revision: String(BigInt(state.profile.revision) + 1n) }; }
        else if (call.path === `/api/v1/company/outbound/${IDS.job}/inspect` && call.method === 'POST') data = state.inspection;
        else if (call.path === '/api/v1/auth/me/permissions') data = effective();
        else if (['/api/v1/domains', '/api/v1/admin/domains', '/api/v1/admin/tenants', '/api/v1/tenants', '/api/v1/company/recovery'].includes(call.path)) data = [];
        else if (['/api/v1/admin/runtime-config', '/api/v1/auth/logout'].includes(call.path)) data = {};
        else { state.violations.push('unlisted-api-path'); status = 501; data = { error: { code: 'FIXTURE_UNLISTED', message: 'Synthetic fixture path not allowed' } }; }
        await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(status === 200 ? { data, meta: { total: Array.isArray(data) ? data.length : 0, page: 1, per_page: 30 } } : data) });
        result.safe_events.push({ scenario: name, ...safeCall(call, status), wire_fulfill_completed: true });
      });
      const page = await context.newPage(); page.setDefaultTimeout(12000);
      const writes = () => state.calls.filter(c => c.method !== 'GET');
      const editorWrites = () => writes().filter(c => c.path.endsWith('/permission-editor'));
      async function openEditor(target = page) {
        entry.step = 'real_users_page';
        await target.goto(config.origin + '/admin/users');
        await target.getByText('employee@fixture.invalid', { exact: true }).waitFor();
        const row = target.getByRole('row').filter({ hasText: 'employee@fixture.invalid' });
        await row.locator('[data-slot="dropdown-menu-trigger"]').click();
        entry.step = 'real_row_menu_and_editor';
        await target.getByRole('menuitem', { name: 'Permissions', exact: true }).click();
        return target.getByRole('dialog');
      }
      const save = dialog => dialog.getByRole('button', { name: 'Save Overrides', exact: true });
      try {
        entry.step = 'scenario_actions';
        if (name === 'permission_presence') {
          for (const presence of ['bound', 'null', 'omitted']) {
            state.presence = presence; state.current = snapshot({ presence });
            const dialog = await openEditor();
            entry.step = `permission_${presence}_presence`;
            const quota = dialog.getByRole('spinbutton').nth(0);
            if (presence === 'omitted') { await expect(dialog.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled(); await expect(save(dialog)).toBeDisabled(); assert.equal(editorWrites().length, 2); }
            else {
              await expect(quota).toBeEnabled(); await expect(quota).toHaveValue('0');
              await expect(dialog.getByRole('spinbutton').nth(1)).toHaveValue('');
              await expect(dialog.getByRole('switch').first()).not.toBeChecked();
              await quota.fill('25'); await save(dialog).click();
              await expect.poll(() => editorWrites().length).toBe(presence === 'bound' ? 1 : 2);
              assert.deepEqual(editorWrites().at(-1).body.patch, { daily_send_quota: 25 });
              await expect(save(dialog)).toBeDisabled();
              await expect(quota).toHaveValue('25');
            }
          }
        } else if (name === 'two_tab_conflict') {
          const second = await context.newPage();
          const firstDialog = await openEditor(), secondDialog = await openEditor(second);
          await expect(firstDialog.getByRole('spinbutton').first()).toBeEnabled();
          await expect(secondDialog.getByRole('spinbutton').first()).toBeEnabled();
          entry.step = 'first_tab_write';
          await firstDialog.getByRole('spinbutton').first().fill('25'); await save(firstDialog).click();
          await expect(save(firstDialog)).toBeDisabled(); state.conflict = true;
          entry.step = 'second_tab_stale_write';
          await secondDialog.getByRole('spinbutton').first().fill('31'); await save(secondDialog).click();
          await expect(secondDialog.getByRole('alert')).toBeVisible();
          await expect(secondDialog.getByRole('spinbutton').first()).toHaveValue('31');
          await expect(save(secondDialog)).toBeDisabled(); assert.equal(editorWrites().length, 2);
          state.conflict = false; state.current.revision = { ...state.current.revision, user_revision: '9007199254740999' };
          entry.step = 'manual_editor_reload';
          await secondDialog.getByRole('button', { name: 'Reload', exact: true }).click();
          await expect(secondDialog.getByRole('spinbutton').first()).toBeEnabled();
          await expect(secondDialog.getByRole('spinbutton').first()).toHaveValue('25');
          await secondDialog.getByRole('spinbutton').first().fill('42'); await save(secondDialog).click();
          await expect(save(secondDialog)).toBeDisabled(); assert.equal(editorWrites()[2].body.expected_revision.user_revision, '9007199254740999');
        } else if (name.startsWith('late_')) {
          const old = snapshot(), gate = deferred(); state.pending = gate;
          const dialog = await openEditor();
          await expect.poll(() => state.calls.filter(c => c.path.endsWith('/permission-editor')).length).toBe(1);
          state.current = snapshot({ tenant: name === 'late_tenant' ? IDS.otherTenant : IDS.tenant, user: IDS.otherUser }); state.profile = state.current.profile;
          entry.step = 'synthetic_session_boundary';
          await page.evaluate(({ ids, tenantChange }) => {
            const actor = JSON.parse(localStorage.getItem('tabmail_user'));
            if (tenantChange) localStorage.setItem('tabmail_tenant_id', ids.otherTenant);
            else { actor.id = ids.otherUser; localStorage.setItem('tabmail_user', JSON.stringify(actor)); localStorage.setItem('tabmail_access_token', 'synthetic-next-session-not-a-JWT'); }
            localStorage.setItem('tabmail_session_epoch', crypto.randomUUID()); window.dispatchEvent(new Event('tabmail-auth-change'));
          }, { ids: IDS, tenantChange: name === 'late_tenant' });
          await expect(dialog).toHaveCount(0);
          entry.step = 'ignored_abort_late_response';
          const lateResponse = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/permission-editor'));
          gate.resolve(old); await assertDeliveredLateReply(await lateResponse);
          entry.late_reply_transport = { status: 200, finished_without_error: true };
          await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
          await expect(dialog).toHaveCount(0);
          await expect.poll(() => result.safe_events.filter(e => e.scenario === name && e.path.endsWith('/permission-editor')).length).toBe(1);
          const fresh = await openEditor(); await expect(fresh.getByRole('spinbutton').first()).toBeEnabled();
          await fresh.getByRole('spinbutton').first().fill('7'); await save(fresh).click();
          await expect(save(fresh)).toBeDisabled(); const sent = editorWrites()[0];
          assert.equal(sent.body.expected_revision.user_id, IDS.otherUser); assert.equal(sent.tenant, state.current.tenant_id);
        } else if (name === 'profile_conflict') {
          entry.step = 'real_profiles_page';
          state.profile.can_send = true;
          await page.goto(config.origin + '/admin/permissions');
          const row = page.getByRole('row').filter({ hasText: state.profile.name }); await row.locator('[data-slot="dropdown-menu-trigger"]').click();
          await page.getByRole('menuitem', { name: 'Edit', exact: true }).click();
          const dialog = page.getByRole('dialog'); const description = dialog.getByPlaceholder('Profile description (optional)');
          await description.fill('Synthetic preserved draft'); await dialog.getByRole('switch').first().click();
          entry.step = 'profile_conflict_preserve_draft';
          state.profileConflict = true; await dialog.getByRole('button', { name: 'Save', exact: true }).click();
          await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled(); await expect(description).toHaveValue('Synthetic preserved draft');
          assert.equal(writes().length, 1); assert.equal(writes()[0].body.can_send, false); assert.equal(writes()[0].body.daily_send_quota, 0); assert.equal(Object.hasOwn(writes()[0].body, 'fields'), false);
          state.profile.revision = '9007199254740999'; state.profileConflict = false;
          entry.step = 'manual_profile_refresh_confirm';
          await dialog.getByRole('button', { name: 'Refresh version', exact: true }).click();
          const confirmation = dialog.getByRole('checkbox'); await expect(confirmation).toBeVisible();
          await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled(); await confirmation.check();
          await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
          assert.equal(writes()[1].body.expected_revision, '9007199254740999');
        } else if (name === 'inspection_allowlist') {
          entry.step = 'real_recovery_page';
          await page.goto(config.origin + '/company/recovery');
          const reason = page.getByLabel('Inspection / recovery reason (8–1000 UTF-8 bytes after trimming)', { exact: true });
          const inspect = page.getByRole('button', { name: 'Audit and inspect', exact: true });
          await page.getByLabel('Outbound job ID', { exact: true }).fill(IDS.job);
          await reason.fill('short'); await expect(inspect).toBeDisabled(); assert.equal(writes().length, 0);
          await reason.fill('   Synthetic audit reason   '); await inspect.click();
          const output = page.getByLabel('Audited outbound inspection result', { exact: true }); await expect(output).toBeVisible();
          assert.deepEqual(writes()[0].body, { reason: 'Synthetic audit reason' });
          await expect(output).toContainText('5.1.1'); await expect(output.locator('img,iframe,script')).toHaveCount(0);
          assert.equal(await page.evaluate(() => window.inspectionExecuted), undefined);
          entry.step = 'closed_inspection_headers';
          state.inspection = inspection(); state.inspection.job.headers['DKIM-Signature'] = 'SYNTHETIC_FORBIDDEN_CANARY';
          await inspect.click(); await expect(inspect).toBeEnabled(); await expect(output).toHaveCount(0);
          await expect(page.locator('body')).not.toContainText('SYNTHETIC_FORBIDDEN_CANARY');
          entry.step = 'fixed_unknown_inspection_enum';
          state.inspection = inspection(); state.inspection.job.state = 'SYNTHETIC_UNKNOWN_ENUM'; state.inspection.job.status = 'SYNTHETIC_UNKNOWN_ENUM';
          await inspect.click(); await expect(output).toBeVisible(); await expect(output).toContainText('Unknown category; review required');
          await expect(output).not.toContainText('SYNTHETIC_UNKNOWN_ENUM');
        }
        assert.deepEqual(state.violations, []); entry.status = 'passed'; entry.step = 'completed';
      } catch (e) { entry.status = 'failed'; entry.error_class = e.name; throw e; }
      finally { await context.close(); }
    }
    result.status = 'browser_wire_consumers_passed'; return result;
  } catch (e) { result.status = 'browser_wire_consumers_failed'; result.error_class = e.name; throw Object.assign(new Error('Browser wire qualification failed'), { receipt: result }); }
  finally { await browser.close(); fs.writeFileSync(config.receipt, JSON.stringify(result, null, 2) + '\n', { mode: 0o600, flag: 'wx' }); }
}
module.exports = { IDS, SCENARIOS, INPUTS, loopbackOrigin, options, artifact, effective, profile, snapshot, inspection, safeCall, receipt, ignorePermissionAbort, assertDeliveredLateReply, run };
if (require.main === module) {
  (async () => { const config = options(process.argv.slice(2)); const out = await run(config); console.log(JSON.stringify({ status: out.status, scenarios: out.scenarios, shipping_Go_PG_SMTP_Docker_passed: false })); })().catch(e => { console.error(JSON.stringify({ status: 'failed_or_blocked', error_class: e.name, scenarios: e.receipt?.scenarios })); process.exitCode = 1; });
}
