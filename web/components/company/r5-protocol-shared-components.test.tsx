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
import { listUsers, getUserPermission, updateUser, updatePermissionProfile, listPermissionProfiles, setUserPermissionOverride, deleteUserPermissionOverride } from "@/lib/api";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";

// Host identity is supplied; API functions, session, SWR, controls, fetch,
// shipping HTTP handlers and PostgreSQL are never response-mocked.
const host = vi.hoisted(() => ({ user: null as AuthUser | null }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ user: host.user, level: host.user?.role ?? "user" }) }));
interface Fixture {
  schema_version: number; case_id: string; variant: string; case_sha256: string;
  api_url: string; auth: { token: string; user: AuthUser };
  employee_id: string; employee_email: string; successor_id: string; foreign_user_id?: string;
  zone_id: string; profile_id?: string; profile_name?: string; control_plan_id?: string;
  external_employee_token?: string; draft?: MailDraft; mailboxes?: WorkMailbox[];
  submission_id?: string; receipt_subject?: string; private_values?: string[]; private_addresses?: string[]; input: Record<string, unknown>;
}
const path = process.env.TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE;
if (!path) throw new Error("Explicit Go-owned protocol component fixture is required; no skip or response fallback");
const fixture = JSON.parse(readFileSync(path, "utf8")) as Fixture;
const raw = readFileSync(resolve(process.cwd(), "../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"));
const cases = (JSON.parse(raw.toString()) as { cases: { id: string; input: unknown; expected?: { projection?: { label_en: string; retry: boolean; delivery_uncertain: boolean } } }[] }).cases;
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
    if (fixture.variant === "submit_replay") {
      if (!fixture.draft || !fixture.mailboxes) throw new Error("Actual replay draft missing");
      render(<Compose initial={fixture.draft} mailboxes={fixture.mailboxes} onClose={() => {}} onSent={() => {}} />);
      await userEvent.click(screen.getByRole("button", { name: "Send" }));
      await screen.findByRole("button", { name: "Retry same submission" });
      await userEvent.click(screen.getByRole("button", { name: "Retry same submission" }));
      await waitFor(() => expect(calls.some(c => c.method === "POST" && c.path.endsWith("/submit") && c.status === 200)).toBe(true));
      const replay = calls.find(c => c.method === "POST" && c.path.endsWith("/submit") && c.status === 200)!;
      if (fixture.private_values?.some(value => JSON.stringify(replay.data).includes(value))) target("R5_PROTOCOL_UI_TARGET_RC02_LEGACY_BYPASS", "actual replay response resurrected expired private content");
      cleanup(); render(<LegacyReceiptFolder />);
    } else {
      render(<ReceiptFolder page={1} selected="" onSelect={() => {}} onPage={() => {}} includeCompatibility />);
      await userEvent.click(screen.getByRole("button", { name: "Compatibility receipts" }));
      expect(screen.getByRole("button", { name: "Compatibility receipts" })).toHaveAttribute("aria-pressed", "true");
    }
    await screen.findAllByText(fixture.receipt_subject!);
    await userEvent.click(screen.getAllByRole("button", { name: "View compatibility receipt" })[0]);
    const detail = await screen.findByRole("region", { name: "Compatibility receipt detail" }).catch(async () => screen.findByLabelText("Compatibility receipt detail"));
    expect(within(detail).queryByRole("button", { name: /Open preserved sent content/ })).not.toBeInTheDocument();
    await screen.findByText("Content unavailable; the safe receipt is retained.");
    const actual = calls.filter(c => c.path === "/api/v1/outbound" || /^\/api\/v1\/outbound\/[^/]+$/.test(c.path));
    expect(actual.some(c => c.path === "/api/v1/outbound" && c.status === 200)).toBe(true);
    expect(actual.some(c => c.path !== "/api/v1/outbound" && c.status === 200)).toBe(true);
    if (actual.some(c => fixture.private_values?.some(value => JSON.stringify(c.data).includes(value)))) target("R5_PROTOCOL_UI_TARGET_RC02_LEGACY_BYPASS", "actual legacy HTTP output contains expired body or private recipients even if the UI omits them");
    expect(screen.queryByText("PRIVATE_COMPATIBILITY_BODY")).not.toBeInTheDocument();
    return;
  }
  if (id.startsWith("RC")) {
    if (!(fixture.input.content_expired === true || fixture.input.content_allowed === false) || !fixture.submission_id) throw new Error("Expired/redacted receipt scenario missing");
    render(<SubmissionPane id={fixture.submission_id} />);
    await screen.findByText(/Attachments:/);
    expect(screen.queryByRole("button", { name: /content/i })).not.toBeInTheDocument();
    const projection = shared!.expected?.projection;
    if (projection) {
      await screen.findByText(projection.label_en);
      if (!projection.retry) expect(screen.queryByRole("button", { name: /Retry unfinished recipients/ })).not.toBeInTheDocument();
      if (projection.delivery_uncertain) await screen.findByText(/Uncertain outcome: retry is blocked/);
    }
    if (fixture.private_addresses?.some(address => screen.queryAllByText(new RegExp(address.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))).length)) {
      target(id === "RC03" ? "R5_PROTOCOL_UI_TARGET_RC03_BCC" : "R5_PROTOCOL_UI_TARGET_RC01_BCC", "actual HTTP receipt rendered a private BCC address");
    }
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
    if (!fixture.profile_id || !fixture.profile_name) throw new Error("Actual profile fixture missing");
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>);
    const dialog = await rowDialog(fixture.profile_name, "Edit");
    await updatePermissionProfile(fixture.profile_id, { can_send: false });
    const input = within(dialog).getByPlaceholderText("Profile description (optional)");
    await userEvent.clear(input); await userEvent.type(input, "Shared stale description");
    await userEvent.click(within(dialog).getByRole("button", { name: /^Save$/ }));
    await waitFor(() => expect(calls.filter(c => c.method === "PATCH" && c.path.endsWith(fixture.profile_id!)).length).toBe(2));
    const after = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id);
    if (after?.can_send) target("R5_PROTOCOL_UI_TARGET_PE03_STALE", "actual stale shipping editor restored revoked sending");
    expect(calls.filter(c => c.method === "PATCH" && c.path.endsWith(fixture.profile_id!)).at(-1)?.status).toBe(409);
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
    const before = (await getUserPermission(fixture.employee_id)).data;
    render(<SidebarProvider><UsersPage /></SidebarProvider>);
    const dialog = await rowDialog(fixture.employee_email, "Permissions");
    await waitFor(() => expect(dialog.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(0));
    const quota = within(dialog).getAllByRole("spinbutton")[0];
    const switchControl = within(dialog).getAllByRole("switch")[0];
    if (id === "PE01" || fixture.variant === "omitted") { await userEvent.clear(quota); await userEvent.type(quota, String((fixture.input.patch as { daily_send_quota?: number } | undefined)?.daily_send_quota ?? 25)); }
    else if (id === "PE04") { await userEvent.click(switchControl); await deleteUserPermissionOverride(fixture.employee_id); await setUserPermissionOverride(fixture.employee_id, { can_send: false }); }
    else if (fixture.variant === "false" || fixture.variant === "null") {
      await userEvent.click(switchControl); await userEvent.click(switchControl);
      if (fixture.variant === "null") { const group = switchControl.parentElement!; const reset = group.querySelector<HTMLButtonElement>('button[data-slot="button"]'); if (!reset) throw new Error("Actual inheritance reset control missing"); await userEvent.click(reset); }
    } else if (fixture.variant === "0") { await userEvent.clear(quota); await userEvent.type(quota, "0"); }
    else if (fixture.variant === "[]") await userEvent.click(within(dialog).getByRole("button", { name: /^All$/ }));
    else throw new Error("Unknown permission intent variant");
    await userEvent.click(within(dialog).getByRole("button", { name: /^Save Overrides$/ }));
    await waitFor(() => expect(calls.some(c => c.method === "PUT" && c.path.endsWith(`${fixture.employee_id}/permissions`))).toBe(true));
    const mutation = calls.filter(c => c.method === "PUT" && c.path.endsWith(`${fixture.employee_id}/permissions`)).at(-1)!;
    const after = (await getUserPermission(fixture.employee_id)).data;
    if (id === "PE04") { if (mutation.status === 200 && after.can_send) target("R5_PROTOCOL_UI_TARGET_PE04_ABA", "actual old editor save restored sending after override recreation"); expect(mutation.status).toBe(409); return; }
    if (fixture.variant === "null" && (!Object.hasOwn(mutation.body ?? {}, "can_send") || mutation.body?.can_send !== null)) target("R5_PROTOCOL_UI_TARGET_PE02_NULL", "shipping inheritance reset sent omission instead of explicit null");
    if (!Object.hasOwn(mutation.body ?? {}, "can_send") && after.can_send !== before.can_send || !Object.hasOwn(mutation.body ?? {}, "allowed_zone_ids") && JSON.stringify(after.allowed_zone_ids) !== JSON.stringify(before.allowed_zone_ids)) {
      target(id === "PE01" ? "R5_PROTOCOL_UI_TARGET_PE01_OMITTED" : "R5_PROTOCOL_UI_TARGET_PE02_OMITTED", "actual shipping editor omitted fields lost existing restrictions in PostgreSQL");
    }
    if (fixture.variant === "false") expect(mutation.body?.can_send).toBe(false);
    if (fixture.variant === "0") expect(mutation.body?.daily_send_quota).toBe(0);
    if (fixture.variant === "[]") expect(mutation.body?.allowed_zone_ids).toEqual([]);
    return;
  }
  throw new Error("No genuine shipping component consumer for this fixture case");
}, 30000);
