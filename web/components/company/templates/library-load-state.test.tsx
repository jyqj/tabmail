import React from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import TemplatesPage from "@/app/(dashboard)/company/templates/page";
import { installSession } from "@/lib/session";
import type { MailTemplate } from "@/lib/company";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const template: MailTemplate = {
  id: "welcome", name: "Welcome", revision: 4, retired: false,
  updated_at: "2026-10-08T00:00:00Z",
  draft: { subject: "Welcome subject", text_body: "Hello", html_body: "", variables: [] },
};
const otherTemplate = { ...template, id: "reminder", name: "Reminder" };
const prefix = "/api/v1/company/templates";
const empty = "No templates yet";
const retire = () => screen.getByRole("button", { name: "Retire (including queued sends)" });
const select = (name = template.name) => screen.getByRole("button", { name: new RegExp(`^${name} Draft revision`) });
type Call = { path: string; method: string; body: Record<string, unknown> };
let calls: Call[];
let catalog: MailTemplate[];
let readLibrary: () => Promise<Response>;
let retireTemplate: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
let user: ReturnType<typeof userEvent.setup>;
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: { code: status === 403 ? "FORBIDDEN" : "INTERNAL", message: `Synthetic library ${status}` } }), { status, headers: { "Content-Type": "application/json" } });
const writes = () => calls.filter(call => call.method !== "GET");
const reads = () => calls.filter(call => call.method === "GET" && call.path === prefix);
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
function changeAccount() {
  installSession("synthetic-other-library", { id: "other-admin", tenant_id: "other-company", role: "admin", email: "other@fixture.test", display_name: "Other" });
}
beforeEach(() => {
  calls = []; pending = []; catalog = structuredClone([template]); user = userEvent.setup();
  readLibrary = async () => json(catalog);
  retireTemplate = async call => {
    catalog = catalog.map(value => call.path === `${prefix}/${value.id}/retire`
      ? { ...value, retired: Boolean(call.body.retired), revision: value.revision + 1 } : value);
    return json({ updated: true });
  };
  installSession("synthetic-library-token", { id: "admin", tenant_id: "company", role: "admin", email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : {} };
    calls.push(call);
    if (call.method === "GET" && call.path === prefix) return readLibrary();
    if (call.method === "GET" && call.path === "/api/v1/company/mailboxes") return json([]);
    if (call.method === "POST" && call.path.endsWith("/retire")) return retireTemplate(call);
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup(); await act(async () => { pending.forEach(resolve => resolve(json([]))); });
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});
function RefreshLibrary() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => {
    void mutate(key => Array.isArray(key) && key[2] === "company-templates").catch(() => undefined);
  }}>Refresh library cache</button>;
}
function mount() {
  const cache = new Map();
  return render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}>
    <TemplatesPage /><RefreshLibrary />
  </SWRConfig>);
}
async function readyLibrary() {
  const view = mount(); await screen.findByRole("button", { name: /^Welcome Draft revision/ });
  await waitFor(() => expect(retire()).toBeEnabled());
  return view;
}
const refresh = () => fireEvent.click(screen.getByRole("button", { name: "Refresh library cache" }));

it("announces an initial library read without claiming that no templates exist", async () => {
  const gate = delayed(); readLibrary = () => gate.promise; mount();
  await waitFor(() => expect(reads()).toHaveLength(1));
  expect(screen.getByText("Loading template library…")).toHaveAttribute("role", "status");
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /^Welcome Draft revision/ })).not.toBeInTheDocument();
  await act(async () => gate.resolve(json([template])));
  await waitFor(() => expect(retire()).toBeEnabled());
});

it("shows an empty library only after a successful empty response", async () => {
  catalog = []; mount(); await screen.findByText(empty);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(writes()).toHaveLength(0);
});

it.each([403, 503])("does not describe an initial %s as an empty library and retries reads", async status => {
  readLibrary = async () => failed(status); mount(); await screen.findByRole("alert");
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retire (including queued sends)" })).not.toBeInTheDocument();
  readLibrary = async () => json([template]);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(retire()).toBeEnabled());
  expect(reads()).toHaveLength(2); expect(writes()).toHaveLength(0);
});

