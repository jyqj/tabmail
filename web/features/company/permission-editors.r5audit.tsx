import { readFileSync } from "node:fs";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import UsersPage from "./user-management";
import PermissionsPage from "./profile-management";
import { SidebarProvider } from "@/components/ui/sidebar";
import { getUserPermission, listPermissionProfiles, updatePermissionProfile } from "@/lib/api";
import { validateObservedPermissionProfile } from "@/lib/api/permission-editor-types";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";

// Only the host identity context is supplied by the test. Real React editors,
// Base UI controls, SWR, locale provider, session scope, API functions, fetch,
// HTTP authentication and PostgreSQL writes all remain in the execution path.
const host = vi.hoisted(() => ({ user: null as AuthUser | null }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ user: host.user, level: host.user?.role ?? "user", tenantId: host.user?.tenant_id ?? null }) }));

interface Fixture {
  case: "A01" | "A02";
  url: string;
  token: string;
  admin: AuthUser;
  employee_id: string;
  employee_email: string;
  zone_id: string;
  profile_id: string;
  profile_name: string;
}
const fixturePath = process.env.TABMAIL_R5_COMPONENT_FIXTURE;
if (!fixturePath) throw new Error("A disposable Go-owned component fixture is required");
const fixture: Fixture = JSON.parse(readFileSync(fixturePath, "utf8"));
const origin = new URL(fixture.url);
if (origin.protocol !== "http:" || origin.hostname !== "127.0.0.1" || !["A01", "A02"].includes(fixture.case)) {
  throw new Error("Invalid loopback-only component fixture");
}

const profileWriteStatuses: number[] = [];
beforeEach(() => {
  profileWriteStatuses.length = 0;
  process.env.NEXT_PUBLIC_API_URL = origin.origin;
  host.user = fixture.admin;
  installSession(fixture.token, fixture.admin);
  // jsdom lacks layout observers; no production UI component is replaced.
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({
    matches: false, media: query, onchange: null, addListener() {}, removeListener() {},
    addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; },
  }) });
  const realFetch = globalThis.fetch.bind(globalThis);
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const target = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    if (target.origin !== origin.origin) throw new Error("Component attempted a non-fixture network request");
    return realFetch(input, init).then(response => {
      if (init?.method === "PATCH" && target.pathname === `/api/v1/admin/permissions/${fixture.profile_id}`) profileWriteStatuses.push(response.status);
      return response;
    });
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); delete process.env.NEXT_PUBLIC_API_URL; });

async function openRowMenu(text: string, action: string) {
  const cell = await screen.findByText(text);
  const row = cell.closest("tr");
  if (!row) throw new Error("Expected real table row");
  const trigger = row.querySelector<HTMLButtonElement>('[data-slot="dropdown-menu-trigger"]');
  if (!trigger) throw new Error("Expected real row action trigger");
  await userEvent.click(trigger);
  await userEvent.click(await screen.findByRole("menuitem", { name: action }));
  return await screen.findByRole("dialog");
}

test(`${fixture.case} secure editor behavior`, async () => {
  const user = userEvent.setup();
  if (fixture.case === "A01") {
    const before = (await getUserPermission(fixture.employee_id)).data;
    expect(before.can_send).toBe(false);
    expect(before.allowed_zone_ids).toEqual([fixture.zone_id]);
    render(<SidebarProvider><UsersPage /></SidebarProvider>);
    const dialog = await openRowMenu(fixture.employee_email, "Permissions");
    await waitFor(() => expect(dialog.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(0));
    const quota = within(dialog).getAllByRole("spinbutton")[0];
    expect(quota).toBeInstanceOf(HTMLInputElement);
    await user.clear(quota);
    await user.type(quota, "25");
    await user.click(within(dialog).getByRole("button", { name: /^Save Overrides$/ }));
    await waitFor(async () => expect((await getUserPermission(fixture.employee_id)).data.daily_send_quota).toBe(25));
    const after = (await getUserPermission(fixture.employee_id)).data;
    if (after.can_send || after.allowed_zone_ids?.length !== 1 || after.allowed_zone_ids[0] !== fixture.zone_id) {
      throw new Error("R5_COMPONENT_DEFECT_A01: real quota-only editor save erased existing restrictions");
    }
  } else {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>);
    const dialog = await openRowMenu(fixture.profile_name, "Edit");
    const description = within(dialog).getByPlaceholderText("Profile description (optional)");
    // A separate administrator action AFTER opening the real editor. It goes
    // through the unmodified API client and server, not a response mock.
    const observed = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id);
    if (!observed) throw new Error("Actual profile GET omitted the fixture profile");
    validateObservedPermissionProfile(observed);
    const revoked = (await updatePermissionProfile(fixture.profile_id, { expected_revision: observed.revision, fields: { can_send: false } })).data;
    expect(revoked.can_send).toBe(false);
    expect(revoked.revision).not.toBe(observed.revision);
    await user.clear(description);
    await user.type(description, "R5 stale description");
    await user.click(within(dialog).getByRole("button", { name: /^Save$/ }));
    // The current CAS contract rejects this stale write. Do not wait for
    // the historical full-form update to succeed: that was the A02 defect.
    await waitFor(() => expect(within(dialog).getByRole("button", { name: /^Save$/ })).toBeDisabled());
    expect(profileWriteStatuses).toEqual([200, 409]);
    expect(description).toHaveValue("R5 stale description");
    expect(within(dialog).getByRole("button", { name: /Refresh revision/i })).toBeEnabled();
    const after = (await listPermissionProfiles()).data.find(p => p.id === fixture.profile_id);
    if (after?.can_send) throw new Error("R5_COMPONENT_DEFECT_A02: real stale editor restored revoked sending");
    expect(after).toBeDefined();
    expect(after?.revision).toBe(revoked.revision);
    expect(after?.description).toBe(revoked.description);
  }
});
