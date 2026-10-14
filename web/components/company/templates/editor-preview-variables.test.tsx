import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { MailTemplate, MailTemplateEditor, TemplateDraft, WorkMailbox } from "@/lib/company";
import { TemplateEditorView } from "./editor";

// Mount the production editor, input controls, request parser and session
// guards. Only the HTTP peer and toast sink are substituted. The peer's
// unknown-key rejection matches company.Render, not a browser/Go/PG claim.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const template: MailTemplate = {
  id: "variable-template", name: "Variable template", revision: 4, retired: false,
  updated_at: "2026-10-08T00:00:00Z",
  draft: {
    subject: "Preview variable edit", text_body: "Prepared by {{.employee_name}}", html_body: "",
    variables: [
      { name: "customer", type: "text", required: false, max_length: 100 },
      { name: "reference", type: "text", required: false, max_length: 100 },
    ],
  },
};
const mailbox: WorkMailbox = {
  mailbox: {
    id: "variable-mailbox", tenant_id: "company", kind: "shared", zone_id: "zone",
    local_part: "sender", full_address: "sender@fixture.test", resolved_domain: "fixture.test",
    access_mode: "token", retention_hours_override: null, expires_at: null,
    created_at: "2026-10-08T00:00:00Z",
  },
  can_read: false, can_send: false, can_organize: false, template_only: false, revision: 1,
};
type PreviewCall = { mailbox_id: string; draft: TemplateDraft; vars: Record<string, string> };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const rejected = (message: string) => new Response(JSON.stringify({ error: { code: "BAD_REQUEST", message } }), {
  status: 400, headers: { "Content-Type": "application/json" },
});
const result = (values: Record<string, string>) => ({
  subject: "Current schema preview",
  text_body: `Values: ${JSON.stringify(Object.fromEntries(Object.entries(values).sort(([left], [right]) => left.localeCompare(right))))}`,
  html_body: "",
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
let calls: PreviewCall[];
let intercept: ((call: PreviewCall) => Promise<Response> | undefined) | undefined;
beforeEach(() => {
  calls = []; intercept = undefined;
  installSession("synthetic-variable-token", { id: "admin", tenant_id: "company", role: "admin", email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (path !== "/api/v1/company/templates/preview" || init?.method !== "POST")
      throw new Error(`Unexpected preview request: ${init?.method ?? "GET"} ${path}`);
    const call = JSON.parse(String(init.body)) as PreviewCall;
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.draft.variables.some(variable => ["employee_name", "company_name", "sender_address"].includes(variable.name)))
      return rejected("invalid or duplicate template variable");
    const allowed = new Set(call.draft.variables.map(variable => variable.name));
    for (const name of Object.keys(call.vars)) {
      if (!allowed.has(name)) return rejected(`unknown or reserved variable: ${name}`);
    }
    return json(result(call.vars));
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function mount() {
  const onSaved = vi.fn(async () => {}), onPublished = vi.fn(async () => template);
  function Host() {
    const [edit, setEdit] = useState<MailTemplateEditor | null>(structuredClone(template));
    const [selected, setSelected] = useState(mailbox.mailbox.id);
    return <>
      <button onClick={() => setEdit(value => value ? { ...value, draft: { ...value.draft,
        variables: value.draft.variables.filter(variable => variable.name !== "customer"),
      } } : value)}>Read schema without customer</button>
      <button onClick={() => setEdit(value => value ? { ...value, draft: { ...value.draft,
        variables: structuredClone(template.draft.variables),
      } } : value)}>Read original schema</button>
      <button onClick={() => setEdit(value => value ? { ...value, draft: { ...value.draft,
        variables: [...value.draft.variables].reverse(),
      } } : value)}>Read reordered schema</button>
      <TemplateEditorView edit={edit} setEdit={setEdit} mailboxes={[mailbox]} mailbox={selected}
        setMailbox={setSelected} onSaved={onSaved} onPublished={onPublished} />
    </>;
  }
  const view = render(<Host />);
  change("Preview value: customer", "Alice");
  change("Preview value: reference", "REF-001");
  return { ...view, onSaved, onPublished };
}
const change = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
const click = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
const rename = (index: number, value: string) => fireEvent.change(screen.getAllByLabelText("Variable name")[index], { target: { value } });
const remove = (index: number) => fireEvent.click(screen.getAllByRole("button", { name: "Remove variable" })[index]);
const preview = () => click("Validate and preview on server");
async function expectPreview(values: Record<string, string>) {
  preview();
  await waitFor(() => expect(screen.getByRole("button", { name: "Validate and preview on server" })).toBeEnabled());
  expect(calls.at(-1)?.vars).toEqual(values);
  expect(await screen.findByText((_text, element) => element?.tagName === "PRE" && element.textContent === result(values).text_body)).toBeInTheDocument();
  expect(toast.error).not.toHaveBeenCalled();
}

it("previews unchanged explicit values without saving, publishing or requiring send capability", async () => {
  const view = mount();
  await expectPreview({ customer: "Alice", reference: "REF-001" });
  expect(calls).toHaveLength(1);
  expect(calls[0]).toMatchObject({ mailbox_id: mailbox.mailbox.id, draft: template.draft });
  expect(view.onSaved).not.toHaveBeenCalled(); expect(view.onPublished).not.toHaveBeenCalled();
});

it("removes a deleted variable from the next valid preview while retaining another value", async () => {
  mount(); remove(0);
  expect(screen.queryByLabelText("Preview value: customer")).not.toBeInTheDocument();
  expect(screen.getByLabelText("Preview value: reference")).toHaveValue("REF-001");
  expect(calls).toHaveLength(0);
  await expectPreview({ reference: "REF-001" });
});

it("sends only the new explicitly entered value after a populated variable is renamed", async () => {
  mount(); rename(0, "contact");
  expect(screen.getByLabelText("Preview value: contact")).toHaveValue("");
  change("Preview value: contact", "Bob");
  await expectPreview({ contact: "Bob", reference: "REF-001" });
});

it("does not resurrect a retired value when a name changes away and back", async () => {
  mount(); rename(0, "contact"); change("Preview value: contact", "Bob"); rename(0, "customer");
  expect(screen.getByLabelText("Preview value: customer")).toHaveValue("");
  await expectPreview({ reference: "REF-001" });
});

it("does not attach a deleted variable's value to a newly added variable with the same name", async () => {
  mount(); remove(0); click("Add variable"); rename(1, "customer");
  expect(screen.getByLabelText("Preview value: customer")).toHaveValue("");
  change("Preview value: customer", "New customer");
  await expectPreview({ reference: "REF-001", customer: "New customer" });
});

it("drops every hidden value when the last declarations are removed", async () => {
  mount(); remove(1); remove(0);
  await expectPreview({});
});

it("retires values removed by an authoritative schema update as well as local controls", async () => {
  mount(); click("Read schema without customer");
  await expectPreview({ reference: "REF-001" });
  click("Read original schema");
  expect(screen.getByLabelText("Preview value: customer")).toHaveValue("");
  await expectPreview({ reference: "REF-001" });
});

it("keeps values associated with names when declaration order changes", async () => {
  mount(); click("Read reordered schema");
  expect(screen.getAllByLabelText("Variable name").map(element => (element as HTMLInputElement).value)).toEqual(["reference", "customer"]);
  await expectPreview({ customer: "Alice", reference: "REF-001" });
});

it("retains explicit values when existing declarations change their non-name constraints", async () => {
  mount();
  fireEvent.change(screen.getAllByLabelText("Maximum length")[0], { target: { value: "120" } });
  fireEvent.click(screen.getAllByLabelText("Required")[0]);
  change("Subject template", "Changed subject");
  await expectPreview({ customer: "Alice", reference: "REF-001" });
  expect(calls[0].draft.variables[0]).toMatchObject({ name: "customer", required: true, max_length: 120 });
});

it.each(["", "  spaced value  ", "客户甲🙂"])("preserves the exact explicit value %j for a remaining declaration", async value => {
  mount(); change("Preview value: reference", value); remove(0);
  await expectPreview({ reference: value });
});

it("does not manufacture empty inputs for newly added declarations", async () => {
  mount(); click("Add variable");
  fireEvent.click(screen.getAllByLabelText("Required")[2]);
  await expectPreview({ customer: "Alice", reference: "REF-001" });
  expect(calls[0].draft.variables.at(-1)?.name).toBe("field_3");
});

it.each(["employee_name", "company_name", "sender_address"])("retains server rejection of an explicitly declared reserved name %s", async name => {
  mount(); rename(0, name); change(`Preview value: ${name}`, "Forged identity"); preview();
  await waitFor(() => expect(toast.error).toHaveBeenCalledExactlyOnceWith("invalid or duplicate template variable"));
  expect(calls[0].draft.variables[0].name).toBe(name);
  expect(calls[0].vars).toEqual({ reference: "REF-001", [name]: "Forged identity" });
  expect(screen.queryByText("Current schema preview")).not.toBeInTheDocument();
});

it("discards an in-flight old-schema result and allows a fresh preview after removing its variable", async () => {
  const pending = deferred<Response>();
  mount(); intercept = () => pending.promise; preview();
  await waitFor(() => expect(calls).toHaveLength(1));
  remove(0);
  await act(async () => pending.resolve(json(result({ customer: "Alice", reference: "REF-001" }))));
  expect(screen.queryByText("Current schema preview")).not.toBeInTheDocument();
  expect(calls).toHaveLength(1); expect(toast.error).not.toHaveBeenCalled();
  intercept = undefined;
  await expectPreview({ reference: "REF-001" });
  expect(calls).toHaveLength(2);
});
