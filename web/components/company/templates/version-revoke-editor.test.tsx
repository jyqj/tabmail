import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import TemplatesPage from "@/app/(dashboard)/company/templates/page";
import { AUTH_EVENT, installSession } from "@/lib/session";
import type { MailTemplate, TemplateDraft, TemplateVersion } from "@/lib/company";

// Actual template page, tabs, version history, editor, SWR and HTTP adapter.
// The synthetic remote enforces revision CAS for revocation and draft writes.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const prefix = "/api/v1/company/templates";
const template: MailTemplate = {
  id: "revoke-template", name: "Versioned template", revision: 7, retired: false, updated_at: "2026-10-09T00:00:00Z",
  draft: { subject: "Saved subject", text_body: "Saved body", html_body: "<p>Saved HTML</p>",
    variables: [{ name: "customer", type: "text", required: false, max_length: 80, options: ["Alice"] }] },
};
const alternative: MailTemplate = { ...template, id: "other-template", name: "Other template", revision: 12,
  draft: { ...template.draft, subject: "Other subject" } };
const version: TemplateVersion = { id: "immutable-one", template_id: template.id, name: template.name,
  version: 1, snapshot: template.draft, published_at: "2026-10-09T00:00:00Z", content_hash: "a".repeat(64) };
type Call = { path: string; method: string; body: Record<string, unknown> };
let catalog: MailTemplate[];
let versions: TemplateVersion[];
let calls: Call[];
let readLibrary: () => Promise<Response>;
let revoke: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
let user: ReturnType<typeof userEvent.setup>;
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: { code: status === 409 ? "CONFLICT" : "UNAVAILABLE",
  message: `Synthetic template ${status}` } }), { status, headers: { "Content-Type": "application/json" } });
const revokes = () => calls.filter(call => call.path.endsWith("/revoke"));
const saves = () => calls.filter(call => call.method === "PUT");
const reads = () => calls.filter(call => call.method === "GET" && call.path === prefix);
const change = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
const saveButton = () => screen.getByRole("button", { name: "Save template draft" });
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
function identity(id = "version-admin") {
  installSession(`token-${id}`, { id, tenant_id: "company", role: "admin", email: `${id}@example.test`, display_name: id });
}
function acknowledge(call: Call) {
  const old = catalog.find(value => call.path.startsWith(`${prefix}/${value.id}/versions/`));
  if (!old || call.body.revision !== old.revision) return failed(409);
  catalog = catalog.map(value => value.id === old.id ? { ...value, revision: value.revision + 1 } : value);
  versions = versions.map(value => value.template_id === old.id ? { ...value, revoked_at: "2026-10-09T00:00:01Z" } : value);
  return json({ revoked: true });
}
function saveResponse(call: Call) {
  const old = catalog.find(value => call.path === `${prefix}/${value.id}`);
  if (!old || call.body.revision !== old.revision) return failed(409);
  const saved = { ...old, name: String(call.body.name), draft: call.body.draft as TemplateDraft, revision: old.revision + 1 };
  catalog = catalog.map(value => value.id === old.id ? saved : value);
  return json(saved);
}
beforeEach(() => {
  catalog = structuredClone([template, alternative]); versions = structuredClone([version]); calls = []; pending = [];
  readLibrary = async () => json(catalog); revoke = async call => acknowledge(call); user = userEvent.setup();
  identity();
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname,
      method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : {} };
    calls.push(call);
    if (call.method === "GET" && call.path === prefix) return readLibrary();
    if (call.method === "GET" && call.path === "/api/v1/company/mailboxes") return json([]);
    if (call.method === "GET" && call.path.endsWith("/versions")) return json(versions);
    if (call.method === "POST" && call.path.endsWith("/revoke")) return revoke(call);
    if (call.method === "PUT") return saveResponse(call);
    throw new Error(`Unexpected revocation request: ${call.method} ${call.path}`);
  });
});

