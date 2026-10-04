import React from "react";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { Toaster } from "sonner";
import { SidebarProvider } from "@/components/ui/sidebar";
import { SubmissionPane } from "@/features/mail/components/submission-pane";
import { OffboardingPanel } from "@/features/company/offboarding-panel";
import UsersPage from "@/features/company/user-management";
import PermissionsPage from "@/features/company/profile-management";
import { LegacyReceiptFolder } from "@/features/mail/components/legacy-receipt-folder";
import { ReceiptFolder } from "@/features/mail/components/receipt-folder";
import { Compose } from "@/components/company/compose";
import { executeOffboarding } from "@/features/company/api";
import { company, type MailDraft, type WorkMailbox } from "@/lib/company";
import { listUsers, updateUser, updatePermissionProfile, listPermissionProfiles } from "@/lib/api";
import { getUserPermissionEditor, patchUserPermissionEditor } from "@/lib/api/permissions";
import { validatePermissionEditorCommand, validateObservedPermissionProfile } from "@/lib/api/permission-editor-types";
import { parseReceiptListResponse, parseReceiptResponse, type OrdinaryReceipt, type ReceiptCounts, type RetryBlockReason } from "@/lib/receipt-types";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import { assertPermissionIntent, resetPermissionPatch } from "./r5-permission-contract-oracle";
import { expectedReceiptCapabilities, receiptCapabilitiesMatch, receiptCountsMatch, type ReceiptSurface } from "./r5-receipt-contract-oracle";

