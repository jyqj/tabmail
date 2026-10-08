import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import DomainsPage from "@/app/(dashboard)/company/domains/page";
import { installSession, sessionScope } from "@/lib/session";
import type { CompanyDomain, CompanySettings } from "@/lib/company";

// The independently discovered truncated-JSON regression and its recovery
// keep the earlier 35 + 6 cases unchanged. The actual HTTP adapter is used.
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
let requests: Array<{ resource: string; method: string; body: Record<string, unknown> }>;
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
const truncated = () => new Response("{", { status: 200, headers: { "Content-Type": "application/json" } });
async function unconfirmed() {
  writeReply = async () => truncated(); await mount(); fireEvent.click(save());
  await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
}
beforeEach(() => {
  requests = []; pending = [];
  settingsReply = async () => json(initial); domainsReply = async () => json([domain]);
  writeReply = async () => json({ ...initial, name: "Preserved draft", revision: 6 });
  installSession("review-token", { id: "review-admin", tenant_id: tenant, role: "admin", email: "review@example.test", display_name: "Review admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const resource = path.split("/").at(-1)!; const method = init?.method ?? "GET";
    requests.push({ resource, method, body: typeof init?.body === "string" ? JSON.parse(init.body) : {} });
    if (path === "/api/v1/company/settings") return method === "PUT" ? writeReply() : settingsReply();
    if (path === "/api/v1/company/domains" && method === "GET") return domainsReply();
    throw new Error(`Unexpected review request: ${method} ${path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(resolve => resolve(json([])))); vi.unstubAllGlobals(); });

describe("company settings response parsing and explicit recovery", () => {
  it("keeps an unparseable HTTP 200 PUT locked until explicit readback", async () => {
    writeReply = async () => new Response("{", { status: 200, headers: { "Content-Type": "application/json" } });
    await mount(); fireEvent.click(save());
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(name()).toHaveValue("Preserved draft");
    expect(save()).toBeDisabled();
    expect(writes()).toHaveLength(1);
  });

  it("does not announce an unreadable response as saved and explicitly reads both resources before submitting the draft again", async () => {
    await unconfirmed(); expect(toast.success).not.toHaveBeenCalled();
    expect(form().getByText("Could not confirm whether the settings were saved. Your draft is preserved; read and review before trying again.")).toBeVisible();
    expect(reads("settings")).toHaveLength(1); expect(reads("domains")).toHaveLength(1);
    settingsReply = async () => json({ ...initial, name: "Confirmed server value", revision: 6 }); fireEvent.click(review());
    await waitFor(() => expect(save()).toBeEnabled()); expect(name()).toHaveValue("Preserved draft");
    expect(reads("settings")).toHaveLength(2); expect(reads("domains")).toHaveLength(2); expect(writes()).toHaveLength(1);
    writeReply = async () => json({ ...initial, name: "Preserved draft", revision: 7 }); fireEvent.click(save());
    await waitFor(() => expect(toast.success).toHaveBeenCalledExactlyOnceWith("Company settings saved")); expect(writes()).toHaveLength(2);
    expect(writes()[1].body.revision).toBe(6); expect(writes()[1].body.name).toBe("Preserved draft");
  });

  it("allows an explicit authoritative same-version read to resolve an unconfirmed result without inventing a successful write", async () => {
    await unconfirmed(); fireEvent.click(review()); await waitFor(() => expect(save()).toBeEnabled());
    expect(name()).toHaveValue("Preserved draft"); expect(toast.success).not.toHaveBeenCalled(); expect(writes()).toHaveLength(1);
    writeReply = async () => json({ ...initial, name: "Preserved draft", revision: 6 }); fireEvent.click(save());
    await waitFor(() => expect(writes()).toHaveLength(2)); expect(writes()[1].body.revision).toBe(5);
  });

  it.each(["settings", "domains"])("keeps an unconfirmed result locked when the explicit %s read fails", async resource => {
    await unconfirmed(); settingsReply = async () => json({ ...initial, revision: 6 });
    if (resource === "settings") settingsReply = async () => failed(503); else domainsReply = async () => failed(503);
    fireEvent.click(review()); await waitFor(() => expect(form().getByText("Synthetic review 503")).toBeVisible());
    expect(save()).toBeDisabled(); expect(name()).toHaveValue("Preserved draft"); expect(toast.success).not.toHaveBeenCalled(); expect(writes()).toHaveLength(1);
    settingsReply = async () => json({ ...initial, revision: 6 }); domainsReply = async () => json([domain]); fireEvent.click(review());
    await waitFor(() => expect(save()).toBeEnabled()); expect(writes()).toHaveLength(1);
  });

  it("keeps a newer A → B → A input locked when a recovery read for an unconfirmed result finishes", async () => {
    await unconfirmed(); const gate = delayed(); settingsReply = () => gate.promise; fireEvent.click(review());
    fireEvent.change(name(), { target: { value: "Next input" } }); fireEvent.change(name(), { target: { value: "Preserved draft" } });
    await act(async () => gate.resolve(json({ ...initial, revision: 6 })));
    expect(name()).toHaveValue("Preserved draft"); expect(save()).toBeDisabled(); expect(writes()).toHaveLength(1); expect(toast.success).not.toHaveBeenCalled();
  });

  it("does not misclassify an explicitly failed non-2xx response with unreadable JSON as a saved settings change", async () => {
    writeReply = async () => new Response("{", { status: 503, statusText: "Service Unavailable", headers: { "Content-Type": "application/json" } });
    await mount(); fireEvent.click(save()); await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Service Unavailable"));
    expect(name()).toHaveValue("Preserved draft"); expect(save()).toBeEnabled(); expect(toast.success).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Review latest settings" })).not.toBeInTheDocument(); expect(writes()).toHaveLength(1);
  });
});