it.each([403, 503])("hides cached selection and retire controls after a %s refresh error", async status => {
  await readyLibrary(); readLibrary = async () => failed(status); refresh();
  await screen.findByRole("alert");
  expect(screen.queryByRole("button", { name: /^Welcome Draft revision/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retire (including queued sends)" })).not.toBeInTheDocument();
  expect(screen.queryByText(empty)).not.toBeInTheDocument(); expect(writes()).toHaveLength(0);
});

it("blocks stale library selection and retirement during a pending revalidation", async () => {
  await readyLibrary(); const gate = delayed(); readLibrary = () => gate.promise; refresh();
  await waitFor(() => expect(reads()).toHaveLength(2));
  expect(screen.getByText("Refreshing template library…")).toHaveAttribute("role", "status");
  expect(select()).toBeDisabled(); expect(retire()).toBeDisabled();
  fireEvent.click(select()); fireEvent.click(retire());
  expect(screen.queryByLabelText("Template name")).not.toBeInTheDocument(); expect(writes()).toHaveLength(0);
  await act(async () => gate.resolve(json([{ ...template, revision: 5 }])));
  await waitFor(() => expect(retire()).toBeEnabled());
});

it("does not retain an empty claim while an empty library is being revalidated", async () => {
  catalog = []; mount(); await screen.findByText(empty);
  const gate = delayed(); readLibrary = () => gate.promise; refresh();
  await waitFor(() => expect(reads()).toHaveLength(2));
  expect(screen.getByText("Refreshing template library…")).toHaveAttribute("role", "status");
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  await act(async () => gate.resolve(json([])));
  await screen.findByText(empty);
});

it("uses the newly confirmed library revision after a failed refresh and explicit retry", async () => {
  await readyLibrary(); readLibrary = async () => failed(403); refresh();
  await screen.findByRole("alert");
  expect(screen.queryByRole("button", { name: "Retire (including queued sends)" })).not.toBeInTheDocument();
  catalog = [{ ...template, revision: 9 }]; readLibrary = async () => json(catalog);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(retire()).toBeEnabled()); fireEvent.click(retire());
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(writes()[0]).toEqual({ path: `${prefix}/welcome/retire`, method: "POST", body: { revision: 9, retired: true } });
  await screen.findByRole("button", { name: "Reactivate" });
});

it("recovers an acknowledged retire whose readback fails without replaying the POST", async () => {
  await readyLibrary(); readLibrary = async () => failed(); fireEvent.click(retire());
  await screen.findByRole("alert");
  expect(screen.queryByRole("button", { name: "Retire (including queued sends)" })).not.toBeInTheDocument();
  expect(writes()).toHaveLength(1);
  readLibrary = async () => json(catalog);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await screen.findByRole("button", { name: "Reactivate" });
  expect(writes()).toHaveLength(1);
  expect(catalog[0]).toMatchObject({ revision: 5, retired: true });
});

it.each([false, true])("ignores a pending retire after the page unmounts (failure=%s)", async failure => {
  const gate = delayed(); retireTemplate = () => gate.promise;
  const view = await readyLibrary(); fireEvent.click(retire());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.unmount(); const count = reads().length;
  await act(async () => gate.resolve(failure ? failed() : json({ updated: true })));
  expect(reads()).toHaveLength(count);
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});

it("preserves explicit retire and reactivate requests with successive authoritative revisions", async () => {
  await readyLibrary(); fireEvent.click(retire());
  await screen.findByRole("button", { name: "Reactivate" });
  fireEvent.click(screen.getByRole("button", { name: "Reactivate" }));
  await waitFor(() => expect(writes()).toHaveLength(2));
  expect(writes()).toEqual([
    { path: `${prefix}/welcome/retire`, method: "POST", body: { revision: 4, retired: true } },
    { path: `${prefix}/welcome/retire`, method: "POST", body: { revision: 5, retired: false } },
  ]);
  await waitFor(() => expect(retire()).toBeEnabled());
});

it("preserves unsaved editor input when a background library read fails", async () => {
  await readyLibrary(); await user.click(select());
  fireEvent.change(screen.getByLabelText("Template name"), { target: { value: "Unsaved welcome" } });
  readLibrary = async () => failed(); refresh(); await screen.findByRole("alert");
  expect(screen.getByLabelText("Template name")).toHaveValue("Unsaved welcome");
  expect(writes()).toHaveLength(0);
});

it("does not clear a newer selected editor when an older retire completes", async () => {
  catalog = [template, otherTemplate];
  const gate = delayed(); retireTemplate = () => gate.promise;
  mount(); await screen.findByRole("button", { name: /^Welcome Draft revision/ });
  fireEvent.click(screen.getAllByRole("button", { name: "Retire (including queued sends)" })[0]);
  await waitFor(() => expect(writes()).toHaveLength(1));
  await user.click(select(otherTemplate.name));
  fireEvent.change(screen.getByLabelText("Template name"), { target: { value: "Current reminder" } });
  await act(async () => gate.resolve(json({ updated: true })));
  expect(screen.getByLabelText("Template name")).toHaveValue("Current reminder");
  expect(writes()).toHaveLength(1);
});

it("keeps a newer session independent from an older pending retire response", async () => {
  const gate = delayed(); retireTemplate = () => gate.promise;
  await readyLibrary(); fireEvent.click(retire());
  await waitFor(() => expect(writes()).toHaveLength(1));
  act(changeAccount); await waitFor(() => expect(reads()).toHaveLength(2));
  await act(async () => gate.resolve(json({ updated: true })));
  await waitFor(() => expect(retire()).toBeEnabled());
  expect(writes()).toHaveLength(1); expect(toast.error).not.toHaveBeenCalled();
});

it("identifies a confirmed status change separately from its failed readback", async () => {
  await readyLibrary(); readLibrary = async () => failed(); fireEvent.click(retire());
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent("Template status was saved, but the library could not be refreshed. Retry loading to check the current state.");
  expect(writes()).toHaveLength(1);
  readLibrary = async () => json(catalog);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await screen.findByRole("button", { name: "Reactivate" });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(writes()).toHaveLength(1);
});

it("reconciles a lost mutation response through GET before permitting another status change", async () => {
  await readyLibrary();
  retireTemplate = async () => {
    catalog = [{ ...template, retired: true, revision: 5 }];
    throw new TypeError("Synthetic response connection lost");
  };
  fireEvent.click(retire());
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent("The template status change could not be confirmed. Reload the library before trying again.");
  expect(screen.queryByRole("button", { name: "Retire (including queued sends)" })).not.toBeInTheDocument();
  expect(writes()).toHaveLength(1);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await screen.findByRole("button", { name: "Reactivate" });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(writes()).toHaveLength(1);
});

it("retains edits made in the same selected editor while retirement is pending", async () => {
  const gate = delayed(); retireTemplate = () => gate.promise;
  await readyLibrary(); await user.click(select());
  await user.click(screen.getByRole("tab", { name: "Template library" }));
  fireEvent.click(retire()); await waitFor(() => expect(writes()).toHaveLength(1));
  await user.click(screen.getByRole("tab", { name: "Editor: Welcome" }));
  fireEvent.change(screen.getByLabelText("Template name"), { target: { value: "Keep this newer draft" } });
  fireEvent.change(screen.getByLabelText("Text body template"), { target: { value: "Typed after retirement began" } });
  catalog = [{ ...template, retired: true, revision: 5 }];
  await act(async () => gate.resolve(json({ updated: true })));
  await waitFor(() => expect(reads()).toHaveLength(2));
  expect(screen.getByLabelText("Template name")).toHaveValue("Keep this newer draft");
  expect(screen.getByLabelText("Text body template")).toHaveValue("Typed after retirement began");
  expect(writes()).toHaveLength(1);
});

it("treats a malformed successful library response as a retryable read failure", async () => {
  class RenderBoundary extends React.Component<{ children: React.ReactNode }, { failed: boolean }> {
    state = { failed: false };
    static getDerivedStateFromError() { return { failed: true }; }
    render() { return this.state.failed ? <p>Unexpected library render failure</p> : this.props.children; }
  }
  readLibrary = async () => json({ templates: [template] });
  const cache = new Map();
  render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}>
    <RenderBoundary><TemplatesPage /></RenderBoundary>
  </SWRConfig>);
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent("Invalid template library response. Reload the library.");
  expect(screen.queryByText("Unexpected library render failure")).not.toBeInTheDocument();
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  readLibrary = async () => json([]);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await screen.findByText(empty);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(writes()).toHaveLength(0);
});
