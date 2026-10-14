import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import DomainsPage from "@/app/(dashboard)/company/domains/page";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import type { CompanyDomain, CompanySettings } from "@/lib/company";

// The real company settings route, form, SWR and HTTP adapter run together.
// Delayed HTTP peers deliberately ignore aborts to exercise retired responses.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("next/navigation", () => ({ usePathname: () => "/company/domains" }));
const tenant = "settings-company";
const firstZone: CompanyDomain = { id: "zone-first", domain: "first.company.test", is_verified: true,
  mx_verified: true, dkim_enabled: false, txt_record: "verify-first", expected_mx: "mx.company.test", created_at: "2026-10-08" };
const secondZone = { ...firstZone, id: "zone-second", domain: "second.company.test" };
const initial: CompanySettings = { tenant_id: tenant, name: "Initial company", primary_zone_id: firstZone.id,
  domain: firstZone.domain, revision: 5, mail_send_policy: "free" };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: {
  code: status === 409 ? "CONFLICT" : status === 403 ? "FORBIDDEN" : "UNAVAILABLE",
  message: `Synthetic settings ${status}`,
} }), { status, headers: { "Content-Type": "application/json" } });
type Call = { path: string; method: string; body: Record<string, unknown>; tenant: string | null };
let calls: Call[];
let stored: CompanySettings | null;
let domains: CompanyDomain[];
let readSettings: () => Promise<Response>;
let readDomains: () => Promise<Response>;
let writeSettings: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
const puts = () => calls.filter(call => call.method === "PUT");
const reads = (resource: "settings" | "domains") => calls.filter(call => call.method === "GET" && call.path.endsWith(`/${resource}`));
const form = () => within(screen.getByRole("heading", { name: "Company primary domain" }).closest("section")!);
const name = () => screen.getByLabelText("Company name");
const zone = () => screen.getByLabelText("Verified primary domain");
const policy = () => screen.getByLabelText("Company default send policy");
const save = () => screen.getByRole("button", { name: "Save company settings" });
const review = () => screen.getByRole("button", { name: "Review latest settings" });
const change = (field: HTMLElement, value: string) => fireEvent.change(field, { target: { value } });
const draft = () => { change(name(), "My company draft"); change(zone(), secondZone.id); change(policy(), "disabled"); };
const expectDraft = () => { expect(name()).toHaveValue("My company draft"); expect(zone()).toHaveValue(secondZone.id); expect(policy()).toHaveValue("disabled"); };
const newer = (revision = 6): CompanySettings => ({ ...initial, name: "Other administrator company", revision, mail_send_policy: "template_required" });
function identity(id = "settings-admin", company = tenant, role: AuthUser["role"] = "admin") {
  installSession(`${id}-${company}-${role}`, { id, tenant_id: company, role, email: `${id}@example.test`, display_name: id });
}
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; }); pending.push(resolve); return { promise, resolve };
}
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
function RefreshControl() {
  const { mutate } = useSWRConfig();
  return <><button onClick={() => { void mutate(key => Array.isArray(key) && key[1] === sessionScope() && key[2] === "company-settings").catch(() => undefined); }}>Refresh settings cache</button>
    <button onClick={() => { void mutate(key => Array.isArray(key) && key[1] === sessionScope() && key[2] === "company-domains").catch(() => undefined); }}>Refresh zones cache</button></>;
}
function mount() {
  return render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}><DomainsPage /><RefreshControl /></SWRConfig>);
}
async function loaded() { await screen.findByRole("option", { name: secondZone.domain }); await waitFor(() => expect(name()).toHaveValue(stored?.name ?? "")); await settle(); }
async function conflict() {
  writeSettings = async () => failed(409); const view = mount(); await loaded(); draft(); fireEvent.click(save());
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Synthetic settings 409")); return view;
}
beforeEach(() => {
  calls = []; pending = []; stored = { ...initial }; domains = [{ ...firstZone }, { ...secondZone }]; identity();
  readSettings = async () => json(stored); readDomains = async () => json(domains);
  writeSettings = async call => {
    stored = { ...initial, ...call.body, tenant_id: call.tenant!, revision: Number(call.body.revision) + 1,
      domain: domains.find(item => item.id === call.body.primary_zone_id)!.domain } as CompanySettings;
    return json(stored);
  };
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : {}, tenant: new Headers(init?.headers).get("X-Tenant-ID") };
    calls.push(call);
    if (call.path === "/api/v1/company/settings") return call.method === "PUT" ? writeSettings(call) : readSettings();
    if (call.path === "/api/v1/company/domains" && call.method === "GET") return readDomains();
    throw new Error(`Unexpected company settings request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(resolve => resolve(json([])))); vi.unstubAllGlobals(); });

describe("company settings conflict recovery and draft ownership", () => {
  it("saves all company settings with the observed revision and adopts the acknowledged result", async () => {
    mount(); await loaded(); draft(); fireEvent.click(save());
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Company settings saved"));
    expect(puts()).toEqual([{ path: "/api/v1/company/settings", method: "PUT", tenant,
      body: { name: "My company draft", primary_zone_id: secondZone.id, revision: 5, mail_send_policy: "disabled" } }]);
    expectDraft(); expect(save()).toBeEnabled(); expect(screen.queryByRole("button", { name: "Review latest settings" })).not.toBeInTheDocument();
  });

  it("allows initial configuration at revision zero only after a successful null settings read", async () => {
    stored = null; mount(); await loaded(); draft(); fireEvent.click(save());
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Company settings saved")); expect(puts()[0].body.revision).toBe(0);
  });

  it("preserves a rejected non-conflict draft and permits an explicit retry without pretending it was saved", async () => {
    writeSettings = async () => failed(); mount(); await loaded(); draft(); fireEvent.click(save());
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Synthetic settings 503")); expectDraft(); expect(save()).toBeEnabled();
    expect(toast.success).not.toHaveBeenCalled(); expect(screen.queryByRole("button", { name: "Review latest settings" })).not.toBeInTheDocument();
    fireEvent.click(save()); await waitFor(() => expect(puts()).toHaveLength(2)); expect(puts().map(call => call.body.revision)).toEqual([5, 5]);
  });

  it("latches a CAS conflict, preserves every draft field and blocks repeated writes", async () => {
    await conflict(); expectDraft(); expect(save()).toBeDisabled(); fireEvent.click(save()); expect(puts()).toHaveLength(1);
    expect(review()).toBeEnabled(); expect(reads("settings")).toHaveLength(1); expect(reads("domains")).toHaveLength(1);
  });

  it("explicitly reads settings and domains, shows the current saved settings and preserves the reviewed draft for a new save", async () => {
    await conflict(); stored = newer(); fireEvent.click(review());
    await waitFor(() => expect(save()).toBeEnabled()); expectDraft();
    const latest = within(form().getByRole("region", { name: "Latest saved settings" }));
    expect(latest.getByText("Other administrator company")).toBeVisible();
    expect(latest.getByText(firstZone.domain)).toBeVisible();
    expect(reads("settings")).toHaveLength(2); expect(reads("domains")).toHaveLength(2); expect(puts()).toHaveLength(1);
    writeSettings = async () => json({ ...newer(7), name: "My company draft", primary_zone_id: secondZone.id,
      domain: secondZone.domain, mail_send_policy: "disabled" }); fireEvent.click(save());
    await waitFor(() => expect(puts()).toHaveLength(2)); expect(puts()[1].body).toEqual({
      name: "My company draft", primary_zone_id: secondZone.id, revision: 6, mail_send_policy: "disabled" });
  });

  it("does not release a CAS conflict when an ordinary SWR refresh returns a newer settings version", async () => {
    await conflict(); stored = newer(); fireEvent.click(screen.getByRole("button", { name: "Refresh settings cache" }));
    await waitFor(() => expect(reads("settings")).toHaveLength(2)); await settle(); expectDraft(); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it("does not synthesize revision zero while the actual initial settings request is pending", async () => {
    const gate = delayed(); readSettings = () => gate.promise; mount(); await screen.findByRole("option", { name: secondZone.domain });
    draft(); expect(save()).toBeDisabled(); fireEvent.click(save()); expect(puts()).toHaveLength(0);
  });

  for (const resource of ["settings", "zones"] as const) {
    for (const status of [403, 503]) {
      it(`blocks cached settings writes after a ${resource} ${status} failure and recovers through real reads`, async () => {
        mount(); await loaded(); draft();
        if (resource === "settings") readSettings = async () => failed(status); else readDomains = async () => failed(status);
        fireEvent.click(screen.getByRole("button", { name: `Refresh ${resource} cache` }));
        await waitFor(() => expect(screen.getAllByRole("alert").length).toBeGreaterThan(0));
        expectDraft(); expect(save()).toBeDisabled(); fireEvent.click(save()); expect(puts()).toHaveLength(0);
        readSettings = async () => json(stored); readDomains = async () => json(domains);
        fireEvent.click(form().getByRole("button", { name: "Retry loading" }));
        await waitFor(() => expect(save()).toBeEnabled()); expectDraft(); expect(puts()).toHaveLength(0);
      });
    }
  }

  it("blocks a dirty settings write while its current settings snapshot is being revalidated", async () => {
    mount(); await loaded(); draft(); const gate = delayed(); readSettings = () => gate.promise;
    fireEvent.click(screen.getByRole("button", { name: "Refresh settings cache" }));
    await waitFor(() => expect(reads("settings")).toHaveLength(2)); expect(save()).toBeDisabled();
    await act(async () => gate.resolve(json(stored))); await waitFor(() => expect(save()).toBeEnabled()); expectDraft();
  });

  it.each(["settings", "domains"] as const)("does not unlock a conflict when the explicit %s GET fails despite cached data", async resource => {
    await conflict(); stored = newer();
    if (resource === "settings") readSettings = async () => failed(); else readDomains = async () => failed();
    fireEvent.click(review()); await waitFor(() => expect(form().getByText("Synthetic settings 503")).toBeVisible());
    expectDraft(); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
    readSettings = async () => json(stored); readDomains = async () => json(domains); fireEvent.click(review());
    await waitFor(() => expect(save()).toBeEnabled()); expectDraft(); expect(reads("settings")).toHaveLength(3); expect(reads("domains")).toHaveLength(3);
  });

  it.each(["same revision", "wrong tenant", "missing revision", "null"])("rejects a %s explicit settings snapshot after a conflict", async invalid => {
    await conflict(); readSettings = async () => json(invalid === "same revision" ? initial : invalid === "wrong tenant" ?
      { ...newer(), tenant_id: "other-company" } : invalid === "missing revision" ? { ...newer(), revision: undefined } : null);
    fireEvent.click(review()); await waitFor(() => expect(review()).toBeEnabled()); await settle();
    expectDraft(); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
    expect(form().getByText("Could not confirm current company settings. Read and review again.")).toBeVisible();
  });

  it("does not roll back the highest settings revision observed during a conflict", async () => {
    await conflict(); stored = newer(8); fireEvent.click(screen.getByRole("button", { name: "Refresh settings cache" }));
    await waitFor(() => expect(reads("settings")).toHaveLength(2)); await settle();
    readSettings = async () => json(newer(7)); fireEvent.click(review()); await waitFor(() => expect(review()).toBeEnabled()); await settle();
    expectDraft(); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it("keeps an unavailable draft domain visible and requires a currently verified choice after review", async () => {
    await conflict(); stored = newer(); domains = [{ ...firstZone }, { ...secondZone, mx_verified: false }]; fireEvent.click(review());
    await waitFor(() => expect(reads("domains")).toHaveLength(2)); await settle();
    expect(name()).toHaveValue("My company draft"); expect(zone()).toHaveValue(secondZone.id); expect(save()).toBeDisabled();
    change(zone(), firstZone.id); expect(save()).toBeEnabled(); fireEvent.click(save()); await waitFor(() => expect(puts()).toHaveLength(2));
    expect(puts()[1].body.revision).toBe(6); expect(puts()[1].body.primary_zone_id).toBe(firstZone.id);
  });

  it.each(["name", "domain", "policy"])("keeps an A → B → A %s intent entered during an explicit review locked for another review", async field => {
    await conflict(); const gate = delayed(); readSettings = () => gate.promise; fireEvent.click(review());
    if (field === "name") { change(name(), "New draft"); change(name(), "My company draft"); }
    if (field === "domain") { change(zone(), firstZone.id); change(zone(), secondZone.id); }
    if (field === "policy") { change(policy(), "free"); change(policy(), "disabled"); }
    await act(async () => gate.resolve(json(newer()))); expectDraft(); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
    readSettings = async () => json(newer()); fireEvent.click(review()); await waitFor(() => expect(save()).toBeEnabled()); expectDraft();
  });

  it("rejects an explicit review overtaken by a newer settings refresh, including a later older cache response", async () => {
    await conflict(); const gate = delayed(); readSettings = () => gate.promise; fireEvent.click(review());
    readSettings = async () => json(newer(8)); fireEvent.click(screen.getByRole("button", { name: "Refresh settings cache" }));
    await waitFor(() => expect(reads("settings")).toHaveLength(3)); await settle();
    readSettings = async () => json(newer(6)); fireEvent.click(screen.getByRole("button", { name: "Refresh settings cache" }));
    await waitFor(() => expect(reads("settings")).toHaveLength(4)); await settle();
    await act(async () => gate.resolve(json(newer(7)))); expectDraft(); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it.each(["name", "domain", "policy"])("does not clear an A → B → A %s draft intent when an older PUT succeeds", async field => {
    const gate = delayed(); writeSettings = () => gate.promise; mount(); await loaded(); draft(); fireEvent.click(save());
    if (field === "name") { change(name(), "New draft"); change(name(), "My company draft"); }
    if (field === "domain") { change(zone(), firstZone.id); change(zone(), secondZone.id); }
    if (field === "policy") { change(policy(), "free"); change(policy(), "disabled"); }
    await act(async () => gate.resolve(json(newer()))); expectDraft(); expect(save()).toBeDisabled();
    expect(toast.success).toHaveBeenCalledExactlyOnceWith("Company settings saved"); expect(review()).toBeEnabled();
  });

  it.each(["account", "tenant", "role"])("lets a fully loaded new %s save while an old write cannot release its busy state", async boundary => {
    const first = delayed(); const second = delayed(); writeSettings = () => puts().length === 1 ? first.promise : second.promise;
    mount(); await loaded(); draft(); fireEvent.click(save()); const company = boundary === "tenant" ? "replacement-company" : tenant;
    stored = { ...initial, name: "Replacement company", tenant_id: company };
    act(() => identity(boundary === "account" ? "replacement-admin" : "settings-admin", company, boundary === "role" ? "super_admin" : "admin"));
    await waitFor(() => expect(name()).toHaveValue("Replacement company")); await waitFor(() => expect(save()).toBeEnabled());
    change(name(), "Replacement draft"); fireEvent.click(save()); await waitFor(() => expect(puts()).toHaveLength(2));
    await act(async () => first.resolve(json(newer()))); expect(name()).toHaveValue("Replacement draft"); expect(save()).toBeDisabled(); expect(toast.success).not.toHaveBeenCalled();
    await act(async () => second.resolve(json({ ...stored, name: "Replacement draft", revision: 6 })));
    expect(name()).toHaveValue("Replacement draft"); expect(toast.success).toHaveBeenCalledTimes(1); expect(puts()[1].tenant).toBe(company);
  });

  it("does not apply an old review after an A → B → A tenant session transition", async () => {
    await conflict(); const gate = delayed(); readSettings = () => gate.promise; fireEvent.click(review());
    readSettings = async () => json(stored); act(() => identity("replacement-admin", "other-company"));
    act(() => identity()); await waitFor(() => expect(name()).toHaveValue("Initial company")); await settle();
    change(name(), "Current session draft"); expect(save()).toBeEnabled();
    await act(async () => gate.resolve(json(newer()))); expect(name()).toHaveValue("Current session draft"); expect(save()).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Review latest settings" })).not.toBeInTheDocument(); expect(puts()).toHaveLength(1);
  });

  it("keeps the same active operation across token-only rotation", async () => {
    const gate = delayed(); writeSettings = () => gate.promise; mount(); await loaded(); draft(); fireEvent.click(save()); const before = sessionScope();
    act(() => { localStorage.setItem("tabmail_access_token", "rotated-settings-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(before); expect(save()).toBeDisabled();
    await act(async () => gate.resolve(json({ ...initial, name: "My company draft", primary_zone_id: secondZone.id,
      domain: secondZone.domain, mail_send_policy: "disabled", revision: 6 })));
    expectDraft(); expect(toast.success).toHaveBeenCalledExactlyOnceWith("Company settings saved");
  });

  it.each([false, true])("suppresses an unmounted settings write's notifications and readbacks (failure=%s)", async error => {
    const gate = delayed(); writeSettings = () => gate.promise; const view = mount(); await loaded(); draft(); fireEvent.click(save());
    view.unmount(); const count = reads("settings").length; await act(async () => gate.resolve(error ? failed() : json(newer())));
    expect(reads("settings")).toHaveLength(count); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
  });

  it("does not send duplicate settings mutations for synchronous repeated clicks", async () => {
    const gate = delayed(); writeSettings = () => gate.promise; mount(); await loaded(); draft(); const button = save();
    act(() => { fireEvent.click(button); fireEvent.click(button); }); expect(puts()).toHaveLength(1);
  });
});
