import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { CompanyDomain, DomainVerification } from "@/lib/company";
import { CompanyDomainsSection } from "./domains";

// Real section, SWR, API adapter and session transport. Only the HTTP peer and
// notifications are controlled. Delayed responses deliberately ignore aborts.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const domainA: CompanyDomain = { id: "domain-a", domain: "mail.company.test", is_verified: false,
  mx_verified: false, dkim_enabled: false, txt_record: "tabmail-verify=company-a",
  expected_mx: "10 mx.company.test", created_at: "2026-10-07T00:00:00Z" };
const domainB: CompanyDomain = { ...domainA, id: "domain-b", domain: "other.company.test",
  txt_record: "tabmail-verify=company-b" };
const verification = (domain = domainA, detail = "Current TXT observation"): DomainVerification => ({
  ...domain, is_verified: true, mx_verified: true, dkim_enabled: true,
  dkim_host: `tabmail._domainkey.${domain.domain}`, dkim_record: "v=DKIM1; p=fixture-public-key",
  checks: { txt: { status: "pass", details: [detail] }, mx: { status: "pass" },
    spf: { status: "fail", details: ["Current SPF observation"] }, dkim: { status: "pass" }, dmarc: { status: "fail" } },
});
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (status = 503) => new Response(JSON.stringify({ error: {
  code: status === 403 ? "FORBIDDEN" : "INTERNAL", message: `Synthetic domain request ${status}`,
} }), { status, headers: { "Content-Type": "application/json" } });
type Call = { path: string; method: string; body: Record<string, unknown>; tenant: string | null;
  authorization: string | null; signal: AbortSignal | null | undefined };
