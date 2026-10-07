import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { clearSessionCredentials, installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import UsersPage from "./user-management";

const notification = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: notification }));

// The page, AuthProvider, dialogs, API adapter and session transport are real.
// Fetch intentionally ignores invitation aborts: a server may already have
// committed a write when its initiating UI closes or changes identity.
const tenantA = "10000000-0000-4000-8000-000000000001";
const tenantB = "10000000-0000-4000-8000-000000000002";
const admin = (id = "first-admin", tenant_id = tenantA): AuthUser => ({
  id, tenant_id, email: `${id}@company.test`, display_name: id, role: "super_admin",
});
const effective = { can_send: false, daily_send_quota: 99, daily_receive_quota: 100,
  max_mailboxes: 2, max_domains: 3, allowed_zone_ids: [], can_create_domains: false,
  can_create_routes: false, can_create_api_keys: false };
const reply = (data: unknown) => new Response(JSON.stringify({ data }), {
  headers: { "Content-Type": "application/json" },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
type Invitation = ReturnType<typeof deferred<Response>> & {
  email: string; tenant: string | null; authorization: string | null;
  signal: AbortSignal | null | undefined;
};
let invitations: Invitation[];
let streams: ReturnType<typeof deferred<Response>>[];

beforeEach(() => {
  invitations = []; streams = [];
  installSession("old-fixture-token", admin());
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({
    matches: false, media: query, onchange: null, addListener() {}, removeListener() {},
    addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; },
  }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (path === "/api/v1/admin/invite") {
      expect(init?.method).toBe("POST");
      const headers = new Headers(init?.headers);
      const pending = { ...deferred<Response>(), email: JSON.parse(String(init?.body)).email,
        tenant: headers.get("X-Tenant-ID"), authorization: headers.get("Authorization"), signal: init?.signal };
      invitations.push(pending);
      return pending.promise;
    }
    if (path === "/api/v1/company/events") {
      const stream = deferred<Response>(); streams.push(stream);
      const abort = () => stream.reject(new DOMException("Aborted", "AbortError"));
      if (init?.signal?.aborted) abort();
      else init?.signal?.addEventListener("abort", abort, { once: true });
      return stream.promise;
    }
    if (path === "/api/v1/auth/me/permissions") return reply(effective);
    if (["/api/v1/admin/users", "/api/v1/admin/permissions", "/api/v1/domains"].includes(path)) return reply([]);
    throw new Error(`Unexpected invitation consumer request: ${init?.method ?? "GET"} ${path}`);
  });
});
afterEach(async () => {
  cleanup();
  // Release fixtures even after a failing assertion, without carrying pending
  // request leases or notifications into another case.
  await act(async () => {
    invitations.forEach((invitation, index) => invitation.resolve(success(invitation.email, index)));
    await Promise.all(invitations.map(invitation => invitation.promise));
  });
  vi.unstubAllGlobals();
});