describe("independent revision review boundaries", () => {
  it.each(["missing", "negative", "fractional", "string", "missing draft"])("keeps malformed %s readback blocked and recovers by GET only", async invalid => {
    await mount(); change("Text body template", "Independent retained draft");
    readLibrary = async () => {
      const value = structuredClone(catalog[0]) as unknown as Record<string, unknown>;
      if (invalid === "negative") value.revision = -1;
      if (invalid === "fractional") value.revision = 8.5;
      if (invalid === "string") value.revision = "8";
      if (invalid === "missing draft") delete value.draft;
      return json(invalid === "missing" ? [alternative] : [value, alternative]);
    };
    await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Text body template")).toHaveValue("Independent retained draft");
    expect(saveButton()).toBeDisabled(); expect(saves()).toHaveLength(0);
    readLibrary = async () => json(catalog);
    fireEvent.click(screen.getByRole("button", { name: "Refresh template revision" }));
    await waitFor(() => expect(saveButton()).toBeEnabled());
    expect(revokes()).toHaveLength(1); expect(saves()).toHaveLength(0);
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body).toMatchObject({ revision: 8, draft: { text_body: "Independent retained draft" } });
  });

  it("recovers a rejected revoke at the unchanged revision without replaying the command", async () => {
    await mount(); change("Subject template", "Rejected revoke draft"); revoke = async () => failed(403);
    await startRevoke(); await settled(); await editor();
    expect(saveButton()).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Refresh template revision" }));
    await waitFor(() => expect(saveButton()).toBeEnabled());
    expect(revokes()).toHaveLength(1); expect(saves()).toHaveLength(0);
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body).toMatchObject({ revision: 7, draft: { subject: "Rejected revoke draft" } });
  });

  it("accepts identical concurrent field changes without an unnecessary conflict", async () => {
    await mount(); change("Text body template", "Agreed body");
    revoke = async call => {
      const response = acknowledge(call);
      catalog = catalog.map(value => value.id === template.id ? { ...value, revision: 9,
        draft: { ...value.draft, text_body: "Agreed body" } } : value);
      return response;
    };
    await startRevoke(); await settled(); await editor();
    expect(saveButton()).toBeEnabled(); expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body).toMatchObject({ revision: 9, draft: { text_body: "Agreed body" } });
  });

  it("deduplicates synchronous revoke clicks while the command is pending", async () => {
    const action = deferred(); revoke = () => action.promise; await mount();
    await user.click(screen.getByRole("tab", { name: "Version history" }));
    const button = await screen.findByRole("button", { name: "Revoke" });
    await waitFor(() => expect(button).toBeEnabled());
    act(() => { fireEvent.click(button); fireEvent.click(button); });
    await waitFor(() => expect(revokes()).toHaveLength(1));
    await act(async () => action.resolve(acknowledge(revokes()[0]))); await settled();
    expect(revokes()).toHaveLength(1);
  });

  it.each(["another", "same"])("retires a pending command's continuation after selecting %s template", async selected => {
    const action = deferred(); revoke = () => action.promise; await mount(); await startRevoke();
    await select(selected === "another" ? alternative.name : template.name);
    change("Text body template", "New selection draft");
    const priorReads = reads().length;
    await act(async () => action.resolve(acknowledge(revokes()[0]))); await settled();
    expect(screen.getByLabelText("Text body template")).toHaveValue("New selection draft");
    expect(screen.getByLabelText("Template name")).toHaveValue(selected === "another" ? alternative.name : template.name);
    expect(reads()).toHaveLength(priorReads); expect(saves()).toHaveLength(0);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(resolve => resolve(json([])))); vi.unstubAllGlobals(); });
