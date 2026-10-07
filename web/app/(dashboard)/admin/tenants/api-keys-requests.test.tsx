import React from "react";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { DEFAULT_API_KEY_SCOPES } from "@/lib/api-key-scopes";
import { installSession } from "@/lib/session";
import type { TenantAPIKey } from "@/lib/types";
import TenantsPage from "./page";

// Exercise the page, scope picker, SWR, API helpers and session boundary.
// Only presentation portals, sidebar chrome, HTTP and toast display are replaced.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/components/layout/page-header", () => ({
  PageHeader: ({ title, actions }: { title: string; actions?: React.ReactNode }) =>
    <header><h1>{title}</h1>{actions}</header>,
}));
vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, onOpenChange, children }: {
    open: boolean; onOpenChange: (open: boolean) => void; children: React.ReactNode;
  }) => open ? <div role="dialog">{children}
    <button onClick={() => onOpenChange(false)}>Close dialog</button>
  </div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <p>{children}</p>,
  DialogTrigger: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));
vi.mock("@/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: (props: React.ButtonHTMLAttributes<HTMLButtonElement>) => <button {...props} />,
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

type Call = { method: string; path: string; body: unknown };
const timestamp = "2026-10-08T00:00:00Z";
const tenants = ["a", "b"].map(id => ({
  id, name: `Tenant ${id.toUpperCase()}`, plan_id: "plan", is_super: false, created_at: timestamp,
}));
const key = (tenant: string, id = `key-${tenant}`): TenantAPIKey => ({
  id, tenant_id: tenant, key_prefix: `prefix-${id}`, label: "",
  scopes: [...DEFAULT_API_KEY_SCOPES], created_at: timestamp,
});
const created = (tenant: string) => ({
  ...key(tenant, `created-${tenant}`), key: `synthetic-secret-${tenant}`,
});
const json = (data: unknown) => new Response(JSON.stringify({ data }), {
  headers: { "Content-Type": "application/json" },
});
const failure = () => new Response(JSON.stringify({ error: {
  code: "FORBIDDEN", message: "Synthetic peer refused this request",
} }), { status: 403, headers: { "Content-Type": "application/json" } });
const keysPath = (tenant: string) => `/api/v1/admin/tenants/${tenant}/keys`;
let calls: Call[];
let replies: Map<string, Array<Response | Promise<Response>>>;
let lists: Map<string, TenantAPIKey[]>;
let releasePending: Array<() => void>;

function defer(reply: Response) {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  const release = () => resolve(reply);
  releasePending.push(release);
  return { promise, release };
}
function enqueue(method: string, tenant: string, ...responses: Array<Response | Promise<Response>>) {
  replies.set(`${method} ${keysPath(tenant)}`, responses);
}
const writes = () => calls.filter(call => call.method !== "GET");
const reads = (tenant: string) => calls.filter(call => call.method === "GET" && call.path === keysPath(tenant));

beforeEach(() => {
  calls = []; replies = new Map(); releasePending = [];
  lists = new Map([["a", [key("a")]], ["b", [key("b")]]]);
  installSession("synthetic-key-admin-token", {
    id: "admin", tenant_id: "super-tenant", role: "super_admin",
    email: "admin@fixture.test", display_name: "Synthetic admin",
  });
  vi.stubGlobal("confirm", vi.fn(() => true));
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = {
      method: init?.method ?? "GET", path: new URL(String(input), "http://localhost").pathname,
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
    };
    calls.push(call);
    const queued = replies.get(`${call.method} ${call.path}`)?.shift();
    if (queued) return await queued;
    if (call.method === "GET" && call.path === "/api/v1/admin/tenants") return json(tenants);
    if (call.method === "GET" && call.path === "/api/v1/admin/plans") return json([]);
    const target = call.path.match(/^\/api\/v1\/admin\/tenants\/([ab])\/keys(?:\/(.+))?$/);
    if (target) {
      const [, tenant, keyId] = target;
      if (call.method === "GET" && !keyId) return json(lists.get(tenant));
      if (call.method === "POST" && !keyId) {
        lists.set(tenant, [...(lists.get(tenant) ?? []), key(tenant, `created-${tenant}`)]);
        return json(created(tenant));
      }
      if (call.method === "DELETE" && keyId) {
        lists.set(tenant, (lists.get(tenant) ?? []).filter(value => value.id !== keyId));
        return new Response(null, { status: 204 });
      }
    }
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => releasePending.forEach(release => release()));
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function mount() {
  const cache = new Map();
  const view = render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0,
    revalidateOnFocus: false, shouldRetryOnError: false }}><TenantsPage /></SWRConfig>);
  await screen.findByText("Tenant A");
  return view;
}
async function open(tenant: "a" | "b", ready = true) {
  const row = screen.getByText(`Tenant ${tenant.toUpperCase()}`).closest("tr")!;
  await act(async () => fireEvent.click(within(row).getByRole("button", { name: "API Keys" })));
  if (ready) await screen.findByText(`prefix-key-${tenant}...`);
}
const close = () => fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
const generate = () => screen.getByRole("button", { name: "Generate Key" });
function revoke(tenant: string) {
  const row = screen.getByText(`prefix-key-${tenant}...`).parentElement!.parentElement!.parentElement!;
  return within(row).getByRole("button");
}
async function flush(release: () => void) { await act(async () => release()); }