type Operation = "add" | "verify" | "delete";
let calls: Call[];
let listReply: (call: Call) => Promise<Response>;
let writeReply: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
const reads = () => calls.filter(call => call.method === "GET");
const writes = () => calls.filter(call => call.method !== "GET");
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
function writeSuccess(operation: Operation) {
  return json(operation === "verify" ? verification() : operation === "add" ? domainB : {});
}
function changeAccount() {
  installSession("replacement-fixture-token", { id: "other-admin", tenant_id: "company", role: "admin",
    email: "other@fixture.test", display_name: "Other admin" });
}
beforeEach(() => {
  calls = []; pending = [];
  listReply = async () => json([domainA]);
  writeReply = async call => writeSuccess(call.method === "DELETE" ? "delete" : call.path.endsWith("/verify") ? "verify" : "add");
  installSession("original-fixture-token", { id: "admin", tenant_id: "company", role: "admin",
    email: "admin@fixture.test", display_name: "Admin" });
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : {}, tenant: headers.get("X-Tenant-ID"),
      authorization: headers.get("Authorization"), signal: init?.signal };
    calls.push(call);
    if (call.path === "/api/v1/company/domains" && call.method === "GET") return listReply(call);
    if (call.path.startsWith("/api/v1/company/domains") && call.method !== "GET") return writeReply(call);
    throw new Error(`Unexpected domain consumer request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => { pending.forEach(resolve => resolve(json([]))); });
  vi.unstubAllGlobals();
});
function RefreshControl() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => { void mutate(() => true).catch(() => undefined); }}>Refresh domain cache</button>;
}
function mount() {
  return render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}>
    <CompanyDomainsSection /><RefreshControl />
  </SWRConfig>);
}
const refresh = () => fireEvent.click(screen.getByRole("button", { name: "Refresh domain cache" }));
const add = () => screen.getByRole("button", { name: "Add domain" });
const input = () => screen.getByLabelText("Add a domain");
const edit = (value: string) => fireEvent.change(input(), { target: { value } });
function row(domain = domainA) {
  return screen.getByText(domain.domain).closest("div.space-y-3") as HTMLElement;
}
const verify = (domain = domainA) => within(row(domain)).getByRole("button", { name: "Verify" });
const remove = (domain = domainA) => within(row(domain)).getByRole("button", { name: "Delete" });
const expand = (domain = domainA) => fireEvent.click(within(row(domain)).getByRole("button", { name: "View DNS records" }));
async function loaded() { await screen.findByText(domainA.domain); await waitFor(() => expect(verify()).toBeEnabled()); }
async function start(operation: Operation) {
  await loaded();
  if (operation === "add") { edit("new.company.test"); fireEvent.click(add()); }
  else fireEvent.click(operation === "verify" ? verify() : remove());
  await waitFor(() => expect(writes()).toHaveLength(1));
}
const settle = async (gate: ReturnType<typeof delayed>, response: Response) => {
  await act(async () => { gate.resolve(response); await gate.promise; });
};
function silent() { expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); }

describe("company domain read and operation ownership", () => {
  it("announces the initial read and blocks writes until an authoritative list arrives", async () => {
    const gate = delayed(); listReply = () => gate.promise;
    mount(); await waitFor(() => expect(reads()).toHaveLength(1)); edit("new.company.test");
    expect(screen.getByText("Loading domains…")).toHaveAttribute("role", "status");
    expect(screen.queryByText("No domains")).not.toBeInTheDocument();
    expect(add()).toBeDisabled();
    await settle(gate, json([domainA])); await loaded(); expect(add()).toBeEnabled();
  });

  it("shows an empty state only after a successful empty response", async () => {
    listReply = async () => json([]); mount(); await screen.findByText("No domains");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    edit("new.company.test"); expect(add()).toBeEnabled(); expect(writes()).toHaveLength(0);
  });

  for (const status of [403, 503]) {
    it(`does not present an initial ${status} failure as an empty or writable list`, async () => {
      listReply = async () => failure(status); mount(); await screen.findByRole("alert");
      edit("new.company.test");
      expect(screen.queryByText("No domains")).not.toBeInTheDocument(); expect(add()).toBeDisabled();
      listReply = async () => json([domainA]);
      fireEvent.click(screen.getByRole("button", { name: "Retry loading" })); await loaded();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(add()).toBeEnabled();
      expect(reads()).toHaveLength(2); expect(writes()).toHaveLength(0);
    });

    it(`hides cached DNS details and actions after a ${status} refresh error until retry`, async () => {
      mount(); await loaded(); expand(); expect(screen.getByText(domainA.txt_record)).toBeVisible();
      listReply = async () => failure(status); refresh(); await screen.findByRole("alert");
      expect(screen.queryByText(domainA.txt_record)).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Verify" })).not.toBeInTheDocument();
      expect(screen.queryByText("No domains")).not.toBeInTheDocument();
      edit("new.company.test"); expect(add()).toBeDisabled();
      listReply = async () => json([domainA]);
      fireEvent.click(screen.getByRole("button", { name: "Retry loading" })); await loaded();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(writes()).toHaveLength(0);
    });
  }

  for (const empty of [false, true]) {
    it(`announces revalidation and blocks stale writes without an empty claim (empty=${empty})`, async () => {
      listReply = async () => json(empty ? [] : [domainA]); mount();
      if (empty) await screen.findByText("No domains"); else await loaded();
      const gate = delayed(); listReply = () => gate.promise; refresh();
      await waitFor(() => expect(reads()).toHaveLength(2)); edit("new.company.test");
      expect(screen.getByText("Refreshing domains…")).toHaveAttribute("role", "status");
      expect(screen.queryByText("No domains")).not.toBeInTheDocument(); expect(add()).toBeDisabled();
      if (!empty) { expect(verify()).toBeDisabled(); expect(remove()).toBeDisabled(); }
      await settle(gate, json([])); await screen.findByText("No domains"); expect(add()).toBeEnabled();
    });
  }

  it("keeps a newer list when an older overlapping GET finishes last", async () => {
    const first = delayed(); listReply = () => first.promise;
    mount(); await waitFor(() => expect(reads()).toHaveLength(1));
    listReply = async () => json([domainB]); refresh(); await screen.findByText(domainB.domain);
    await settle(first, json([domainA]));
    expect(screen.getByText(domainB.domain)).toBeVisible(); expect(screen.queryByText(domainA.domain)).not.toBeInTheDocument();
  });

  for (const operation of ["add", "verify", "delete"] as const) {
    for (const failed of [false, true]) {
      it(`has no notification or follow-up read after unmounting a pending ${operation} (failure=${failed})`, async () => {
        const gate = delayed(); writeReply = () => gate.promise;
        const view = mount(); await start(operation); view.unmount(); const count = reads().length;
        await settle(gate, failed ? failure() : writeSuccess(operation));
        expect(reads()).toHaveLength(count); silent();
      });
    }

    it(`does not finish the old ${operation} UI when its refresh crosses an account boundary`, async () => {
      const write = delayed(); writeReply = () => write.promise;
      mount(); await start(operation);
      const reload = delayed(); listReply = () => reload.promise;
      await settle(write, writeSuccess(operation)); await waitFor(() => expect(reads()).toHaveLength(2));
      listReply = async () => json([domainB]); act(changeAccount); await screen.findByText(domainB.domain);
      edit("replacement.company.test");
      await settle(reload, json([domainA]));
      expect(input()).toHaveValue("replacement.company.test"); expect(add()).toBeEnabled();
      expect(screen.queryByText(domainA.domain)).not.toBeInTheDocument(); silent();
    });
  }

  it("clears the previous account's local DNS expansion, observations and draft", async () => {
    mount(); await loaded(); expand(); fireEvent.click(verify());
    await screen.findByText("Current TXT observation"); await waitFor(() => expect(verify()).toBeEnabled());
    edit("old-draft.company.test"); act(changeAccount);
    await waitFor(() => expect(reads()).toHaveLength(3)); await loaded();
    expect(input()).toHaveValue("");
    expect(screen.queryByText("Current TXT observation")).not.toBeInTheDocument();
    expect(screen.queryByText(domainA.txt_record)).not.toBeInTheDocument();
  });

  it("lets the new account verify while the old account's ignored response cannot release its busy state", async () => {
    const oldWrite = delayed(); writeReply = () => oldWrite.promise;
    mount(); await start("verify");
    const newWrite = delayed(); writeReply = () => newWrite.promise; act(changeAccount); await loaded();
    fireEvent.click(verify()); expect(writes()).toHaveLength(2);
    await settle(oldWrite, json(verification(domainA, "Old account observation")));
    expect(verify()).toBeDisabled(); expect(screen.queryByText("Old account observation")).not.toBeInTheDocument(); silent();
    await settle(newWrite, json(verification(domainA, "New account observation")));
    await screen.findByText("New account observation"); await waitFor(() => expect(verify()).toBeEnabled());
    expect(toast.success).toHaveBeenCalledExactlyOnceWith("Verification executed");
    expect(writes()[1].authorization).toBe("Bearer replacement-fixture-token");
  });

  it("preserves the DNS panel selected while another domain is being deleted", async () => {
    listReply = async () => json([domainA, domainB]);
    const gate = delayed(); writeReply = () => gate.promise;
    mount(); await loaded(); expand(); fireEvent.click(remove()); expand(domainB);
    expect(screen.getByText(domainB.txt_record)).toBeVisible();
    listReply = async () => json([domainB]); await settle(gate, writeSuccess("delete"));
    await waitFor(() => expect(screen.queryByText(domainA.domain)).not.toBeInTheDocument());
    expect(screen.getByText(domainB.txt_record)).toBeVisible();
    expect(toast.success).toHaveBeenCalledExactlyOnceWith("Domain deleted");
  });

  it("does not erase the next domain typed while the first addition is pending", async () => {
    const gate = delayed(); writeReply = () => gate.promise;
    mount(); await start("add"); edit("second.company.test");
    await settle(gate, writeSuccess("add")); await waitFor(() => expect(add()).toBeEnabled());
    expect(input()).toHaveValue("second.company.test"); expect(writes()[0].body).toEqual({ domain: "new.company.test" });
  });

  it("ignores verification completion after its observed domain list was replaced", async () => {
    const gate = delayed(); writeReply = () => gate.promise;
    mount(); await start("verify");
    listReply = async () => json([domainB]); refresh(); await screen.findByText(domainB.domain);
    const count = reads().length; await settle(gate, json(verification()));
    expect(reads()).toHaveLength(count); expect(screen.queryByText("Current TXT observation")).not.toBeInTheDocument(); silent();
  });

  it("ignores a verification error after its observed domain list was replaced", async () => {
    const gate = delayed(); writeReply = () => gate.promise;
    mount(); await start("verify");
    listReply = async () => json([domainB]); refresh(); await screen.findByText(domainB.domain);
    const count = reads().length; await settle(gate, failure());
    expect(reads()).toHaveLength(count); expect(verify(domainB)).toBeEnabled(); silent();
  });

  it("does not submit a confirmed old-domain deletion under a newly installed account", async () => {
    mount(); await loaded();
    vi.spyOn(window, "confirm").mockImplementation(() => { changeAccount(); return true; });
    fireEvent.click(remove()); await act(async () => undefined);
    expect(writes()).toHaveLength(0); silent();
  });

  it("keeps current-domain verification, all DNS checks and an explicit error retry working", async () => {
    writeReply = async () => failure(); mount(); await start("verify");
    await waitFor(() => expect(toast.error).toHaveBeenCalledExactlyOnceWith("Synthetic domain request 503"));
    await waitFor(() => expect(verify()).toBeEnabled());
    writeReply = async () => json(verification());
    fireEvent.click(verify()); await screen.findByText("Current TXT observation");
    expect(screen.getByText("Current SPF observation")).toBeVisible();
    expect(screen.getAllByText("pass")).toHaveLength(3); expect(screen.getAllByText("fail")).toHaveLength(2);
    await waitFor(() => expect(toast.success).toHaveBeenCalledExactlyOnceWith("Verification executed"));
    expect(writes()).toHaveLength(2); expect(reads()).toHaveLength(2);
  });
});
