import React from "react";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { SidebarProvider } from "@/components/ui/sidebar";
import PermissionsPage from "@/features/company/profile-management";
import { updatePermissionProfile, listPermissionProfiles } from "@/lib/api";
import { validateObservedPermissionProfile } from "@/lib/api/permission-editor-types";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";

// Host identity is supplied; API functions, session, SWR, controls, fetch,
// shipping HTTP handlers and PostgreSQL are never response-mocked.
const host = vi.hoisted(() => ({ user: null as AuthUser | null }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ user: host.user, level: host.user?.role ?? "user", tenantId: host.user?.tenant_id ?? null }) }));
interface Fixture {
  schema_version: number; case_id: string; variant: string; case_sha256: string;
  api_url: string; auth: { token: string; user: AuthUser };
  profile_id: string; profile_name: string; probe_live?: boolean; input: Record<string, unknown>;
}
const path = process.env.TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE;
if (!path) throw new Error("Explicit Go-owned protocol component fixture is required; no skip or response fallback");
const fixture = JSON.parse(readFileSync(path, "utf8")) as Fixture;
const raw = readFileSync(resolve(process.cwd(), "../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"));
const cases = (JSON.parse(raw.toString()) as { cases: { id: string; input: unknown;  }[] }).cases;
const shared = cases.find(c => c.id === fixture.case_id);
const origin = new URL(fixture.api_url);
if (fixture.schema_version !== 1 || !shared || fixture.case_sha256 !== createHash("sha256").update(raw).digest("hex") || JSON.stringify(shared.input) !== JSON.stringify(fixture.input) || origin.protocol !== "http:" || origin.hostname !== "127.0.0.1") {
  throw new Error("Invalid source hash, shared input, or non-loopback Go-owned fixture");
}
let sseText = "";
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
    if (response.headers.get("content-type")?.includes("text/event-stream")) {
      const reader = response.clone().body!.getReader(); const decoder = new TextDecoder();
      void (async () => { try { while (true) { const chunk = await reader.read(); if (chunk.done) break; sseText += decoder.decode(chunk.value, { stream: true }); } } catch { /* cleanup abort */ } finally { reader.releaseLock(); } })();
    }
    if (fixture.probe_live && response.headers.get("content-type")?.includes("text/event-stream")) {
      calls.push({ method: init?.method ?? "GET", path: target.pathname, status: response.status });
      return response;
    }
    let data: unknown; try { data = await response.clone().json(); } catch { /* real 204/non-JSON */ }
    calls.push({ method: init?.method ?? "GET", path: target.pathname, body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined, status: response.status, data });
    return response;
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); delete process.env.NEXT_PUBLIC_API_URL; });
async function rowDialog(text: string, action: string) {
  const cell = await screen.findByText(text); const row = cell.closest("tr");
  const button = row?.querySelector<HTMLButtonElement>('[data-slot="dropdown-menu-trigger"]');
  if (!button) throw new Error("Actual shipping table action trigger missing");
  await userEvent.click(button); await userEvent.click(await screen.findByRole("menuitem", { name: action }));
  return await screen.findByRole("dialog");
}
// Self-authored diagnostic; never a formal catalog consumer or qualification.
test("PE03 real shipping stale guard and independent HTTP CAS", async () => {
  if (!fixture.profile_id || !fixture.profile_name) throw new Error("Profile fixture required");
  render(<SidebarProvider><PermissionsPage /></SidebarProvider>);
  if (fixture.probe_live) {
    await waitFor(() => expect(sseText.includes("event: ready")).toBe(true));
    await waitFor(() => expect(calls.some(c => c.path.endsWith("/overview") && c.status === 200)).toBe(true));
  }
  const dialog = await rowDialog(fixture.profile_name, "Edit");
  const input = within(dialog).getByPlaceholderText("Profile description (optional)");
  const save = within(dialog).getByRole("button", { name: /^Save$/ });
  const observed = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id)!;
  validateObservedPermissionProfile(observed);
  expect(observed.can_send).toBe(true);
  const alertBeforeRevocation = Boolean(within(dialog).queryByRole("alert"));
  const revoked = (await updatePermissionProfile(fixture.profile_id, { expected_revision: observed.revision, fields: { can_send: false } })).data;
  expect(revoked.can_send).toBe(false);
  expect(revoked.revision).not.toBe(observed.revision);
  await userEvent.clear(input); await userEvent.type(input, "Shared stale description");
  if (!fixture.probe_live) expect(sseText).toBe("");
  if (fixture.probe_live) await waitFor(() => expect(sseText.includes('"action":"permission.profile.update"')).toBe(true), { timeout: 10000 });
  await within(dialog).findByRole("alert");
  expect(save).toBeDisabled();
  expect(input).toHaveValue("Shared stale description");
  expect(within(dialog).getAllByRole("switch")[0]).toHaveAttribute("aria-checked", "true");
  expect(within(dialog).getAllByRole("spinbutton").every(el => (el as HTMLInputElement).value === "0")).toBe(true);
  const alert = within(dialog).getByRole("alert");
  expect(alert).toHaveTextContent(/changed|refresh/i);
  await userEvent.click(save);
  expect(calls.filter(c => c.method === "PATCH")).toHaveLength(1);
  const afterUI = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id)!;
  expect(afterUI.can_send).toBe(false); expect(afterUI.revision).toBe(revoked.revision);
  expect(afterUI.description).toBe(observed.description);
  // Independent HTTP control sends real old security values and old CAS token.
  await expect(updatePermissionProfile(fixture.profile_id, { expected_revision: observed.revision,
    fields: { description: "Shared stale description", can_send: observed.can_send,
      daily_send_quota: observed.daily_send_quota, daily_receive_quota: observed.daily_receive_quota,
      max_mailboxes: observed.max_mailboxes, max_domains: observed.max_domains,
      allowed_zone_ids: observed.allowed_zone_ids ?? [], can_create_domains: observed.can_create_domains,
      can_create_routes: observed.can_create_routes, can_create_api_keys: observed.can_create_api_keys } })).rejects.toMatchObject({ error: { code: "CONFLICT" } });
  const stale = calls.filter(c => c.method === "PATCH").at(-1)!;
  expect(stale.status).toBe(409);
  expect(stale.body?.expected_revision).toBe(observed.revision); expect(stale.body?.can_send).toBe(true);
  const afterCAS = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id)!;
  expect(afterCAS.can_send).toBe(false); expect(afterCAS.revision).toBe(revoked.revision);
  expect(afterCAS.description).toBe(observed.description);
  // Supported recovery proves the shipping Save action works after deliberate review.
  await userEvent.click(within(dialog).getByRole("button", { name: /Refresh/i }));
  const review = await within(dialog).findByRole("checkbox");
  expect(save).toBeDisabled(); expect(input).toHaveValue("Shared stale description");
  expect(within(dialog).getAllByRole("switch")[0]).toHaveAttribute("aria-checked", "false");
  await userEvent.click(review); await waitFor(() => expect(save).toBeEnabled());
  await userEvent.click(save); await waitFor(() => expect(calls.filter(c => c.method === "PATCH")).toHaveLength(3));
  const saved = calls.filter(c => c.method === "PATCH").at(-1)!;
  expect(saved.status).toBe(200); expect(saved.body).toEqual({ description: "Shared stale description", expected_revision: revoked.revision });
  const afterSave = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id)!;
  expect(afterSave.can_send).toBe(false); expect(afterSave.description).toBe("Shared stale description");
  writeFileSync(process.env.TABMAIL_PE03_PROBE_SUMMARY!, JSON.stringify({ diagnostic: "PE03", transport: fixture.probe_live ? "live" : "original-buffered",
    alertBeforeRevocation, sseReady: sseText.includes("event: ready"), sseProfileChanged: sseText.includes('"action":"permission.profile.update"'), sseEmpty: sseText.length === 0,
    staleUI: "alert-disabled-draft-retained", patchStatuses: calls.filter(c => c.method === "PATCH").map(c => c.status),
    staleCAS: "409-CONFLICT-no-restoration", recoveredSave: "200-description-only-revocation-preserved" }, null, 2));
});