async function editor() {
  await user.click(screen.getByRole("tab", { name: /^Editor/ }));
  await screen.findByLabelText("Template name");
}
async function select(name = template.name) {
  const library = screen.getByRole("tab", { name: "Template library" });
  if (library.getAttribute("aria-selected") !== "true") await user.click(library);
  await user.click(await screen.findByRole("button", { name: new RegExp(`^${name} Draft revision`) }));
  await waitFor(() => expect(screen.getByLabelText("Template name")).toHaveValue(name));
}
async function mount() {
  const view = render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}><TemplatesPage /></SWRConfig>);
  await select(); return view;
}
async function startRevoke() {
  await user.click(screen.getByRole("tab", { name: "Version history" }));
  const button = await screen.findByRole("button", { name: "Revoke" });
  await waitFor(() => expect(button).toBeEnabled()); fireEvent.click(button);
  await waitFor(() => expect(revokes()).toHaveLength(1));
}
async function settled() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }

describe("template version revocation retains editor intent", () => {
  it.each([
    ["Template name", "Unsaved name"], ["Subject template", "Unsaved subject"],
    ["Text body template", "Unsaved body"], ["HTML template", "<p>Unsaved HTML</p>"],
    ["Allowed values (optional, one per line)", "Unsaved option\nAnother option"],
  ])("retains %s and sends it with the post-revocation revision only after explicit save", async (label, value) => {
    await mount(); change(label, value); await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText(label)).toHaveValue(value); expect(saves()).toHaveLength(0);
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body.revision).toBe(8); await settled(); expect(toast.error).not.toHaveBeenCalled();
  });

  it("keeps removed variables removed when the administrator revokes a published version", async () => {
    await mount(); fireEvent.click(screen.getByRole("button", { name: "Remove variable" }));
    await startRevoke(); await settled(); await editor();
    expect(screen.queryByLabelText("Variable name")).not.toBeInTheDocument();
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body).toMatchObject({ revision: 8, draft: { variables: [] } });
  });

  it("allows the untouched editor to save against the new revision", async () => {
    await mount(); await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Template name")).toHaveValue(template.name);
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body.revision).toBe(8); await settled(); expect(toast.error).not.toHaveBeenCalled();
  });

  it("blocks saving while the revoke is in flight, survives tab navigation and keeps later edits", async () => {
    const action = deferred(); revoke = () => action.promise;
    await mount(); change("Text body template", "Draft before revoke"); await startRevoke(); await editor();
    expect(saveButton()).toBeDisabled(); change("Text body template", "Draft typed during revoke");
    await act(async () => action.resolve(acknowledge(revokes()[0]))); await settled();
    expect(screen.getByLabelText("Text body template")).toHaveValue("Draft typed during revoke");
    expect(saveButton()).toBeEnabled(); fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body).toMatchObject({ revision: 8, draft: { text_body: "Draft typed during revoke" } });
  });

  it.each([403, 503])("preserves edits after post-revocation read %s and retries only the authoritative GET", async status => {
    await mount(); change("Text body template", "Retained failed-read draft"); readLibrary = async () => failed(status);
    await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Text body template")).toHaveValue("Retained failed-read draft");
    expect(saveButton()).toBeDisabled(); expect(screen.getByRole("button", { name: "Save and publish new version" })).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent(/revision|版本/i);
    readLibrary = async () => json(catalog);
    fireEvent.click(screen.getByRole("button", { name: "Refresh template revision" }));
    await waitFor(() => expect(saveButton()).toBeEnabled());
    expect(revokes()).toHaveLength(1); expect(saves()).toHaveLength(0);
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1)); expect(saves()[0].body.revision).toBe(8);
  });

  it("keeps a readback at the old revision blocked instead of treating cached content as current", async () => {
    await mount(); change("Subject template", "Unsaved subject"); readLibrary = async () => json([template, alternative]);
    await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Subject template")).toHaveValue("Unsaved subject"); expect(saveButton()).toBeDisabled();
    readLibrary = async () => json(catalog); fireEvent.click(screen.getByRole("button", { name: "Refresh template revision" }));
    await waitFor(() => expect(saveButton()).toBeEnabled()); expect(saves()).toHaveLength(0); expect(revokes()).toHaveLength(1);
  });

  it("merges an untouched server field while retaining independent unsaved edits", async () => {
    await mount(); change("Text body template", "Local unsaved body");
    revoke = async call => {
      const result = acknowledge(call);
      catalog = catalog.map(value => value.id === template.id ? { ...value, revision: value.revision + 1,
        draft: { ...value.draft, subject: "New server subject" } } : value); return result;
    };
    await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Text body template")).toHaveValue("Local unsaved body");
    expect(screen.getByLabelText("Subject template")).toHaveValue("New server subject");
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body).toMatchObject({ revision: 9, draft: { text_body: "Local unsaved body", subject: "New server subject" } });
  });

  it("requires explicit review when the server changed a locally edited field", async () => {
    await mount(); change("Text body template", "My unresolved body");
    revoke = async call => {
      const result = acknowledge(call);
      catalog = catalog.map(value => value.id === template.id ? { ...value, revision: value.revision + 1,
        draft: { ...value.draft, text_body: "Conflicting server body" } } : value); return result;
    };
    await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Text body template")).toHaveValue("My unresolved body"); expect(saveButton()).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent(/changed|变化/i);
    fireEvent.click(screen.getByRole("button", { name: "Keep my edits with the reviewed revision" }));
    await waitFor(() => expect(saveButton()).toBeEnabled()); expect(saves()).toHaveLength(0);
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1));
    expect(saves()[0].body).toMatchObject({ revision: 9, draft: { text_body: "My unresolved body" } });
  });

  it("uses a successful draft save as the baseline for the next revoke reconciliation", async () => {
    await mount(); change("Text body template", "Saved in this editor"); fireEvent.click(saveButton());
    await waitFor(() => expect(saves()).toHaveLength(1)); await waitFor(() => expect(saveButton()).toBeEnabled());
    change("Subject template", "New unsaved subject"); await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Text body template")).toHaveValue("Saved in this editor");
    expect(screen.getByLabelText("Subject template")).toHaveValue("New unsaved subject");
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(2)); expect(saves()[1].body.revision).toBe(9);
  });

  it("keeps the unknown revoke result blocked until an explicit read confirms the current revision", async () => {
    await mount(); change("Text body template", "Unknown-result draft");
    revoke = async call => { acknowledge(call); return failed(); };
    await startRevoke(); await settled(); await editor();
    expect(screen.getByLabelText("Text body template")).toHaveValue("Unknown-result draft"); expect(saveButton()).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Refresh template revision" }));
    await waitFor(() => expect(saveButton()).toBeEnabled()); expect(revokes()).toHaveLength(1);
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1)); expect(saves()[0].body.revision).toBe(8);
  });

  it.each(["unmount", "session"])("does not notify or overwrite after the revoke page leaves by %s", async destination => {
    const action = deferred(); revoke = () => action.promise; const view = await mount(); await startRevoke();
    if (destination === "unmount") view.unmount(); else act(() => identity("replacement-version-admin"));
    const count = reads().length;
    await act(async () => action.resolve(acknowledge(revokes()[0]))); await settled();
    expect(reads()).toHaveLength(count); expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
  });

  it("does not interrupt an owned revoke when only the access token rotates", async () => {
    const action = deferred(); revoke = () => action.promise; await mount(); change("Text body template", "Token-owned draft"); await startRevoke();
    act(() => { localStorage.setItem("tabmail_access_token", "rotated-version-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    await act(async () => action.resolve(acknowledge(revokes()[0]))); await settled(); await editor();
    expect(screen.getByLabelText("Text body template")).toHaveValue("Token-owned draft");
    fireEvent.click(saveButton()); await waitFor(() => expect(saves()).toHaveLength(1)); expect(saves()[0].body.revision).toBe(8);
  });
});