describe("tenant API key request ownership and mutation outcomes", () => {
  it("lists the selected tenant and creates with the existing read-only defaults", async () => {
    await mount(); await open("a");
    await act(async () => fireEvent.click(generate()));
    expect(writes()).toEqual([{ method: "POST", path: keysPath("a"), body: { scopes: [...DEFAULT_API_KEY_SCOPES] } }]);
    expect(reads("a")).toHaveLength(2);
    expect(screen.getByText("synthetic-secret-a")).toBeInTheDocument();
    expect(screen.getByText("prefix-created-a...")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("API key created");
  });

  it("does not revoke after explicit cancellation", async () => {
    await mount(); await open("a");
    vi.mocked(window.confirm).mockReturnValue(false);
    await act(async () => fireEvent.click(revoke("a")));
    expect(writes()).toEqual([]);
    expect(screen.getByText("prefix-key-a...")).toBeInTheDocument();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("revokes the current tenant key once after explicit confirmation", async () => {
    await mount(); await open("a");
    await act(async () => fireEvent.click(revoke("a")));
    expect(writes()).toEqual([{ method: "DELETE", path: `${keysPath("a")}/key-a`, body: undefined }]);
    expect(screen.queryByText("prefix-key-a...")).not.toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("Key revoked");
  });

  it("blocks generation while the initial key list is unresolved", async () => {
    const pending = defer(json([key("a")])); enqueue("GET", "a", pending.promise);
    await mount(); await open("a", false);
    expect(generate()).toBeDisabled();
    fireEvent.click(generate());
    expect(writes()).toEqual([]);
    await flush(pending.release);
    expect(generate()).toBeEnabled();
  });

  it("shows a read failure instead of an authoritative empty list or enabled generation", async () => {
    enqueue("GET", "a", failure()); await mount(); await open("a", false);
    expect(screen.getByRole("alert")).toHaveTextContent("Failed to load keys");
    expect(screen.queryByText("No API keys")).not.toBeInTheDocument();
    expect(generate()).toBeDisabled();
    expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
    expect(writes()).toEqual([]);
  });

  it("retries a failed list using only GET and enables the current tenant after success", async () => {
    enqueue("GET", "a", failure()); await mount(); await open("a", false);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Retry" })));
    expect(reads("a")).toHaveLength(2);
    expect(screen.getByText("prefix-key-a...")).toBeInTheDocument();
    expect(generate()).toBeEnabled();
    expect(writes()).toEqual([]);
  });

  it("never exposes the previous tenant list when the next tenant read fails", async () => {
    await mount(); await open("a"); close();
    enqueue("GET", "b", failure()); await open("b", false);
    expect(screen.queryByText("prefix-key-a...")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Failed to load keys");
    expect(generate()).toBeDisabled();
  });

  it("ignores tenant A list completion after tenant B is ready", async () => {
    const pending = defer(json([key("a")])); enqueue("GET", "a", pending.promise);
    await mount(); await open("a", false); close(); await open("b");
    await flush(pending.release);
    expect(screen.getByText("prefix-key-b...")).toBeInTheDocument();
    expect(screen.queryByText("prefix-key-a...")).not.toBeInTheDocument();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("does not let an old read failure finish a new tenant loading state", async () => {
    const oldRead = defer(failure()); const currentRead = defer(json([key("b")]));
    enqueue("GET", "a", oldRead.promise); enqueue("GET", "b", currentRead.promise);
    await mount(); await open("a", false); close(); await open("b", false);
    await flush(oldRead.release);
    expect(toast.error).not.toHaveBeenCalled();
    expect(generate()).toBeDisabled();
    await flush(currentRead.release);
    expect(screen.getByText("prefix-key-b...")).toBeInTheDocument();
  });

  it("treats A to B to A as a new dialog lifetime even with the same tenant ID", async () => {
    const oldRead = defer(json([key("a", "obsolete-a")]));
    enqueue("GET", "a", oldRead.promise, json([key("a")]));
    await mount(); await open("a", false); close(); await open("b"); close(); await open("a");
    await flush(oldRead.release);
    expect(screen.getByText("prefix-key-a...")).toBeInTheDocument();
    expect(screen.queryByText("prefix-obsolete-a...")).not.toBeInTheDocument();
    expect(reads("a")).toHaveLength(2);
  });

  it("never displays A's late created secret or starts its follow-up GET inside B", async () => {
    const pending = defer(json(created("a"))); enqueue("POST", "a", pending.promise);
    await mount(); await open("a"); fireEvent.click(generate());
    close(); await open("b"); await flush(pending.release);
    expect(screen.queryByText("synthetic-secret-a")).not.toBeInTheDocument();
    expect(screen.getByText("prefix-key-b...")).toBeInTheDocument();
    expect(reads("a")).toHaveLength(1);
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("suppresses a late create failure after switching tenants", async () => {
    const pending = defer(failure()); enqueue("POST", "a", pending.promise);
    await mount(); await open("a"); fireEvent.click(generate());
    close(); await open("b"); await flush(pending.release);
    expect(toast.error).not.toHaveBeenCalled();
    expect(generate()).toBeEnabled();
  });

  it("does not refresh or notify for a create completed after closing the dialog", async () => {
    const pending = defer(json(created("a"))); enqueue("POST", "a", pending.promise);
    await mount(); await open("a"); fireEvent.click(generate()); close(); await flush(pending.release);
    expect(reads("a")).toHaveLength(1);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("does not reveal a prior lifetime secret after reopening the same tenant", async () => {
    const pending = defer(json(created("a"))); enqueue("POST", "a", pending.promise);
    await mount(); await open("a"); fireEvent.click(generate());
    close(); await open("a"); await flush(pending.release);
    expect(screen.queryByText("synthetic-secret-a")).not.toBeInTheDocument();
    expect(reads("a")).toHaveLength(2);
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("admits only one create during rapid clicks and keeps mutation controls locked", async () => {
    const pending = defer(json(created("a"))); enqueue("POST", "a", pending.promise, pending.promise);
    await mount(); await open("a");
    const button = generate();
    act(() => { fireEvent.click(button); fireEvent.click(button); });
    expect(writes()).toHaveLength(1);
    expect(button).toBeDisabled();
    expect(revoke("a")).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "domains:read" })).toBeDisabled();
  });

  it("keeps an acknowledged secret and retries only the list if the post-create read fails", async () => {
    enqueue("GET", "a", json([key("a")]), failure());
    await mount(); await open("a"); await act(async () => fireEvent.click(generate()));
    expect(screen.getByText("synthetic-secret-a")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("API key created");
    expect(toast.error).not.toHaveBeenCalledWith("Failed to create key");
    expect(screen.getByRole("alert")).toHaveTextContent("Failed to load keys");
    expect(generate()).toBeDisabled();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Retry" })));
    expect(writes()).toHaveLength(1);
    expect(reads("a")).toHaveLength(3);
    expect(screen.getByText("synthetic-secret-a")).toBeInTheDocument();
    expect(screen.getByText("prefix-created-a...")).toBeInTheDocument();
    expect(generate()).toBeEnabled();
  });

  it("admits only one revoke while its request is pending", async () => {
    const pending = defer(new Response(null, { status: 204 }));
    replies.set(`DELETE ${keysPath("a")}/key-a`, [pending.promise, pending.promise]);
    await mount(); await open("a"); const button = revoke("a");
    act(() => { fireEvent.click(button); fireEvent.click(button); });
    expect(writes()).toHaveLength(1);
    expect(window.confirm).toHaveBeenCalledOnce();
    expect(button).toBeDisabled();
    expect(generate()).toBeDisabled();
  });

  it("does not let a previous tenant revoke announce success or unlock B's create", async () => {
    const oldDelete = defer(new Response(null, { status: 204 }));
    const currentCreate = defer(json(created("b")));
    replies.set(`DELETE ${keysPath("a")}/key-a`, [oldDelete.promise]);
    enqueue("POST", "b", currentCreate.promise);
    await mount(); await open("a"); fireEvent.click(revoke("a"));
    close(); await open("b"); fireEvent.click(generate()); await flush(oldDelete.release);
    expect(toast.success).not.toHaveBeenCalled();
    expect(generate()).toBeDisabled();
    expect(screen.getByText("prefix-key-b...")).toBeInTheDocument();
  });

  it("ends a key dialog on a session change and suppresses the rejected old response", async () => {
    const pending = defer(json(created("a"))); enqueue("POST", "a", pending.promise);
    await mount(); await open("a"); fireEvent.click(generate());
    await act(async () => installSession("synthetic-other-session", {
      id: "other-admin", tenant_id: "other-company", role: "admin",
      email: "other@fixture.test", display_name: "Other synthetic admin",
    }));
    await flush(pending.release);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("synthetic-secret-a")).not.toBeInTheDocument();
    expect(toast.error).not.toHaveBeenCalled();
    expect(reads("a")).toHaveLength(1);
  });

  it("does not notify for a read rejected after the whole page unmounts", async () => {
    const pending = defer(failure()); enqueue("GET", "a", pending.promise);
    const view = await mount(); await open("a", false); view.unmount(); await flush(pending.release);
    expect(toast.error).not.toHaveBeenCalled();
    expect(writes()).toEqual([]);
  });

  it("sends deliberate custom scopes without restoring unchecked permissions", async () => {
    await mount(); await open("a");
    fireEvent.click(screen.getByRole("checkbox", { name: "domains:read" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "messages:write" }));
    await act(async () => fireEvent.click(generate()));
    expect(writes()).toEqual([{ method: "POST", path: keysPath("a"), body: {
      scopes: [...DEFAULT_API_KEY_SCOPES.filter(scope => scope !== "domains:read"), "messages:write"],
    } }]);
  });

  it("requires at least one explicitly selected scope", async () => {
    await mount(); await open("a");
    for (const scope of DEFAULT_API_KEY_SCOPES) fireEvent.click(screen.getByRole("checkbox", { name: scope }));
    expect(generate()).toBeDisabled(); fireEvent.click(generate());
    expect(writes()).toEqual([]);
  });

  it("keeps the current form usable after a definitive create rejection without replay", async () => {
    enqueue("POST", "a", failure()); await mount(); await open("a");
    await act(async () => fireEvent.click(generate()));
    expect(writes()).toHaveLength(1);
    expect(reads("a")).toHaveLength(1);
    expect(toast.error).toHaveBeenCalledWith("Failed to create key");
    expect(screen.queryByText("synthetic-secret-a")).not.toBeInTheDocument();
    expect(generate()).toBeEnabled();
  });
});
