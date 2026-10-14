import React from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import TemplatesPage from "@/app/(dashboard)/company/templates/page";
import { AUTH_EVENT, installSession } from "@/lib/session";
import type { MailTemplate, TemplateDraft } from "@/lib/company";

// Real template page, tabs, editor, SWR and API/session client. The synthetic
// HTTP peer enforces revisions and captures every save/publish request.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
type Call = { path: string; method: string; body: Record<string, unknown> };
const prefix = "/api/v1/company/templates";
const template: MailTemplate = {
  id: "template-one", name: "First template", revision: 7, retired: false, updated_at: "2026-10-07T00:00:00Z",
  draft: { subject: "Initial subject", text_body: "Initial body", html_body: "<p>Initial HTML</p>",
    variables: [{ name: "customer", type: "text", required: false, max_length: 80, options: ["Alice"] }] },
};
const alternative: MailTemplate = { ...template, id: "template-two", name: "Second template", revision: 12,
  draft: { ...template.draft, subject: "Second subject" } };
let catalog: MailTemplate[];
let calls: Call[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
let user: ReturnType<typeof userEvent.setup>;
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (code = "CONFLICT") => new Response(JSON.stringify({ error: { code, message: `Synthetic ${code}` } }), {
  status: code === "CONFLICT" ? 409 : 500, headers: { "Content-Type": "application/json" },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const saves = () => calls.filter(call => call.method === "PUT" || call.method === "POST" && call.path === prefix);
const publications = () => calls.filter(call => call.path.endsWith("/publish"));
const reads = () => calls.filter(call => call.method === "GET" && call.path === prefix);
function saveResponse(call: Call) {
  const old = catalog.find(value => call.path === `${prefix}/${value.id}`);
  if (call.body.revision !== (old?.revision ?? 0)) return failure();
  const value: MailTemplate = { id: old?.id ?? "created-template", name: String(call.body.name).trim(),
    revision: (old?.revision ?? 0) + 1, draft: call.body.draft as TemplateDraft,
    retired: old?.retired ?? false, updated_at: "2026-10-07T00:00:01Z" };
  catalog = [...catalog.filter(item => item.id !== value.id), value];
  return json(value);
}
function publishResponse(call: Call) {
  const old = catalog.find(value => call.path === `${prefix}/${value.id}/publish`);
  if (!old || call.body.revision !== old.revision) return failure();
  catalog = catalog.map(value => value.id === old.id ? { ...value, revision: value.revision + 1, updated_at: "2026-10-07T00:00:02Z" } : value);
  return json({ id: "published-version", template_id: old.id, name: old.name, version: 1,
    snapshot: old.draft, published_at: "2026-10-07T00:00:02Z", content_hash: "a".repeat(64) });
}
beforeEach(() => {
  catalog = structuredClone([template, alternative]); calls = []; intercept = undefined; user = userEvent.setup();
  installSession("synthetic-template-token", { id: "admin", tenant_id: "company", role: "admin", email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) as Record<string, unknown> : {} };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path === prefix && call.method === "GET") return json(catalog);
    if (call.path === "/api/v1/company/mailboxes") return json([]);
    if (call.path.endsWith("/publish")) return publishResponse(call);
    if (call.method === "PUT" || call.method === "POST" && call.path === prefix) return saveResponse(call);
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function mount() {
  const view = render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><TemplatesPage /></SWRConfig>);
  await select("First template");
  return view;
}
async function select(name: string) {
  const library = screen.getByRole("tab", { name: "Template library" });
  if (library.getAttribute("aria-selected") !== "true") await user.click(library);
  await user.click(await screen.findByRole("button", { name: new RegExp(`^${name} Draft revision`) }));
  await waitFor(() => expect(screen.getByLabelText("Template name")).toHaveValue(name));
}
const change = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
const save = () => fireEvent.click(screen.getByRole("button", { name: "Save template draft" }));
const publish = () => fireEvent.click(screen.getByRole("button", { name: "Save and publish new version" }));
async function settled() { await waitFor(() => expect(screen.getByRole("button", { name: "Save template draft" })).toBeEnabled()); }

it("saves once, accepts server normalization and uses the returned revision on the next save", async () => {
  await mount();
  change("Template name", "  Normalized template  ");
  change("Allowed values (optional, one per line)", "Alice\n\nBob\n");
  save();
  await waitFor(() => expect(toast.success).toHaveBeenCalled());
  await settled();
  expect(screen.getByLabelText("Template name")).toHaveValue("Normalized template");
  expect(screen.getByLabelText("Allowed values (optional, one per line)")).toHaveValue("Alice\nBob");
  change("Subject template", "Next subject"); save();
  await waitFor(() => expect(saves()).toHaveLength(2));
  expect(saves()[1].body).toMatchObject({ revision: 8, name: "Normalized template", draft: { subject: "Next subject" } });
});

it.each([
  ["Template name", "Newer name"], ["Subject template", "Newer subject"],
  ["Text body template", "Newer body"], ["HTML template", "<p>Newer HTML</p>"],
  ["Allowed values (optional, one per line)", "Newer option\nAnother option"],
])("preserves %s edited during save and reconciles the saved revision", async (label, value) => {
  const pending = deferred<Response>();
  await mount();
  intercept = call => call.method === "PUT" ? pending.promise : undefined;
  save();
  await waitFor(() => expect(saves()).toHaveLength(1));
  change(label, value);
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  await settled();
  expect(screen.getByLabelText(label)).toHaveValue(value);
  expect(toast.success).toHaveBeenLastCalledWith(expect.stringMatching(/newer edits.*unsaved/i));
  intercept = undefined; save();
  await waitFor(() => expect(saves()).toHaveLength(2));
  expect(saves()[1].body.revision).toBe(8);
  expect(toast.error).not.toHaveBeenCalled();
});

it("preserves new-template edits while adopting the created ID, then updates instead of creating again", async () => {
  const pending = deferred<Response>();
  await mount(); await user.click(screen.getByRole("button", { name: "New template" }));
  change("Template name", "New template work");
  intercept = call => call.method === "POST" && call.path === prefix ? pending.promise : undefined;
  save(); await waitFor(() => expect(saves()).toHaveLength(1));
  change("Text body template", "Edited during creation");
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  await settled();
  expect(screen.getByLabelText("Text body template")).toHaveValue("Edited during creation");
  intercept = undefined; save();
  await waitFor(() => expect(saves()).toHaveLength(2));
  expect(saves()[1]).toMatchObject({ path: `${prefix}/created-template`, method: "PUT", body: { revision: 1 } });
});

it.each(["different", "same-again", "new", "new-again"] as const)("does not overwrite the %s editor with an obsolete save response", async destination => {
  const pending = deferred<Response>();
  await mount();
  if (destination === "new-again") { await user.click(screen.getByRole("button", { name: "New template" })); change("Template name", "First unsaved template"); }
  intercept = call => call.method === "PUT" || call.method === "POST" && call.path === prefix ? pending.promise : undefined;
  save(); await waitFor(() => expect(saves()).toHaveLength(1));
  if (destination === "different" || destination === "same-again") {
    await select("Second template");
    if (destination === "same-again") await select("First template");
  } else await user.click(screen.getByRole("button", { name: "New template" }));
  change("Template name", "Current intended editor");
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  expect(screen.getByLabelText("Template name")).toHaveValue("Current intended editor");
  expect(toast.success).not.toHaveBeenCalled();
  expect(toast.error).not.toHaveBeenCalled();
});

it("suppresses a save rejection after the user selects a new editor", async () => {
  const pending = deferred<Response>();
  await mount(); intercept = call => call.method === "PUT" ? pending.promise : undefined;
  save(); await waitFor(() => expect(saves()).toHaveLength(1));
  await select("Second template");
  await act(async () => pending.resolve(failure()));
  expect(screen.getByLabelText("Template name")).toHaveValue("Second template");
  expect(toast.error).not.toHaveBeenCalled();
});

it("has no save callback or notification after unmount", async () => {
  const pending = deferred<Response>();
  const view = await mount(); intercept = call => call.method === "PUT" ? pending.promise : undefined;
  save(); await waitFor(() => expect(saves()).toHaveLength(1));
  const readCount = reads().length;
  view.unmount();
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  expect(reads()).toHaveLength(readCount);
  expect(toast.success).not.toHaveBeenCalled();
  expect(toast.error).not.toHaveBeenCalled();
});

it("does not leave the old template selected when the account changes during save", async () => {
  const pending = deferred<Response>();
  await mount(); intercept = call => call.method === "PUT" ? pending.promise : undefined;
  save(); await waitFor(() => expect(saves()).toHaveLength(1));
  act(() => installSession("replacement-token", { id: "replacement", tenant_id: "company", role: "admin", email: "replacement@fixture.test", display_name: "Replacement" }));
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  expect(screen.queryByLabelText("Template name")).not.toBeInTheDocument();
  expect(toast.success).not.toHaveBeenCalled();
});

it("retains an active save across token-only rotation", async () => {
  const pending = deferred<Response>();
  await mount(); intercept = call => call.method === "PUT" ? pending.promise : undefined;
  save(); await waitFor(() => expect(saves()).toHaveLength(1));
  act(() => { localStorage.setItem("tabmail_access_token", "rotated-only"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Draft saved"));
  expect(screen.getByLabelText("Template name")).toHaveValue("First template");
});

it("requires a new publish decision when the draft changed during its save", async () => {
  const pending = deferred<Response>();
  await mount(); intercept = call => call.method === "PUT" ? pending.promise : undefined;
  publish(); await waitFor(() => expect(saves()).toHaveLength(1));
  change("Text body template", "Review this new body before publishing");
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  await settled();
  expect(publications()).toHaveLength(0);
  expect(screen.getByLabelText("Text body template")).toHaveValue("Review this new body before publishing");
});

it("does not dispatch publish after the editor was replaced while its save was pending", async () => {
  const pending = deferred<Response>();
  await mount(); intercept = call => call.method === "PUT" ? pending.promise : undefined;
  publish(); await waitFor(() => expect(saves()).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "New template" }));
  change("Template name", "New publish intention");
  await act(async () => pending.resolve(saveResponse(saves()[0])));
  expect(publications()).toHaveLength(0);
  expect(screen.getByLabelText("Template name")).toHaveValue("New publish intention");
  expect(toast.success).not.toHaveBeenCalled();
});

it("does not dispatch publish after its save callback outlives the selected editor", async () => {
  const pending = deferred<Response>();
  await mount();
  intercept = call => call.path === prefix && call.method === "GET" && saves().length > 0 ? pending.promise : undefined;
  publish(); await waitFor(() => expect(reads()).toHaveLength(2));
  await user.click(screen.getByRole("button", { name: "New template" }));
  change("Template name", "Newer callback owner");
  await act(async () => pending.resolve(json(catalog)));
  expect(publications()).toHaveLength(0);
  expect(screen.getByLabelText("Template name")).toHaveValue("Newer callback owner");
});

it("publishes the saved revision exactly once and closes an unchanged editor", async () => {
  await mount(); publish();
  await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Immutable version published"));
  expect(saves()).toHaveLength(1);
  expect(publications()).toHaveLength(1);
  expect(publications()[0].body).toEqual({ revision: 8 });
  expect(screen.queryByLabelText("Template name")).not.toBeInTheDocument();
});

it("preserves edits during an already dispatched publish and uses the refreshed revision for their next save", async () => {
  const pending = deferred<Response>();
  await mount(); intercept = call => call.path.endsWith("/publish") ? pending.promise : undefined;
  publish(); await waitFor(() => expect(publications()).toHaveLength(1));
  change("Text body template", "Unpublished follow-up body");
  await act(async () => pending.resolve(publishResponse(publications()[0])));
  await settled();
  expect(screen.getByLabelText("Text body template")).toHaveValue("Unpublished follow-up body");
  expect(toast.success).toHaveBeenLastCalledWith(expect.stringMatching(/published.*newer edits.*unsaved/i));
  intercept = undefined; save(); await waitFor(() => expect(saves()).toHaveLength(2));
  expect(saves()[1].body).toMatchObject({ revision: 9, draft: { text_body: "Unpublished follow-up body" } });
  expect(publications()).toHaveLength(1);
});

it("does not clear a newly selected editor when an earlier publish finishes", async () => {
  const pending = deferred<Response>();
  await mount(); intercept = call => call.path.endsWith("/publish") ? pending.promise : undefined;
  publish(); await waitFor(() => expect(publications()).toHaveLength(1));
  await select("Second template"); change("Text body template", "Keep the new selection");
  await act(async () => pending.resolve(publishResponse(publications()[0])));
  expect(screen.getByLabelText("Template name")).toHaveValue("Second template");
  expect(screen.getByLabelText("Text body template")).toHaveValue("Keep the new selection");
  expect(toast.success).not.toHaveBeenCalled();
});

it("preserves input and authoritative revision when the post-publish refresh completes late", async () => {
  const pending = deferred<Response>();
  await mount();
  intercept = call => call.path === prefix && call.method === "GET" && publications().length > 0 ? pending.promise : undefined;
  publish(); await waitFor(() => expect(reads()).toHaveLength(3));
  change("Subject template", "Edited while refreshing the publication");
  await act(async () => pending.resolve(json(catalog)));
  await settled();
  expect(screen.getByLabelText("Subject template")).toHaveValue("Edited while refreshing the publication");
  intercept = undefined; save(); await waitFor(() => expect(saves()).toHaveLength(2));
  expect(saves()[1].body.revision).toBe(9);
});

it("retries only the read after an acknowledged publish could not refresh its revision", async () => {
  await mount();
  intercept = call => call.path === prefix && call.method === "GET" && publications().length > 0 ? Promise.resolve(failure("INTERNAL")) : undefined;
  publish(); await waitFor(() => expect(publications()).toHaveLength(1));
  const retry = await screen.findByRole("button", { name: "Refresh template revision" });
  expect(screen.getByRole("button", { name: "Save template draft" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Save and publish new version" })).toBeDisabled();
  change("Text body template", "Preserved after published refresh failure");
  intercept = undefined; await user.click(retry);
  await settled();
  expect(publications()).toHaveLength(1);
  expect(saves()).toHaveLength(1);
  expect(screen.getByLabelText("Text body template")).toHaveValue("Preserved after published refresh failure");
  save(); await waitFor(() => expect(saves()).toHaveLength(2));
  expect(saves()[1].body.revision).toBe(9);
});

it("preserves the saved draft and makes no automatic write after a publish conflict", async () => {
  await mount();
  intercept = call => call.path.endsWith("/publish") ? Promise.resolve(failure()) : undefined;
  change("Text body template", "Saved draft must remain"); publish();
  await waitFor(() => expect(toast.error).toHaveBeenCalled());
  expect(screen.getByLabelText("Text body template")).toHaveValue("Saved draft must remain");
  expect(saves()).toHaveLength(1); expect(publications()).toHaveLength(1);
});

it.each(["same", "older", "missing", "wrong-id"] as const)("rejects a %s post-publish revision read and retains edits for a read-only retry", async invalid => {
  await mount();
  intercept = call => {
    if (call.path !== prefix || call.method !== "GET" || publications().length === 0) return undefined;
    const value = catalog.find(item => item.id === template.id)!;
    return Promise.resolve(json(invalid === "missing" ? [] : [{ ...value,
      id: invalid === "wrong-id" ? "unrelated-template" : value.id,
      revision: invalid === "older" ? 7 : invalid === "same" ? 8 : value.revision }]));
  };
  publish();
  await waitFor(() => expect(toast.error).toHaveBeenCalled());
  expect(screen.getByRole("button", { name: "Save template draft" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Save and publish new version" })).toBeDisabled();
  change("Text body template", "Keep edits during revision recovery");
  intercept = undefined;
  await user.click(screen.getByRole("button", { name: "Refresh template revision" }));
  await settled();
  expect(screen.getByLabelText("Text body template")).toHaveValue("Keep edits during revision recovery");
  expect(publications()).toHaveLength(1);
  save(); await waitFor(() => expect(saves()).toHaveLength(2));
  expect(saves()[1].body.revision).toBe(9);
});

it("does not silently rebase newer local edits onto concurrently replaced server content", async () => {
  const pending = deferred<Response>();
  await mount();
  intercept = call => call.path === prefix && call.method === "GET" && publications().length > 0 ? pending.promise : undefined;
  publish(); await waitFor(() => expect(reads()).toHaveLength(3));
  change("Text body template", "Preserve local changes");
  const current = catalog.find(item => item.id === template.id)!;
  await act(async () => pending.resolve(json([{ ...current, revision: 10, draft: { ...current.draft, subject: "Another administrator's change" } }])));
  await waitFor(() => expect(toast.error).toHaveBeenCalled());
  expect(screen.getByLabelText("Text body template")).toHaveValue("Preserve local changes");
  expect(screen.getByLabelText("Subject template")).toHaveValue(template.draft.subject);
  expect(screen.getByRole("button", { name: "Save template draft" })).toBeDisabled();
  expect(saves()).toHaveLength(1); expect(publications()).toHaveLength(1);
});

it.each(["replaced", "unmounted"] as const)("ignores a post-publish refresh after its editor is %s", async retired => {
  const pending = deferred<Response>();
  const view = await mount();
  intercept = call => call.path === prefix && call.method === "GET" && publications().length > 0 ? pending.promise : undefined;
  publish(); await waitFor(() => expect(reads()).toHaveLength(3));
  if (retired === "replaced") {
    await user.click(screen.getByRole("button", { name: "New template" }));
    change("Template name", "New callback owner");
  } else view.unmount();
  await act(async () => retired === "replaced" ? pending.resolve(json(catalog)) : pending.resolve(failure("INTERNAL")));
  if (retired === "replaced") expect(screen.getByLabelText("Template name")).toHaveValue("New callback owner");
  expect(toast.success).not.toHaveBeenCalled();
  expect(toast.error).not.toHaveBeenCalled();
  expect(publications()).toHaveLength(1);
});

it("continues a new-template publication with the created ID and returned revision", async () => {
  await mount();
  await user.click(screen.getByRole("button", { name: "New template" }));
  change("Template name", "New published template"); publish();
  await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Immutable version published"));
  expect(saves()).toHaveLength(1);
  expect(saves()[0]).toMatchObject({ method: "POST", path: prefix, body: { revision: 0 } });
  expect(publications()).toHaveLength(1);
  expect(publications()[0]).toMatchObject({ path: `${prefix}/created-template/publish`, body: { revision: 1 } });
  expect(screen.queryByLabelText("Template name")).not.toBeInTheDocument();
});
