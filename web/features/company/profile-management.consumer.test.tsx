import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig } from "swr";
import PermissionsPage from "./profile-management";
import { toast } from "sonner";
import { installSession } from "@/lib/session";

// Actual profile panel + Base UI + production API/session serializer. Fetch is a
// deterministic HTTP fixture, not PostgreSQL or browser acceptance evidence.
const auth = vi.hoisted(() => ({ level: "admin", tenantId: "10000000-0000-4000-8000-000000000001" }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => auth }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/components/layout/page-header", () => ({ PageHeader: ({ actions }: { actions: React.ReactNode }) => <header>{actions}</header> }));
vi.mock("@/lib/i18n", () => ({ useI18n: () => ({ t: (key: string) => key }) }));
const id = "40000000-0000-4000-8000-000000000001";
const user = "20000000-0000-4000-8000-000000000001";
const effective = { can_send: true, daily_send_quota: 0, daily_receive_quota: 0, max_mailboxes: 0, max_domains: 0, allowed_zone_ids: null, can_create_domains: false, can_create_routes: false, can_create_api_keys: false };
const revision = { user_id: user, tenant_id: auth.tenantId, user_revision: "9007199254740993", profile_id: id, profile_revision: "9007199254740995" };
let profile: Record<string, unknown>;
let calls: { method: string; path: string; body: any }[];
let rejectWrite: boolean;
let previewVersion: string;
let intercept: ((method: string, path: string, init?: RequestInit) => Promise<Response> | undefined) | undefined;
const response = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : data), { status, headers: { "Content-Type": "application/json" } });
beforeEach(() => {
  auth.level = "admin";
  profile = { ...effective, id, tenant_id: auth.tenantId, name: "Controlled", description: "Draft", revision: "9007199254740995", is_system: false, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" };
  calls = []; rejectWrite = false; intercept = undefined; previewVersion = "9007199254740995";
  installSession("profile-test", { id: "admin", tenant_id: auth.tenantId, email: "admin@test.invalid", display_name: "Admin", role: "admin" });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const method = init?.method ?? "GET";
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ path, method, body });
    const pending = intercept?.(method, path, init);
    if (pending) return pending;
    if (method !== "GET" && rejectWrite) return response({ error: { code: "CONFLICT", message: "stale" } }, 409);
    if (path.endsWith("/deletion-preview")) return response({ profile_id: id, profile_revision: previewVersion, members: [{ ...revision, profile_revision: previewVersion }], changes: [{ revision: { ...revision, profile_revision: previewVersion }, before: effective, after: { ...effective, can_send: false } }] });
    if (method === "DELETE") return new Response(null, { status: 204 });
    if (method === "PATCH") return response({ ...profile, ...body, revision: "9007199254740997" });
    if (path === "/api/v1/admin/permissions") return response([profile]);
    return response([]);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
function deferred() {
  let resolve!: (value: Response) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<Response>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
async function switchIdentity() {
  await act(async () => installSession("profile-test-new", { id: "new-admin", tenant_id: auth.tenantId, email: "new-admin@test.invalid", display_name: "New admin", role: "admin" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
}
function mount() { return render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}><PermissionsPage /></SWRConfig>); }
async function action(label: string) {
  await screen.findByText("Controlled");
  const row = screen.getByText("Controlled").closest("tr")!;
  await userEvent.click(within(row).getByRole("button"));
  await userEvent.click(await screen.findByRole("menuitem", { name: label }));
}
const writes = () => calls.filter(call => call.method !== "GET");
describe("formal profile management CAS consumer", () => {
  it("sends observed decimal revision, explicit false and zero in fields", async () => {
    mount(); await action("permissions.edit");
    const dialog = screen.getByRole("dialog");
    fireEvent.change(dialog.querySelectorAll("input")[0], { target: { value: "Updated" } });
    await userEvent.click(within(dialog).getAllByRole("switch")[0]);
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.save" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toMatchObject({ expected_revision: "9007199254740995", name: "Updated", can_send: false, daily_send_quota: 0, allowed_zone_ids: [] });
    expect(writes()[0].body).not.toHaveProperty("tenant_id");
    expect(writes()[0].body).not.toHaveProperty("fields");
  });
  it("keeps draft on conflict, blocks blind retry and requires manual revision refresh", async () => {
    mount(); await action("permissions.edit");
    const dialog = screen.getByRole("dialog");
    fireEvent.change(dialog.querySelectorAll("input")[0], { target: { value: "Preserved draft" } });
    rejectWrite = true;
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.save" }));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeDisabled());
    expect(within(dialog).getByDisplayValue("Preserved draft")).toBeInTheDocument();
    expect(writes()).toHaveLength(1);
    profile.revision = "9007199254740999"; rejectWrite = false;
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.refreshRevision" }));
    await within(dialog).findByRole("checkbox");
    expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeDisabled();
    await userEvent.click(within(dialog).getByRole("checkbox"));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeEnabled());
    expect(within(dialog).getByDisplayValue("Preserved draft")).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.save" }));
    await waitFor(() => expect(writes()).toHaveLength(2));
    expect(writes()[1].body.expected_revision).toBe("9007199254740999");
  });
  it("does not manufacture a missing legacy revision or permit unsafe quotas", async () => {
    delete profile.revision;
    mount(); await action("permissions.edit");
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeDisabled();
    profile.revision = "8";
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.refreshRevision" }));
    await within(dialog).findByRole("checkbox");
    expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeDisabled();
    await userEvent.click(within(dialog).getByRole("checkbox"));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeEnabled());
    fireEvent.change(dialog.querySelectorAll("input[type=number]")[0], { target: { value: "9007199254740993" } });
    expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });
  it("shows effective before/after and requires explicit revision-bound deletion confirmation", async () => {
    mount(); await action("permissions.delete");
    const dialog = screen.getByRole("dialog");
    await within(dialog).findByText("permissions.beforeDeletion");
    expect(within(dialog).getByText("permissions.afterDeletion")).toBeInTheDocument();
    const button = within(dialog).getByRole("button", { name: "permissions.delete" });
    expect(button).toBeDisabled();
    await userEvent.click(within(dialog).getByRole("checkbox"));
    await userEvent.click(button);
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toEqual({ expected_revision: previewVersion, confirmed_members: [revision] });
  });
  it("invalidates stale deletion preview and never automatically replays DELETE", async () => {
    mount(); await action("permissions.delete");
    const dialog = screen.getByRole("dialog");
    await within(dialog).findByRole("checkbox");
    rejectWrite = true;
    await userEvent.click(within(dialog).getByRole("checkbox"));
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.delete" }));
    await waitFor(() => expect(within(dialog).queryByRole("checkbox")).toBeNull());
    expect(writes()).toHaveLength(1);
    previewVersion = "9007199254740999"; rejectWrite = false;
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.refreshPreview" }));
    const check = await within(dialog).findByRole("checkbox");
    expect(check).not.toBeChecked();
    expect(writes()).toHaveLength(1);
    await userEvent.click(check);
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.delete" }));
    await waitFor(() => expect(writes()).toHaveLength(2));
    expect(writes()[1].body.expected_revision).toBe(previewVersion);
  });
  it.each(["resolve", "reject"] as const)("ignores old-session edit %s without clearing a new write lock", async outcome => {
    const old = deferred(), fresh = deferred();
    let count = 0; let oldSignal: AbortSignal | null | undefined;
    intercept = (method, _path, init) => {
      if (method !== "PATCH") return;
      count++;
      if (count === 1) { oldSignal = init?.signal; return old.promise; }
      expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer profile-test-new");
      return fresh.promise;
    };
    mount(); await action("permissions.edit");
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "permissions.save" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    await switchIdentity(); expect(oldSignal?.aborted).toBe(true);
    await action("permissions.edit");
    const dialog = screen.getByRole("dialog");
    fireEvent.change(dialog.querySelectorAll("input")[0], { target: { value: "New identity draft" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.save" }));
    await waitFor(() => expect(writes()).toHaveLength(2));
    await act(async () => { if (outcome === "resolve") old.resolve(response(profile)); else old.reject(new DOMException("session abort", "AbortError")); });
    expect(within(dialog).getByDisplayValue("New identity draft")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "permissions.saving" })).toBeDisabled();
    expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
    expect(writes()).toHaveLength(2);
    await act(async () => fresh.resolve(response({ ...profile, name: "New identity draft", revision: "9007199254740997" })));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(toast.success).toHaveBeenCalledTimes(1);
  });
  it.each(["resolve", "reject"] as const)("ignores old-session DELETE %s without clearing new preview/confirmation", async outcome => {
    const old = deferred(), fresh = deferred(); let count = 0; let oldSignal: AbortSignal | null | undefined;
    intercept = (method, _path, init) => {
      if (method !== "DELETE") return;
      if (++count === 1) { oldSignal = init?.signal; return old.promise; }
      expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer profile-test-new");
      return fresh.promise;
    };
    mount(); await action("permissions.delete");
    let dialog = screen.getByRole("dialog");
    await userEvent.click(await within(dialog).findByRole("checkbox"));
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.delete" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    await switchIdentity(); expect(oldSignal?.aborted).toBe(true);
    await action("permissions.delete"); dialog = screen.getByRole("dialog");
    await userEvent.click(await within(dialog).findByRole("checkbox"));
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.delete" }));
    await waitFor(() => expect(writes()).toHaveLength(2));
    await act(async () => { if (outcome === "resolve") old.resolve(new Response(null, { status: 204 })); else old.reject(new DOMException("session abort", "AbortError")); });
    expect(within(dialog).getByRole("checkbox")).toBeChecked();
    expect(within(dialog).getByRole("button", { name: "permissions.delete" })).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: "permissions.refreshPreview" })).toBeDisabled();
    expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
    await act(async () => fresh.resolve(new Response(null, { status: 204 })));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(writes()).toHaveLength(2); expect(toast.success).toHaveBeenCalledTimes(1);
  });
  it.each(["resolve", "reject"] as const)("ignores old-session create %s without clearing new draft/busy state", async outcome => {
    const old = deferred(), fresh = deferred(); let count = 0; let oldSignal: AbortSignal | null | undefined;
    intercept = (method, _path, init) => {
      if (method !== "POST") return;
      if (++count === 1) { oldSignal = init?.signal; return old.promise; }
      expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer profile-test-new");
      return fresh.promise;
    };
    mount(); await screen.findByText("Controlled");
    await userEvent.click(screen.getByRole("button", { name: "permissions.create" }));
    let dialog = screen.getByRole("dialog");
    fireEvent.change(dialog.querySelectorAll("input")[0], { target: { value: "Old create" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.create" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    await switchIdentity(); expect(oldSignal?.aborted).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: "permissions.create" }));
    dialog = screen.getByRole("dialog");
    fireEvent.change(dialog.querySelectorAll("input")[0], { target: { value: "New create" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.create" }));
    await waitFor(() => expect(writes()).toHaveLength(2));
    await act(async () => { if (outcome === "resolve") old.resolve(response(profile)); else old.reject(new DOMException("session abort", "AbortError")); });
    expect(within(dialog).getByDisplayValue("New create")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "permissions.creating" })).toBeDisabled();
    expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
    await act(async () => fresh.resolve(response({ ...profile, name: "New create" })));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(writes()).toHaveLength(2); expect(toast.success).toHaveBeenCalledTimes(1);
  });
  it("ignores an old-session revision refresh rejection without unlocking a newer refresh", async () => {
    const old = deferred(), fresh = deferred(); let armed = false, count = 0;
    intercept = (method, path, init) => {
      if (!armed || method !== "GET" || path !== "/api/v1/admin/permissions") return;
      armed = false;
      if (++count === 1) return old.promise;
      expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer profile-test-new");
      return fresh.promise;
    };
    mount(); await action("permissions.edit");
    armed = true;
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "permissions.refreshRevision" }));
    await waitFor(() => expect(count).toBe(1));
    await switchIdentity(); await action("permissions.edit");
    const dialog = screen.getByRole("dialog"); armed = true;
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.refreshRevision" }));
    await waitFor(() => expect(count).toBe(2));
    await act(async () => old.reject(new DOMException("session abort", "AbortError")));
    expect(within(dialog).getByRole("button", { name: "permissions.refreshRevision" })).toBeDisabled();
    expect(toast.error).not.toHaveBeenCalled(); expect(writes()).toHaveLength(0);
    await act(async () => fresh.resolve(response([profile])));
    const review = await within(dialog).findByRole("checkbox");
    expect(review).not.toBeChecked();
    expect(within(dialog).getByRole("button", { name: "permissions.save" })).toBeDisabled();
  });
  it("platform admin edits a global profile using its observed revision without changing scope", async () => {
    auth.level = "super_admin"; profile.tenant_id = null;
    mount(); await action("permissions.edit");
    const dialog = screen.getByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "permissions.save" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body.expected_revision).toBe("9007199254740995");
    expect(writes()[0].body).not.toHaveProperty("tenant_id");
  });
  it.each(["0", "01", "9223372036854775808", 9])("rejects malformed or non-string observed profile revision %j", async value => {
    profile.revision = value;
    mount(); await action("permissions.edit");
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "permissions.save" })).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });
  it.each([{ is_system: true }, { tenant_id: null }, { tenant_id: "10000000-0000-4000-8000-000000000002" }])("tenant admin cannot mutate protected scope %j", async protectedFields => {
    Object.assign(profile, protectedFields);
    mount(); await screen.findByText("Controlled");
    await userEvent.click(within(screen.getByText("Controlled").closest("tr")!).getByRole("button"));
    expect(await screen.findByRole("menuitem", { name: "permissions.edit" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("menuitem", { name: "permissions.delete" })).toHaveAttribute("aria-disabled", "true");
    expect(writes()).toHaveLength(0);
  });
});
