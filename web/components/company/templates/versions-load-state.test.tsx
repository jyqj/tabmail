import React, { useLayoutEffect, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { MailTemplate, TemplateVersion } from "@/lib/company";
import { TemplateVersionsView } from "./versions";

// Exercise the shipping component, SWR and request/session client. The
// controlled HTTP peer supplies delayed reads and records actual revocations.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const template: MailTemplate = {
  id: "template-one", name: "Welcome", revision: 7, retired: false,
  updated_at: "2026-10-07T00:00:00Z",
  draft: { subject: "Welcome subject", text_body: "Welcome body", html_body: "", variables: [] },
};
const version: TemplateVersion = {
  id: "version-one", template_id: template.id, name: template.name, version: 1,
  snapshot: template.draft, published_at: "2026-10-07T00:00:00Z", content_hash: "a".repeat(64),
};
const loading = "Loading version history…";
const refreshing = "Refreshing version history…";
const empty = "This template has no published versions";
type Call = { path: string; method: string; body: Record<string, unknown> };
let calls: Call[];
let listReply: (call: Call) => Promise<Response>;
let revokeReply: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: { code: status === 403 ? "FORBIDDEN" : "INTERNAL",
  message: `Synthetic version read ${status}` } }), { status, headers: { "Content-Type": "application/json" } });
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
const reads = () => calls.filter(call => call.method === "GET");
const writes = () => calls.filter(call => call.method !== "GET");
function changeAccount() {
  installSession("synthetic-other-token", { id: "other-admin", tenant_id: "other-company", role: "admin",
    email: "other@fixture.test", display_name: "Other admin" });
}
beforeEach(() => {
  calls = []; pending = [];
  listReply = async () => json([version]);
  revokeReply = async () => json({ revoked: true });
  installSession("synthetic-version-token", { id: "admin", tenant_id: "company", role: "admin",
    email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : {} };
    calls.push(call);
    if (call.method === "GET" && call.path.endsWith("/versions")) return listReply(call);
    if (call.method === "POST" && call.path.endsWith("/revoke")) return revokeReply(call);
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => { pending.forEach(resolve => resolve(json([]))); });
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function RevalidateControl() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => { void mutate(() => true).catch(() => undefined); }}>Revalidate history cache</button>;
}
function mount(initial: MailTemplate | null = template) {
  const cache = new Map();
  const onRevoked = vi.fn().mockResolvedValue(undefined);
  let choose!: (value: MailTemplate | null) => void;
  function Harness() {
    const [selected, setSelected] = useState(initial);
    useLayoutEffect(() => { choose = setSelected; }, []);
    return <SWRConfig value={{ provider: () => cache, dedupingInterval: 0,
      shouldRetryOnError: false, revalidateOnFocus: false }}>
      <TemplateVersionsView template={selected} refreshKey={0} onRevoked={onRevoked} />
      <RevalidateControl />
    </SWRConfig>;
  }
  const view = render(<Harness />);
  return { ...view, onRevoked, select: (value: MailTemplate | null) => { act(() => choose(value)); } };
}
const refresh = () => fireEvent.click(screen.getByRole("button", { name: "Revalidate history cache" }));
const revoke = () => screen.getByTestId("revoke-version-1");

it("requests no versions until a template is selected", () => {
  mount(null);
  expect(screen.getByText("Pick a template from the library to inspect its published versions.")).toBeInTheDocument();
  expect(reads()).toHaveLength(0);
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
});

it("announces a pending initial read without claiming empty history", async () => {
  const gate = delayed(); listReply = () => gate.promise;
  mount(); await waitFor(() => expect(reads()).toHaveLength(1));
  expect(screen.getByText(loading)).toHaveAttribute("role", "status");
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByTestId("revoke-version-1")).not.toBeInTheDocument();
  await act(async () => gate.resolve(json([version])));
  await waitFor(() => expect(revoke()).toBeEnabled());
  expect(screen.queryByText(loading)).not.toBeInTheDocument();
});

it("shows empty history after a successful empty response", async () => {
  listReply = async () => json([]);
  mount(); await screen.findByText(empty);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(writes()).toHaveLength(0);
});

it.each([403, 503])("does not label an initial %s error as empty and restores data on retry", async status => {
  listReply = async () => failed(status);
  mount(); await screen.findByRole("alert");
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  expect(screen.queryByTestId("revoke-version-1")).not.toBeInTheDocument();
  listReply = async () => json([version]);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(revoke()).toBeEnabled());
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(reads()).toHaveLength(2);
  expect(writes()).toHaveLength(0);
});

