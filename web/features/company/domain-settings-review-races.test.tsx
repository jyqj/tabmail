import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import DomainsPage from "@/app/(dashboard)/company/domains/page";
import { installSession, sessionScope } from "@/lib/session";
import type { CompanyDomain, CompanySettings } from "@/lib/company";

// Supplemental HTTP races keep the original 35-case frozen baseline intact.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("next/navigation", () => ({ usePathname: () => "/company/domains" }));
const tenant = "review-company";
const domain: CompanyDomain = { id: "review-domain", domain: "review.company.test", is_verified: true,
  mx_verified: true, dkim_enabled: false, txt_record: "review-token", expected_mx: "mx.company.test", created_at: "2026-10-08" };
const initial: CompanySettings = { tenant_id: tenant, name: "Saved company", primary_zone_id: domain.id,
  domain: domain.domain, revision: 5, mail_send_policy: "free" };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status: number) => new Response(JSON.stringify({ error: {
  code: status === 409 ? "CONFLICT" : "UNAVAILABLE", message: `Synthetic review ${status}`,
} }), { status, headers: { "Content-Type": "application/json" } });
let settingsReply: () => Promise<Response>;
let domainsReply: () => Promise<Response>;
let writeReply: () => Promise<Response>;
let requests: Array<{ resource: string; method: string }>;
let pending: Array<(response: Response) => void>;
const form = () => within(screen.getByRole("heading", { name: "Company primary domain" }).closest("section")!);
const name = () => screen.getByLabelText("Company name");
const save = () => screen.getByRole("button", { name: "Save company settings" });
const review = () => screen.getByRole("button", { name: "Review latest settings" });
const reads = (resource: string) => requests.filter(request => request.method === "GET" && request.resource === resource);
const writes = () => requests.filter(request => request.method === "PUT");
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; }); pending.push(resolve); return { promise, resolve };
}
function RefreshControl() {
  const { mutate } = useSWRConfig();
  return <>{["settings", "domains"].map(resource => <button key={resource} onClick={() => { void mutate(key =>
    Array.isArray(key) && key[1] === sessionScope() && key[2] === `company-${resource}`).catch(() => undefined); }}>Refresh {resource}</button>)}</>;
}
async function mount() {
  render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}><DomainsPage /><RefreshControl /></SWRConfig>);
  await waitFor(() => expect(name()).toHaveValue(initial.name)); await screen.findByRole("option", { name: domain.domain }); await settle();
  fireEvent.change(name(), { target: { value: "Preserved draft" } });
}
async function conflict() {
  writeReply = async () => failed(409); await mount(); fireEvent.click(save());
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Synthetic review 409"));
}
beforeEach(() => {
  requests = []; pending = [];
  settingsReply = async () => json(initial); domainsReply = async () => json([domain]);
  writeReply = async () => json({ ...initial, name: "Preserved draft", revision: 6 });
  installSession("review-token", { id: "review-admin", tenant_id: tenant, role: "admin", email: "review@example.test", display_name: "Review admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const resource = path.split("/").at(-1)!; const method = init?.method ?? "GET"; requests.push({ resource, method });
    if (path === "/api/v1/company/settings") return method === "PUT" ? writeReply() : settingsReply();
    if (path === "/api/v1/company/domains" && method === "GET") return domainsReply();
    throw new Error(`Unexpected review request: ${method} ${path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(resolve => resolve(json([])))); vi.unstubAllGlobals(); });

describe("company settings acknowledgement and overlapping authority reads", () => {
  it.each(["malformed", "old revision"])("keeps an acknowledged %s PUT separate from failed writes and requires review before another mutation", async invalid => {
    writeReply = async () => json(invalid === "malformed" ? {} : initial); await mount(); fireEvent.click(save());
    await waitFor(() => expect(toast.success).toHaveBeenCalledExactlyOnceWith("Company settings saved"));
    expect(toast.error).not.toHaveBeenCalled(); expect(name()).toHaveValue("Preserved draft"); expect(save()).toBeDisabled();
    expect(form().getByText("The settings were saved, but their current state could not be confirmed. Your draft is preserved; review before saving again.")).toBeVisible();
    expect(reads("settings")).toHaveLength(1); expect(writes()).toHaveLength(1);
    settingsReply = async () => json({ ...initial, revision: 6 }); fireEvent.click(review());
    await waitFor(() => expect(save()).toBeEnabled()); expect(name()).toHaveValue("Preserved draft"); expect(writes()).toHaveLength(1);
  });

  it("does not overwrite a newer observed settings revision with a delayed successful PUT response", async () => {
    const gate = delayed(); writeReply = () => gate.promise; await mount(); fireEvent.click(save());
    settingsReply = async () => json({ ...initial, name: "Latest administrator change", revision: 8 });
    fireEvent.click(screen.getByRole("button", { name: "Refresh settings" }));
    await waitFor(() => expect(reads("settings")).toHaveLength(2)); await settle();
    await act(async () => gate.resolve(json({ ...initial, name: "Preserved draft", revision: 6 })));
    expect(name()).toHaveValue("Preserved draft"); expect(save()).toBeDisabled(); expect(reads("settings")).toHaveLength(2);
    expect(toast.success).toHaveBeenCalledExactlyOnceWith("Company settings saved"); expect(toast.error).not.toHaveBeenCalled();
    fireEvent.click(review()); await waitFor(() => expect(save()).toBeEnabled());
    expect(within(form().getByRole("region", { name: "Latest saved settings" })).getByText("Latest administrator change")).toBeVisible();
  });

  it("retires an explicit review when the observed domains change A → B → A while its settings read is pending", async () => {
    await conflict(); const gate = delayed(); settingsReply = () => gate.promise; fireEvent.click(review());
    domainsReply = async () => json([{ ...domain, mx_verified: false }]); fireEvent.click(screen.getByRole("button", { name: "Refresh domains" }));
    await waitFor(() => expect(screen.getByRole("option", { name: "Previously selected domain (currently unavailable)" })).toBeVisible());
    domainsReply = async () => json([domain]); fireEvent.click(screen.getByRole("button", { name: "Refresh domains" }));
    await screen.findByRole("option", { name: domain.domain }); await settle();
    await act(async () => gate.resolve(json({ ...initial, revision: 6 })));
    expect(name()).toHaveValue("Preserved draft"); expect(save()).toBeDisabled(); expect(writes()).toHaveLength(1);
    settingsReply = async () => json({ ...initial, revision: 6 }); fireEvent.click(review()); await waitFor(() => expect(save()).toBeEnabled());
  });

  it("does not erase a later domains refresh failure with an older successful explicit domains response", async () => {
    await conflict(); const gate = delayed(); settingsReply = async () => json({ ...initial, revision: 6 }); domainsReply = () => gate.promise; fireEvent.click(review());
    domainsReply = async () => failed(503); fireEvent.click(screen.getByRole("button", { name: "Refresh domains" }));
    await waitFor(() => expect(screen.getAllByText("Synthetic review 503").length).toBeGreaterThan(0));
    await act(async () => gate.resolve(json([domain]))); expect(save()).toBeDisabled(); expect(name()).toHaveValue("Preserved draft"); expect(writes()).toHaveLength(1);
    expect(screen.getAllByText("Synthetic review 503").length).toBeGreaterThan(0);
  });

  it("does not publish malformed domains or unlock the draft after a conflict review", async () => {
    await conflict(); settingsReply = async () => json({ ...initial, revision: 6 }); domainsReply = async () => json([{ ...domain, mx_verified: "yes" }]);
    fireEvent.click(review()); await waitFor(() => expect(form().getByText("Could not confirm current company domains. Read and review again.")).toBeVisible());
    expect(name()).toHaveValue("Preserved draft"); expect(save()).toBeDisabled(); expect(screen.getByRole("option", { name: domain.domain })).toBeVisible(); expect(writes()).toHaveLength(1);
  });
});