function mount() {
  return render(<AuthProvider><SidebarProvider><UsersPage /></SidebarProvider></AuthProvider>);
}
async function open() {
  await screen.findByText("No users yet");
  await userEvent.click(await screen.findByRole("button", { name: "Invite Admin" }));
  return screen.findByRole("dialog");
}
const input = (dialog: HTMLElement) => within(dialog).getByPlaceholderText("user@example.com");
const setEmail = (dialog: HTMLElement, email: string) => fireEvent.change(input(dialog), { target: { value: email } });
const enter = (dialog: HTMLElement) => fireEvent.keyDown(input(dialog), { key: "Enter", code: "Enter" });
const send = (dialog: HTMLElement) => within(dialog).getByRole("button", { name: "Send Invite" });
const sending = (dialog: HTMLElement) => within(dialog).getByRole("button", { name: "Sending…" });
async function close(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole("button", { name: "关闭" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
}
async function start(email = "first@company.test") {
  const dialog = await open(); setEmail(dialog, email); enter(dialog);
  await waitFor(() => expect(invitations.length).toBeGreaterThan(0));
  return dialog;
}
function success(email: string, index: number) {
  return reply({ id: `invitation-${index}`, email: email.toLowerCase(), invite_code: `fixture-code-${index}`,
    expires_at: "2026-10-09T00:00:00Z" });
}
async function settle(index: number, outcome: "success" | "error" = "success") {
  const invitation = invitations[index];
  const response = outcome === "success" ? success(invitation.email, index) : new Response(JSON.stringify({
    error: { code: "CONFLICT", message: `Invitation ${index} rejected` },
  }), { status: 409, headers: { "Content-Type": "application/json" } });
  await act(async () => { invitation.resolve(response); await invitation.promise; });
}
function expectSilent() {
  expect(notification.success).not.toHaveBeenCalled();
  expect(notification.error).not.toHaveBeenCalled();
}

describe("administrator invitation ownership through the real users page", () => {
  it("submits the reviewed email under its session and shows the successful invitation", async () => {
    mount(); const dialog = await start("  Fresh@company.test  ");
    expect(invitations).toHaveLength(1);
    expect(invitations[0]).toMatchObject({ email: "Fresh@company.test", tenant: tenantA,
      authorization: "Bearer old-fixture-token" });
    expect(sending(dialog)).toBeDisabled();
    await settle(0);
    expect(within(dialog).getByText("fresh@company.test")).toBeVisible();
    expect(within(dialog).getByText("fixture-code-0")).toBeVisible();
    expect(notification.success).toHaveBeenCalledExactlyOnceWith("Invitation sent");
  });

  it("starts a fresh form after closing the successful result with its footer button", async () => {
    mount(); const dialog = await start(); await settle(0);
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const next = await open();
    expect(input(next)).toHaveValue("");
    expect(within(next).queryByText("fixture-code-0")).not.toBeInTheDocument();
    expect(send(next)).toBeDisabled();
  });

  it("coalesces rapid Enter, repeated Enter and click while an invitation is pending", async () => {
    mount(); const dialog = await open(); setEmail(dialog, "first@company.test");
    const button = send(dialog);
    act(() => { enter(dialog); enter(dialog); fireEvent.click(button); });
    expect(invitations).toHaveLength(1);
    await settle(0);
    expect(notification.success).toHaveBeenCalledTimes(1);
  });

  for (const outcome of ["success", "error"] as const) {
    it(`preserves continuing input and ignores the prior email's ${outcome}`, async () => {
      mount(); const dialog = await start(); setEmail(dialog, "second@company.test");
      await settle(0, outcome);
      expect(input(dialog)).toHaveValue("second@company.test");
      expect(send(dialog)).toBeEnabled();
      expectSilent();
      fireEvent.click(send(dialog)); expect(invitations).toHaveLength(2);
      expect(invitations[1].email).toBe("second@company.test");
      await settle(1);
      expect(within(dialog).getByText("fixture-code-1")).toBeVisible();
    });

    it(`ignores an invitation's late ${outcome} after close and reopen`, async () => {
      mount(); const dialog = await start(); await close(dialog);
      const next = await open(); setEmail(next, "second@company.test");
      await settle(0, outcome);
      expect(input(next)).toHaveValue("second@company.test");
      expect(send(next)).toBeEnabled();
      expectSilent();
    });

    it(`does not let an old ${outcome} unlock or replace a newer pending invitation`, async () => {
      mount(); const dialog = await start(); await close(dialog);
      const next = await open(); setEmail(next, "second@company.test"); enter(next);
      expect(invitations).toHaveLength(2);
      await settle(0, outcome);
      expect(input(next)).toHaveValue("second@company.test");
      expect(sending(next)).toBeDisabled();
      expectSilent();
      await settle(1);
      expect(within(next).getByText("fixture-code-1")).toBeVisible();
      expect(notification.success).toHaveBeenCalledExactlyOnceWith("Invitation sent");
    });

    it(`preserves the new result when the abandoned invitation finishes with ${outcome} last`, async () => {
      mount(); const dialog = await start(); await close(dialog);
      const next = await open(); setEmail(next, "second@company.test"); enter(next);
      expect(invitations).toHaveLength(2);
      await settle(1); await settle(0, outcome);
      expect(within(next).getByText("fixture-code-1")).toBeVisible();
      expect(within(next).queryByText("fixture-code-0")).not.toBeInTheDocument();
      expect(notification.success).toHaveBeenCalledExactlyOnceWith("Invitation sent");
      expect(notification.error).not.toHaveBeenCalled();
    });

    it(`does not notify after unmount when the pending invitation finishes with ${outcome}`, async () => {
      const page = mount(); await start(); page.unmount();
      await settle(0, outcome); expectSilent();
    });
  }

  it("requires a fresh submit after editing away from and back to the submitted email", async () => {
    mount(); const dialog = await start();
    setEmail(dialog, "second@company.test"); setEmail(dialog, "first@company.test");
    await settle(0);
    expect(input(dialog)).toHaveValue("first@company.test");
    expect(send(dialog)).toBeEnabled();
    expectSilent();
  });

  it("releases a closed dialog's busy state immediately for a new explicit invitation", async () => {
    mount(); const dialog = await start(); await close(dialog);
    const next = await open(); setEmail(next, "second@company.test");
    expect(send(next)).toBeEnabled();
    fireEvent.click(send(next)); expect(invitations).toHaveLength(2);
    await settle(1); await settle(0);
    expect(within(next).getByText("fixture-code-1")).toBeVisible();
  });

  for (const boundary of ["account", "tenant", "logout", "role"] as const) {
    it(`discards the old invitation and its abort notification at a ${boundary} boundary`, async () => {
      mount(); await start();
      act(() => {
        if (boundary === "logout") clearSessionCredentials();
        else if (boundary === "role") installSession("fresh-fixture-token", { ...admin(), role: "admin" });
        else installSession("fresh-fixture-token", admin(boundary === "account" ? "second-admin" : "first-admin",
          boundary === "tenant" ? tenantB : tenantA));
      });
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(invitations[0].signal?.aborted).toBe(true);
      await settle(0);
      expectSilent();
      if (boundary === "logout" || boundary === "role") {
        expect(screen.queryByRole("button", { name: "Invite Admin" })).not.toBeInTheDocument();
        act(() => installSession("replacement-fixture-token", admin("second-admin", tenantB)));
      }
      const next = await open();
      expect(input(next)).toHaveValue("");
      expect(within(next).queryByText("fixture-code-0")).not.toBeInTheDocument();
      setEmail(next, "second@company.test"); fireEvent.click(send(next));
      expect(invitations).toHaveLength(2);
      await settle(1);
      expect(within(next).getByText("fixture-code-1")).toBeVisible();
    });
  }

  it("rejects an old dialog submit before React observes a changed session scope", async () => {
    mount(); const dialog = await open(); setEmail(dialog, "first@company.test");
    // Model the cross-tab interval before the storage event is delivered.
    localStorage.setItem("tabmail_tenant_id", tenantB);
    enter(dialog);
    expect(invitations).toHaveLength(0);
    expectSilent();
  });

  it("drops pending invitation authority when the company event stream is forbidden", async () => {
    mount(); await start();
    expect(streams).toHaveLength(1);
    await act(async () => {
      streams[0].resolve(new Response(null, { status: 403 })); await streams[0].promise;
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Invite Admin" })).not.toBeInTheDocument();
    await settle(0); expectSilent();
  });

  it("shows a current request error, preserves its email, and permits an explicit retry", async () => {
    mount(); const dialog = await start(); await settle(0, "error");
    expect(notification.error).toHaveBeenCalledExactlyOnceWith("Invitation 0 rejected");
    expect(input(dialog)).toHaveValue("first@company.test");
    expect(send(dialog)).toBeEnabled();
    fireEvent.click(send(dialog)); expect(invitations).toHaveLength(2);
    await settle(1);
    expect(within(dialog).getByText("fixture-code-1")).toBeVisible();
    expect(notification.success).toHaveBeenCalledExactlyOnceWith("Invitation sent");
  });
});