it.each([false, true])("announces cache revalidation without a stale action or empty claim (empty=%s)", async wasEmpty => {
  listReply = async () => json(wasEmpty ? [] : [version]);
  mount();
  if (wasEmpty) await screen.findByText(empty); else await waitFor(() => expect(revoke()).toBeEnabled());
  const gate = delayed(); listReply = () => gate.promise; refresh();
  await waitFor(() => expect(reads()).toHaveLength(2));
  expect(screen.getByText(refreshing)).toHaveAttribute("role", "status");
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  const old = screen.queryByTestId("revoke-version-1");
  if (old) expect(old).toBeDisabled();
  await act(async () => gate.resolve(json([])));
  await screen.findByText(empty);
  expect(screen.queryByText(refreshing)).not.toBeInTheDocument();
});

it.each([403, 503])("hides cached revocation after a %s refresh error until successful retry", async status => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
  mount(); await waitFor(() => expect(revoke()).toBeEnabled());
  listReply = async () => failed(status); refresh();
  await screen.findByRole("alert");
  expect(screen.queryByTestId("revoke-version-1")).not.toBeInTheDocument();
  expect(screen.queryByText(/Welcome body/)).not.toBeInTheDocument();
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  expect(confirm).not.toHaveBeenCalled(); expect(writes()).toHaveLength(0);
  listReply = async () => json([{ ...version, revoked_at: "2026-10-07T00:01:00Z" }]);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await screen.findByText(/Revoked/);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByTestId("revoke-version-1")).not.toBeInTheDocument();
  expect(reads()).toHaveLength(3); expect(writes()).toHaveLength(0);
});

it("revalidates a changed template revision before revoking with that revision", async () => {
  const view = mount(); await waitFor(() => expect(revoke()).toBeEnabled());
  const gate = delayed(); listReply = () => gate.promise;
  view.select({ ...template, revision: 8 });
  await waitFor(() => expect(reads()).toHaveLength(2));
  const old = screen.queryByTestId("revoke-version-1"); if (old) expect(old).toBeDisabled();
  await act(async () => gate.resolve(json([version])));
  await waitFor(() => expect(revoke()).toBeEnabled());
  listReply = async () => json([{ ...version, revoked_at: "2026-10-07T00:01:00Z" }]);
  vi.spyOn(window, "confirm").mockReturnValue(true); fireEvent.click(revoke());
  await waitFor(() => expect(view.onRevoked).toHaveBeenCalledOnce());
  expect(writes()).toEqual([{ path: "/api/v1/company/templates/template-one/versions/1/revoke", method: "POST", body: { revision: 8 } }]);
});

it("ignores a delayed read after selecting a different template", async () => {
  const gate = delayed(); listReply = () => gate.promise;
  const view = mount(); await waitFor(() => expect(reads()).toHaveLength(1));
  listReply = async () => json([]); view.select({ ...template, id: "template-two" });
  await screen.findByText(empty);
  await act(async () => gate.resolve(json([version])));
  expect(screen.getByText(empty)).toBeInTheDocument();
  expect(screen.queryByTestId("revoke-version-1")).not.toBeInTheDocument();
  expect(writes()).toHaveLength(0);
});

it("does not send an old revocation if the session changes during confirmation", async () => {
  mount(); await waitFor(() => expect(revoke()).toBeEnabled());
  vi.spyOn(window, "confirm").mockImplementation(() => { changeAccount(); return true; });
  fireEvent.click(revoke());
  await act(async () => undefined);
  expect(writes()).toHaveLength(0);
});

it("keeps the confirmed current-version request and refresh callback intact", async () => {
  const view = mount(); await waitFor(() => expect(revoke()).toBeEnabled());
  listReply = async () => json([{ ...version, revoked_at: "2026-10-07T00:01:00Z" }]);
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(true); fireEvent.click(revoke());
  await waitFor(() => expect(view.onRevoked).toHaveBeenCalledOnce());
  expect(confirm).toHaveBeenCalledWith(expect.stringMatching(/Irreversible:.*not started yet/));
  expect(writes()).toEqual([{ path: "/api/v1/company/templates/template-one/versions/1/revoke", method: "POST", body: { revision: 7 } }]);
  await screen.findByText(/Revoked/); expect(reads()).toHaveLength(2);
});

it.each([false, true])("has no stale read, callback or error after unmounting a pending revocation (failure=%s)", async failure => {
  const gate = delayed(); revokeReply = () => gate.promise;
  const view = mount(); await waitFor(() => expect(revoke()).toBeEnabled());
  vi.spyOn(window, "confirm").mockReturnValue(true); fireEvent.click(revoke());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.unmount(); const previousReads = reads().length;
  await act(async () => gate.resolve(failure ? failed() : json({ revoked: true })));
  expect(view.onRevoked).not.toHaveBeenCalled();
  expect(reads()).toHaveLength(previousReads);
  expect(toast.error).not.toHaveBeenCalled();
});
