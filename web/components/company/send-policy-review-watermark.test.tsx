import React from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { useAPI } from "@/hooks/use-api";
import { workMailboxes, type MailSendPolicy, type WorkMailbox } from "@/lib/company";
import type { Mailbox } from "@/lib/types";
import { MailboxSendPolicyEditor } from "./send-policy";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const mailbox = (revision: number, policy: MailSendPolicy): WorkMailbox => ({
  mailbox: { id: "watermark", kind: "shared", full_address: "watermark@fixture.test", send_policy: policy } as Mailbox,
  revision, can_read: false, can_organize: false, can_send: false, template_only: false,
});
const json = (revision: number, policy: MailSendPolicy) => new Response(JSON.stringify({ data: [mailbox(revision, policy)] }), {
  headers: { "Content-Type": "application/json" },
});
const error = (status: number, code: string) => new Response(JSON.stringify({ error: { code, message: `Synthetic ${code}` } }), {
  status, headers: { "Content-Type": "application/json" },
});
let release: (() => void) | undefined;
afterEach(async () => {
  cleanup(); await act(async () => release?.());
  release = undefined;
  vi.unstubAllGlobals();
});

function Harness() {
  const boxes = useAPI("company-managed-mailboxes", workMailboxes);
  const current = boxes.data?.[0];
  return <>
    <button onClick={() => void boxes.mutate()}>Parent reread</button>
    {current && <MailboxSendPolicyEditor mailbox={current} refresh={() => boxes.mutate()} />}
  </>;
}

it("retains an observed revision high-water mark across 6 to 3 to late 4, while allowing a fresh same-version policy", async () => {
  let reads = 0;
  let writes = 0;
  const delayed = new Promise<Response>(resolve => { release = () => resolve(json(4, "disabled")); });
  const replies: Array<Response | Promise<Response>> = [
    json(3, "free"), error(503, "UNAVAILABLE"), delayed,
    json(6, "disabled"), json(3, "free"),
    json(6, "template_required"), json(6, "free"),
  ];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (path === "/api/v1/company/mailboxes" && (init?.method ?? "GET") === "GET") {
      reads++;
      const reply = replies.shift();
      if (!reply) throw new Error("Unexpected synthetic reread");
      return await reply;
    }
    if (path === "/api/v1/company/mailboxes/watermark/send-policy" && init?.method === "PUT") {
      writes++; return error(409, "CONFLICT");
    }
    throw new Error(`Unexpected synthetic request: ${path}`);
  });
  const cache = new Map();
  render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0,
    revalidateOnFocus: false, shouldRetryOnError: false }}><Harness /></SWRConfig>);
  const choice = await screen.findByLabelText("Send policy override");
  const save = () => screen.getByRole("button", { name: "Save send policy override" });
  const review = () => screen.getByRole("button", { name: "Review latest version" });
  fireEvent.change(choice, { target: { value: "disabled" } });
  await act(async () => fireEvent.click(save()));
  fireEvent.click(review()); expect(reads).toBe(3);
  await act(async () => fireEvent.click(screen.getByText("Parent reread")));
  expect(screen.getByText(/Effective send policy.*Sending disabled/)).toBeInTheDocument();
  await act(async () => fireEvent.click(screen.getByText("Parent reread")));
  expect(reads).toBe(5);
  await act(async () => release?.());
  expect(save()).toBeDisabled();
  expect(choice).toHaveValue("disabled");
  expect(writes).toBe(1);

  // A genuine read at the observed high-water mark is usable, including an
  // inherited policy that changes later without a mailbox revision bump.
  await act(async () => fireEvent.click(review()));
  expect(reads).toBe(6); expect(save()).toBeEnabled(); expect(choice).toHaveValue("");
  expect(screen.getByText(/Effective send policy.*Published templates only/)).toBeInTheDocument();
  await act(async () => fireEvent.click(screen.getByText("Parent reread")));
  expect(reads).toBe(7); expect(save()).toBeEnabled(); expect(writes).toBe(1);
  expect(screen.getByText(/Effective send policy.*Free-form writing/)).toBeInTheDocument();
});

it("keeps a previously reviewed revision 4 stale after parent revision 6 then 4 until a real read reaches 6", async () => {
  let reads = 0;
  let writes = 0;
  const replies = [
    json(3, "free"), error(503, "UNAVAILABLE"), json(4, "template_required"),
    json(6, "disabled"), json(4, "template_required"), json(6, "free"),
  ];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (path === "/api/v1/company/mailboxes" && (init?.method ?? "GET") === "GET") {
      reads++;
      const reply = replies.shift();
      if (!reply) throw new Error("Unexpected synthetic reread");
      return reply;
    }
    if (path === "/api/v1/company/mailboxes/watermark/send-policy" && init?.method === "PUT") {
      writes++; return error(409, "CONFLICT");
    }
    throw new Error(`Unexpected synthetic request: ${path}`);
  });
  const cache = new Map();
  render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0,
    revalidateOnFocus: false, shouldRetryOnError: false }}><Harness /></SWRConfig>);
  const choice = await screen.findByLabelText("Send policy override");
  const save = () => screen.getByRole("button", { name: "Save send policy override" });
  const review = () => screen.getByRole("button", { name: "Review latest version" });
  fireEvent.change(choice, { target: { value: "disabled" } });
  await act(async () => fireEvent.click(save()));
  await act(async () => fireEvent.click(review()));
  expect(reads).toBe(3); expect(save()).toBeEnabled();
  fireEvent.change(choice, { target: { value: "free" } });
  await act(async () => fireEvent.click(screen.getByText("Parent reread")));
  expect(save()).toBeDisabled();
  await act(async () => fireEvent.click(screen.getByText("Parent reread")));
  expect(reads).toBe(5); expect(save()).toBeDisabled(); expect(choice).toHaveValue("free");
  fireEvent.click(save()); expect(writes).toBe(1);
  await act(async () => fireEvent.click(review()));
  expect(reads).toBe(6); expect(save()).toBeEnabled(); expect(choice).toHaveValue("");
  expect(screen.getByText(/Effective send policy.*Free-form writing/)).toBeInTheDocument();
  expect(writes).toBe(1);
});
