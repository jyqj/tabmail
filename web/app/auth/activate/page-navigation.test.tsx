import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { toast } from "sonner";
import ActivatePage from "./page";

const tokenA = "a".repeat(64), tokenB = "b".repeat(64);
const ready = "Your account and personal mailbox are ready. Sign in with the login email specified in your invitation.";
const url = (token: string) => `${window.location.origin}/auth/activate?retained=1#${token}`;
const json = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : data), {
  status, headers: { "Content-Type": "application/json" },
});
const deferred = () => {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
};
async function drain() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
let calls: { token: string; password: string }[];
let reply: () => Promise<Response>;
function fill(password: string) {
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: password } });
}
const submit = () => fireEvent.submit(screen.getByLabelText("New password").closest("form")!);

beforeEach(() => {
  calls = [];
  history.replaceState({ retained: 1 }, "", url(tokenA));
  reply = async () => json({ activated: true });
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (_input: RequestInfo | URL, init?: RequestInit) => {
    calls.push(JSON.parse(String(init?.body)));
    return reply();
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.replaceState(null, "", "/"); });

describe("activation navigation before React commits the replacement form", () => {
  it.each([
    ["batched", true], ["batched", false], ["queued hash events", true], ["queued hash events", false],
  ])("retires a %s invitation roundtrip before an old success=%s response", async (navigation, success) => {
    const pending = deferred(); reply = () => pending.promise;
    render(<ActivatePage />);
    await act(drain);
    fill("old invitation password"); submit();
    await act(drain);
    expect(calls).toHaveLength(1);

    await act(async () => {
      // The React commit is deliberately held until after the old response's
      // microtasks. The browser URL returns to A in the same update batch.
      history.replaceState({ retained: 1 }, "", url(tokenB));
      if (navigation === "batched") {
        window.dispatchEvent(new HashChangeEvent("hashchange", { oldURL: url(tokenA), newURL: url(tokenB) }));
      }
      history.replaceState({ retained: 1 }, "", url(tokenA));
      if (navigation === "queued hash events") {
        // Native hashchange events retain their own old/new URLs even when
        // both notifications are delivered after location has returned to A.
        window.dispatchEvent(new HashChangeEvent("hashchange", { oldURL: url(tokenA), newURL: url(tokenB) }));
      }
      window.dispatchEvent(new HashChangeEvent("hashchange", { oldURL: url(tokenB), newURL: url(tokenA) }));
      pending.resolve(success ? json({ activated: true }) : json({ error: { code: "NOT_FOUND", message: "Old invitation result" } }, 404));
      await drain();
    });
    expect(window.location.hash).toBe(`#${tokenA}`);
    expect(screen.queryByText(ready)).not.toBeInTheDocument();
    expect(toast.error).not.toHaveBeenCalled();
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(screen.getByLabelText("Confirm password")).toHaveValue("");
    expect(calls).toHaveLength(1);

    reply = async () => json({ activated: true });
    fill("fresh invitation password"); submit();
    await act(drain);
    expect(calls).toEqual([
      { token: tokenA, password: "old invitation password" },
      { token: tokenA, password: "fresh invitation password" },
    ]);
    expect(screen.getByRole("status")).toHaveTextContent(ready);
  });
});