// Host identity is supplied; API functions, session, SWR, controls, fetch,
// shipping HTTP handlers and PostgreSQL are never response-mocked.
const host = vi.hoisted(() => ({ user: null as AuthUser | null }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ user: host.user, level: host.user?.role ?? "user", tenantId: host.user?.tenant_id ?? null }) }));
interface Fixture {
  schema_version: number; case_id: string; variant: string; case_sha256: string;
  api_url: string; auth: { token: string; user: AuthUser };
  employee_id: string; employee_email: string; successor_id: string; foreign_user_id?: string;
  zone_id: string; profile_id?: string; profile_name?: string; control_plan_id?: string;
  external_employee_token?: string; draft?: MailDraft; mailboxes?: WorkMailbox[];
  receipt_state?: OrdinaryReceipt["state"]; receipt_counts?: ReceiptCounts;
  submission_id?: string; receipt_subject?: string; private_values?: string[]; private_addresses?: string[]; input: Record<string, unknown>;
}
const path = process.env.TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE;
if (!path) throw new Error("Explicit Go-owned protocol component fixture is required; no skip or response fallback");
const fixture = JSON.parse(readFileSync(path, "utf8")) as Fixture;
const raw = readFileSync(resolve(process.cwd(), "../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"));
const cases = (JSON.parse(raw.toString()) as { cases: { id: string; input: unknown; expected?: { projection?: { status: OrdinaryReceipt["status"]; label_en: string; retry: boolean; delivery_uncertain: boolean; retry_block_reason: RetryBlockReason | null } } }[] }).cases;
const shared = cases.find(c => c.id === fixture.case_id);
const origin = new URL(fixture.api_url);
if (fixture.schema_version !== 1 || !shared || fixture.case_sha256 !== createHash("sha256").update(raw).digest("hex") || JSON.stringify(shared.input) !== JSON.stringify(fixture.input) || origin.protocol !== "http:" || origin.hostname !== "127.0.0.1") {
  throw new Error("Invalid source hash, shared input, or non-loopback Go-owned fixture");
}
const calls: { method: string; path: string; body?: Record<string, unknown>; status: number; data?: unknown }[] = [];
beforeEach(() => {
  process.env.NEXT_PUBLIC_API_URL = origin.origin;
  host.user = fixture.auth.user; installSession(fixture.auth.token, fixture.auth.user);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.spyOn(window, "confirm").mockReturnValue(true);
  const fetchReal = globalThis.fetch.bind(globalThis);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const target = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    if (target.origin !== origin.origin) throw new Error("Component attempted non-fixture network I/O");
    const response = await fetchReal(input, init);
    let data: unknown; try { data = await response.clone().json(); } catch { /* real 204/non-JSON */ }
    calls.push({ method: init?.method ?? "GET", path: target.pathname, body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined, status: response.status, data });
    return response;
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); delete process.env.NEXT_PUBLIC_API_URL; });
function target(marker: string, detail: string): never { throw new Error(`${marker}: ${detail}`); }
// Inspect the captured REAL response, including fields the UI parser could reject.
// Boolean assertions avoid publishing response bodies or private canaries on failure.
function assertNoReceiptSecrets(data: unknown) {
  const recipients = fixture.input.recipients as { address: string }[] | undefined;
  const secrets = [...(fixture.private_values ?? []), ...(fixture.private_addresses ?? []),
    ...(recipients ?? []).map(r => r.address), fixture.receipt_subject,
    ...["subject", "text_body", "smtp_response", "delivery_token"].map(key => fixture.input[key])]
    .filter((value): value is string => typeof value === "string" && value.length > 0);
  const encoded = JSON.stringify(data) ?? "";
  if (secrets.some(value => encoded.includes(value))) {
    target(fixture.case_id === "RC02" ? "R5_PROTOCOL_UI_TARGET_RC02_LEGACY_BYPASS" :
      fixture.case_id === "RC03" ? "R5_PROTOCOL_UI_TARGET_RC03_BCC" : "R5_PROTOCOL_UI_TARGET_RC01_BCC",
    "actual receipt response contains private content or recipient data");
  }
}
function expectedCounts(): ReceiptCounts {
  if (fixture.receipt_counts) return fixture.receipt_counts;
  const recipients = fixture.input.recipients as { state: string }[] | undefined;
  if (!recipients?.length) throw new Error("Receipt seed ledger oracle missing");
  const counts: ReceiptCounts = { total: recipients.length, accepted: 0, pending: 0, temporary: 0, permanent: 0, uncertain: 0 };
  for (const recipient of recipients) {
    if (!["accepted", "pending", "temporary", "permanent", "uncertain"].includes(recipient.state)) throw new Error("Invalid receipt seed state");
    counts[recipient.state as Exclude<keyof ReceiptCounts, "total">]++;
  }
  return counts;
}
function assertReceipt(data: unknown, id: string, surface: ReceiptSurface): OrdinaryReceipt {
  assertNoReceiptSecrets(data);
  const receipt = parseReceiptResponse(data, { id, tenantId: fixture.auth.user.tenant_id });
  // The seeded real job is NOT a degraded, scope-less committed fallback.
  expect(receipt.tenant_id === fixture.auth.user.tenant_id).toBe(true);
  expect(Boolean(receipt.created_at && receipt.updated_at && receipt.attempt_count === 0)).toBe(true);
  const expectedState = fixture.receipt_state ?? fixture.input.job_state;
  expect(receipt.state === expectedState).toBe(true);
  expect(receipt.progress.completeness === "known" &&
    receiptCountsMatch(receipt.progress.counts, expectedCounts())).toBe(true);
  const projection = shared!.expected?.projection;
  expect(receipt.status === (projection?.status ?? (expectedState === "pending" ? "submitted" : "needs_attention"))).toBe(true);
  expect(receipt.delivery_uncertain === (projection?.delivery_uncertain ?? false)).toBe(true);
  expect(receiptCapabilitiesMatch(receipt.capabilities, expectedReceiptCapabilities(surface, { view_content: false,
    retry: projection?.retry ?? false, retry_block_reason: projection ? (projection.retry_block_reason ?? "") : "state_not_retryable" }))).toBe(true);
  return receipt;
}
function assertRenderedReceipt(container: HTMLElement, receipt: OrdinaryReceipt) {
  expect(container.textContent?.includes(receipt.id)).toBe(true);
  const aggregate = within(container).getByTestId("ordinary-receipt-aggregate");
  if (receipt.progress.completeness !== "known") throw new Error("Expected complete receipt ledger");
  for (const [key, count] of Object.entries(receipt.progress.counts)) {
    expect(within(aggregate).getByTestId(`receipt-count-${key}`).querySelector("dd")?.textContent === String(count)).toBe(true);
  }
  expect(within(container).queryByTestId("receipt-content-disclosure")).not.toBeInTheDocument();
  expect(calls.some(c => /\/(content|attachments)(\/|$)/.test(c.path))).toBe(false);
  assertNoReceiptSecrets(container.textContent);
}
async function rowDialog(text: string, action: string) {
  const cell = await screen.findByText(text); const row = cell.closest("tr");
  const button = row?.querySelector<HTMLButtonElement>('[data-slot="dropdown-menu-trigger"]');
  if (!button) throw new Error("Actual shipping table action trigger missing");
  await userEvent.click(button); await userEvent.click(await screen.findByRole("menuitem", { name: action }));
  return await screen.findByRole("dialog");
}
async function openOffboard() {
  const employees = (await listUsers({ page: 1, per_page: 100 })).data;
  render(<><Toaster /><OffboardingPanel employees={employees} refresh={() => listUsers({ page: 1, per_page: 100 })} /></>);
}
async function preview() {
  await userEvent.selectOptions(screen.getByLabelText("Employee to offboard"), fixture.employee_id);
  await userEvent.selectOptions(screen.getByLabelText("Mailbox successor"), fixture.successor_id);
  await userEvent.type(screen.getByLabelText(/Reason \/ ticket/), "Shared protocol authorized handover");
  await userEvent.click(screen.getByRole("button", { name: "Preview handover impact" }));
  const paragraph = await screen.findByText(/^Plan:/);
  const id = paragraph.textContent?.match(/[0-9a-f]{8}-[0-9a-f-]{27,}/i)?.[0];
  if (!id) throw new Error("Real preview omitted plan identity");
  return id;
}
async function confirm() { await userEvent.click(screen.getByRole("button", { name: "Confirm handover" })); }
async function waitExecute(prior = 0) {
  await waitFor(() => expect(calls.filter(c => c.method === "POST" && c.path.endsWith("/offboard")).length).toBeGreaterThan(prior));
  return calls.filter(c => c.method === "POST" && c.path.endsWith("/offboard")).at(-1)!;
}

test(`R5 protocol component ${fixture.case_id} ${fixture.variant} secure behavior`, async () => {
  const id = fixture.case_id;
  if (id === "RC02") {
    if (fixture.input.content_expired !== true) throw new Error("Original compatibility expiry scenario missing");
    let receiptId = fixture.submission_id;
    if (fixture.variant === "submit_replay") {
      if (!fixture.draft || !fixture.mailboxes) throw new Error("Actual replay draft missing");
      render(<Compose initial={fixture.draft} mailboxes={fixture.mailboxes} onClose={() => {}} onSent={() => {}} />);
      await userEvent.click(screen.getByRole("button", { name: "Send" }));
      await screen.findByRole("button", { name: "Retry same submission" });
      await userEvent.click(screen.getByRole("button", { name: "Retry same submission" }));
      await waitFor(() => expect(calls.some(c => c.method === "POST" && c.path.endsWith("/submit") && c.status === 200)).toBe(true));
      const replay = calls.find(c => c.method === "POST" && c.path.endsWith("/submit") && c.status === 200)!;
      const replayReceipt = parseReceiptResponse(replay.data, { tenantId: fixture.auth.user.tenant_id });
      receiptId = replayReceipt.id;
      assertReceipt(replay.data, receiptId, "replay");
      cleanup(); render(<LegacyReceiptFolder />);
    } else {
      render(<ReceiptFolder page={1} selected="" onSelect={() => {}} onPage={() => {}} includeCompatibility />);
      await userEvent.click(screen.getByRole("button", { name: "Compatibility receipts" }));
      expect(screen.getByRole("button", { name: "Compatibility receipts" })).toHaveAttribute("aria-pressed", "true");
    }
    await screen.findAllByRole("button", { name: "View compatibility receipt" });
    const listCall = calls.find(c => c.method === "GET" && c.path === "/api/v1/outbound" && c.status === 200);
    if (!listCall) throw new Error("Real compatibility list response missing");
    assertNoReceiptSecrets(listCall.data);
    const list = parseReceiptListResponse(listCall.data, { tenantId: fixture.auth.user.tenant_id });
    if (!receiptId) throw new Error("Seeded or replay receipt identity missing");
    expect(list.data.length === 1 && list.data[0].id === receiptId).toBe(true);
    assertReceipt({ data: list.data[0] }, receiptId, "list");
    await userEvent.click(screen.getAllByRole("button", { name: "View compatibility receipt" })[0]);
    const detail = await screen.findByRole("region", { name: "Compatibility receipt detail" }).catch(async () => screen.findByLabelText("Compatibility receipt detail"));
    await within(detail).findByTestId("ordinary-receipt-aggregate");
    const detailCall = calls.find(c => c.method === "GET" && c.path === `/api/v1/outbound/${receiptId}` && c.status === 200);
    if (!detailCall) throw new Error("Real compatibility detail response missing");
    const receipt = assertReceipt(detailCall.data, receiptId, "detail");
    assertRenderedReceipt(detail, receipt);
    assertNoReceiptSecrets(screen.getByRole("region", { name: "Compatibility task receipts" }).textContent);
    expect(within(detail).getByText(receipt.status === "submitted" ? "Submitted" : "Needs attention")).toBeInTheDocument();
    return;
  }
  if (id.startsWith("RC")) {
    if (!(fixture.input.content_expired === true || fixture.input.content_allowed === false) || !fixture.submission_id) throw new Error("Expired/redacted receipt scenario missing");
    render(<SubmissionPane id={fixture.submission_id} />);
    const aggregate = await screen.findByTestId("ordinary-receipt-aggregate");
    const real = calls.find(c => c.method === "GET" && c.path === `/api/v1/company/submissions/${fixture.submission_id}` && c.status === 200);
    if (!real) throw new Error("Real company submission response missing");
    const receipt = assertReceipt(real.data, fixture.submission_id, "detail");
    const pane = screen.getByTestId("ordinary-submission-pane");
    assertRenderedReceipt(pane, receipt);
    const projection = shared!.expected?.projection;
    expect(within(aggregate).getByText(projection?.label_en ?? "Needs attention")).toBeInTheDocument();
    expect(Boolean(within(pane).queryByTestId("receipt-retry"))).toBe(receipt.capabilities!.retry);
    expect(Boolean(within(pane).queryByRole("status"))).toBe(receipt.delivery_uncertain);
    if (receipt.delivery_uncertain) expect(within(pane).getByRole("status")).toHaveTextContent("Retry is blocked; refresh the status.");
    return;
  }
  if (id.startsWith("LF")) {
    await openOffboard();
    if (id === "LF01") {
      const choices = screen.getByLabelText("Employee to offboard") as HTMLSelectElement;
      if (!Array.from(choices.options).some(o => o.value === fixture.employee_id)) target("R5_PROTOCOL_UI_TARGET_LF01_FROZEN", "actual frozen employee is excluded from the shipping handover selector");
      await preview(); return;
    }
    if (id === "LF05" && fixture.variant !== "frozen") {
      const targetSelect = screen.getByLabelText("Employee to offboard") as HTMLSelectElement;
      if (fixture.variant === "higher_role") { expect(Array.from(targetSelect.options).map(o => o.value)).not.toContain(fixture.employee_id); return; }
      await userEvent.selectOptions(targetSelect, fixture.employee_id);
      const successor = screen.getByLabelText("Mailbox successor") as HTMLSelectElement;
      expect(Array.from(successor.options).map(o => o.value)).not.toContain(fixture.variant === "self" ? fixture.employee_id : fixture.foreign_user_id);
      return;
    }
    const plan = await preview();
    let replayReceipt: Awaited<ReturnType<typeof executeOffboarding>> | undefined;
    if (id === "LF02") replayReceipt = await executeOffboarding(fixture.employee_id, plan);
    if (id === "LF03") { if (!fixture.control_plan_id) throw new Error("Actual first plan missing"); await executeOffboarding(fixture.employee_id, fixture.control_plan_id); }
    if (id === "LF04") {
      if (!fixture.draft || !fixture.external_employee_token) throw new Error("Actual employee draft mutation identity missing");
      await company(`/drafts/${fixture.draft.id}`, { method: "PUT", headers: { Authorization: `Bearer ${fixture.external_employee_token}` }, body: { ...fixture.draft, payload: { ...fixture.draft.payload, subject: "Same-count newer private draft revision" } } });
    }
    if (id === "LF05") await updateUser(fixture.successor_id, { is_active: false });
    if (id === "LF06") { await executeOffboarding(fixture.employee_id, plan); await updateUser(fixture.employee_id, { is_active: true }); }
    const prior = calls.filter(c => c.method === "POST" && c.path.endsWith("/offboard")).length;
    await confirm(); const result = await waitExecute(prior);
    if (id === "LF02") {
      expect(result.status).toBe(200);
      await screen.findByText(/Handover completed; the disposition receipt is retained/);
      expect(replayReceipt?.executed_at).toBeTruthy();
      expect(screen.getByRole("status")).toHaveTextContent(replayReceipt!.executed_at!);
      const executes = calls.filter(c => c.method === "POST" && c.path.endsWith("/offboard"));
      expect(executes).toHaveLength(2); expect(executes[0].body?.plan_id).toBe(plan); expect(executes[1].body?.plan_id).toBe(plan);
      expect(executes[1].data).toEqual(executes[0].data);
      return;
    }
    if (id === "LF06" && result.status === 200) target("R5_PROTOCOL_UI_TARGET_LF06_LIFECYCLE", "actual stale plan after reactivation returned success to the shipping panel");
    if (["LF03", "LF04", "LF06"].includes(id)) {
      expect(result.status).toBe(409); await screen.findByRole("alert"); expect(screen.queryByText(/Handover completed/)).not.toBeInTheDocument();
    } else if (id === "LF05") {
      expect(result.status).toBe(400); expect(screen.queryByText(/Handover completed/)).not.toBeInTheDocument();
    } else if (id === "LF07") {
      expect(result.status).toBe(500); await screen.findByText(/internal server error/i); expect(screen.queryByText(/Handover completed/)).not.toBeInTheDocument();
    }
    return;
  }
  if (id === "PE03") {
    if (fixture.input.stale_revision !== true || fixture.input.patch_contains_old_security_fields !== true) throw new Error("Shared old-revision/security control input missing");
    if (!fixture.profile_id || !fixture.profile_name) throw new Error("Actual profile fixture missing");
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>);
    const dialog = await rowDialog(fixture.profile_name, "Edit");
    // Observe the current persistent version through the real authorized GET;
    // the shipping editor retains the older snapshot opened above.
    const observed = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id);
    if (!observed) throw new Error("Actual profile GET omitted the fixture profile");
    validateObservedPermissionProfile(observed);
    expect(observed.can_send).toBe(true);
    const input = within(dialog).getByPlaceholderText("Profile description (optional)");
    await userEvent.clear(input); await userEvent.type(input, "Shared stale description");
    const initialSave = within(dialog).getByRole("button", { name: /^Save$/ });
    expect(initialSave).toBeEnabled();
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
    const revoked = (await updatePermissionProfile(fixture.profile_id, { expected_revision: observed.revision, fields: { can_send: false } })).data;
    expect(revoked.can_send).toBe(false);
    expect(BigInt(revoked.revision)).toBeGreaterThan(BigInt(observed.revision));
    // UI control: an invalidated dirty shipping editor must block Save.
    // HTTP CAS is an independent real request below, not a UI request.
    const save = within(dialog).getByRole("button", { name: /^Save$/ });
    await within(dialog).findByRole("alert");
    expect(save).toBeDisabled();
    expect(input).toHaveValue("Shared stale description");
    expect(within(dialog).getAllByRole("switch")[0]).toHaveAttribute("aria-checked", "true");
    await userEvent.click(save);
    expect(calls.filter(c => c.method === "PATCH" && c.path.endsWith(fixture.profile_id!))).toHaveLength(1);
    const afterUI = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id);
    if (afterUI?.can_send) target("R5_PROTOCOL_UI_TARGET_PE03_STALE", "actual stale shipping editor restored revoked sending");
    expect(afterUI?.revision).toBe(revoked.revision);
    expect(afterUI?.description).toBe(observed.description);
    // Catalog HTTP control: explicit old revision and old security fields.
    // This second PATCH is issued by the test API client, never by Save.
    await expect(updatePermissionProfile(fixture.profile_id, {
      expected_revision: observed.revision,
      fields: { description: "Shared stale description", can_send: observed.can_send,
        daily_send_quota: observed.daily_send_quota, daily_receive_quota: observed.daily_receive_quota,
        max_mailboxes: observed.max_mailboxes, max_domains: observed.max_domains,
        allowed_zone_ids: observed.allowed_zone_ids ?? [], can_create_domains: observed.can_create_domains,
        can_create_routes: observed.can_create_routes, can_create_api_keys: observed.can_create_api_keys },
    })).rejects.toMatchObject({ error: { code: "CONFLICT" } });
    await waitFor(() => expect(calls.filter(c => c.method === "PATCH" && c.path.endsWith(fixture.profile_id!)).length).toBe(2));
    const after = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id);
    if (after?.can_send) target("R5_PROTOCOL_UI_TARGET_PE03_STALE", "actual stale shipping editor restored revoked sending");
    expect(calls.filter(c => c.method === "PATCH" && c.path.endsWith(fixture.profile_id!)).at(-1)?.status).toBe(409);
    expect(after?.revision).toBe(revoked.revision);
    expect(after?.description).toBe(observed.description);
    return;
  }
  if (id === "PE05") {
    if (!fixture.draft || !fixture.mailboxes) throw new Error("Actual locked draft fixture missing");
    render(<Compose initial={fixture.draft} mailboxes={fixture.mailboxes} onClose={() => {}} onSent={() => {}} />);
    const subject = screen.getByLabelText(/Subject/i); await userEvent.clear(subject); await userEvent.type(subject, "Actual UI serialized edit");
    await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(calls.some(c => c.method === "PUT" && c.path.endsWith(fixture.draft!.id!))).toBe(true), { timeout: 15000 });
    const mailboxes = await company<WorkMailbox[]>("/mailboxes");
    const mb = mailboxes.find(row => row.mailbox.id === fixture.draft!.mailbox_id); expect(mb).toBeDefined(); expect(mb!.can_send).toBe(false);
    // The next real UI save must not use its old props/authorization snapshot.
    await userEvent.clear(subject); await userEvent.type(subject, "After completed revocation");
    await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(calls.filter(c => c.method === "PUT" && c.path.endsWith(fixture.draft!.id!)).length).toBeGreaterThan(1));
    expect(calls.filter(c => c.method === "PUT" && c.path.endsWith(fixture.draft!.id!)).at(-1)?.status).toBe(403);
    expect(await screen.findByRole("alert")).toHaveTextContent(/permission|forbidden|authority/i);
    return;
  }
  if (["PE01", "PE02", "PE04"].includes(id)) {
    let before = (await getUserPermissionEditor(fixture.employee_id)).data;
    // The Go seed deliberately stores false. To qualify an explicit changed
    // false intent (rather than an unchanged false omission), prepare true
    // through a successful current CAS before opening the shipping editor.
    if (id === "PE02" && fixture.variant === "false") {
      const prepared = (await patchUserPermissionEditor(fixture.employee_id, {
        expected_revision: before.revision, patch: { can_send: true },
      })).data;
      expect(BigInt(prepared.revision.user_revision)).toBeGreaterThan(BigInt(before.revision.user_revision));
      before = (await getUserPermissionEditor(fixture.employee_id)).data;
      expect(before.overrides?.can_send).toBe(true);
    }
    render(<SidebarProvider><UsersPage /></SidebarProvider>);
    const dialog = await rowDialog(fixture.employee_email, "Permissions");
    await waitFor(() => expect(dialog.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(0));
    const quota = within(dialog).getAllByRole("spinbutton")[0];
    const switchControl = within(dialog).getAllByRole("switch")[0];
    const editorPath = `/api/v1/admin/users/${fixture.employee_id}/permission-editor`;
    const writes = () => calls.filter(c => c.method === "PATCH" && c.path === editorPath);
    if (id === "PE04") {
      if (fixture.input.old_revision !== true || fixture.input.override_recreated_or_profile_reassigned !== true) throw new Error("Shared ABA old revision/recreation input missing");
      // Dirty stale editor tries to grant sending. Independent HTTP controls
      // reset/recreate raw restrictions using each successful returned version.
      await userEvent.click(switchControl);
      const save = within(dialog).getByRole("button", { name: /^Save Overrides$/ });
      expect(save).toBeEnabled();
      expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
      const reset = (await patchUserPermissionEditor(fixture.employee_id, {
        expected_revision: before.revision, patch: resetPermissionPatch,
      })).data;
      expect(BigInt(reset.revision.user_revision)).toBeGreaterThan(BigInt(before.revision.user_revision));
      expect(reset.overrides === null || reset.overrides.can_send === null).toBe(true);
      const restored = (await patchUserPermissionEditor(fixture.employee_id, {
        expected_revision: reset.revision,
        patch: { can_send: false, daily_send_quota: 19, domain_access: { mode: "list", zone_ids: [fixture.zone_id] } },
      })).data;
      expect(BigInt(restored.revision.user_revision)).toBeGreaterThan(BigInt(reset.revision.user_revision));
      expect(restored.overrides?.can_send).toBe(false);
      expect(restored.effective.can_send).toBe(false);
      await within(dialog).findByRole("alert");
      expect(save).toBeDisabled();
      expect(switchControl).toHaveAttribute("aria-checked", "true");
      const prior = writes().length;
      await userEvent.click(save);
      expect(writes()).toHaveLength(prior);
      const afterUI = (await getUserPermissionEditor(fixture.employee_id)).data;
      if (afterUI.effective.can_send || afterUI.overrides?.can_send !== false) target("R5_PROTOCOL_UI_TARGET_PE04_ABA", "actual stale shipping editor restored sending after reset/recreation");
      expect(afterUI.revision).toEqual(restored.revision);
      expect(afterUI.overrides).toEqual(restored.overrides);
      // Separately labelled direct old compound-revision CAS, not a UI Save.
      await expect(patchUserPermissionEditor(fixture.employee_id, {
        expected_revision: before.revision, patch: { can_send: true },
      })).rejects.toMatchObject({ error: { code: "CONFLICT" } });
      expect(writes()).toHaveLength(prior + 1);
      expect(writes().at(-1)?.status).toBe(409);
      const after = (await getUserPermissionEditor(fixture.employee_id)).data;
      if (after.effective.can_send || after.overrides?.can_send !== false) target("R5_PROTOCOL_UI_TARGET_PE04_ABA", "old editor/control restored sending after versioned reset and recreation");
      expect(after.revision).toEqual(restored.revision);
      expect(after.overrides).toEqual(restored.overrides);
      return;
    }
    if (id === "PE01" || fixture.variant === "omitted") {
      await userEvent.clear(quota); await userEvent.type(quota, String((fixture.input.patch as { daily_send_quota?: number } | undefined)?.daily_send_quota ?? 25));
    } else if (fixture.variant === "false") await userEvent.click(switchControl);
    else if (fixture.variant === "null") {
      const reset = switchControl.parentElement!.querySelector<HTMLButtonElement>('button[data-slot="button"]');
      if (!reset) throw new Error("Actual inheritance reset control missing");
      await userEvent.click(reset);
    } else if (fixture.variant === "0") { await userEvent.clear(quota); await userEvent.type(quota, "0"); }
    else if (fixture.variant === "[]") await userEvent.click(within(dialog).getByRole("button", { name: /^All$/ }));
    else throw new Error("Unknown permission intent variant");
    const prior = writes().length;
    await userEvent.click(within(dialog).getByRole("button", { name: /^Save Overrides$/ }));
    await waitFor(() => expect(writes()).toHaveLength(prior + 1));
    const mutation = writes().at(-1)!;
    expect(mutation.status).toBe(200);
    const command = mutation.body as unknown as import("@/lib/api/permission-editor-types").PermissionEditorCommand;
    validatePermissionEditorCommand(command, fixture.employee_id);
    expect(command.expected_revision).toEqual(before.revision);
    const after = (await getUserPermissionEditor(fixture.employee_id)).data;
    assertPermissionIntent(id, fixture.variant, before, command, after);
    return;
  }
  throw new Error("No genuine shipping component consumer for this fixture case");
}, 30000);
