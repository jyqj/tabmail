import React from "react";
import { readFileSync } from "node:fs";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { OffboardingPanel } from "@/features/company/offboarding-panel";
import { previewOffboarding } from "@/features/company/api";
import { listUsers, updateUser } from "@/lib/api";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
const host = vi.hoisted(() => ({ user: null as AuthUser | null }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ user: host.user, tenantId: host.user?.tenant_id }) }));
const fixture = JSON.parse(readFileSync(process.env.TABMAIL_LF01_PROBE!, "utf8")) as { url: string; token: string; user: AuthUser; target: string; successor: string; mode: string };
test(`owned real HTTP/PG shipping panel: ${fixture.mode}`, async () => {
  process.env.NEXT_PUBLIC_API_URL = fixture.url;
  host.user = fixture.user;
  installSession(fixture.token, fixture.user);
  vi.spyOn(window, "confirm").mockReturnValue(true);
  const statuses: number[] = [];
  const realFetch = globalThis.fetch.bind(globalThis);
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    if (url.origin !== fixture.url) throw new Error("non-owned network destination");
    const response = await realFetch(input, init);
    if (url.pathname.endsWith("/offboard")) statuses.push(response.status);
    return response;
  });
  const rows = await listUsers();
  const refresh = vi.fn(async () => {});
  render(<OffboardingPanel employees={rows.data} refresh={refresh} />);
  expect(Array.from((screen.getByLabelText("Employee to offboard") as HTMLSelectElement).options).map(o => o.value)).toContain(fixture.target);
  fireEvent.change(screen.getByLabelText("Employee to offboard"), { target: { value: fixture.target } });
  fireEvent.change(screen.getByLabelText("Mailbox successor"), { target: { value: fixture.successor } });
  fireEvent.change(screen.getByLabelText("Reason / ticket (at least 8 characters)"), { target: { value: "Dedicated LF01 review ticket" } });
  fireEvent.click(screen.getByRole("button", { name: "Preview handover impact" }));
  const execute = await screen.findByRole("button", { name: "Confirm handover" });
  expect((await listUsers()).data.find(u => u.id === fixture.target)!.is_active).toBe(fixture.mode !== "frozen");
  if (fixture.mode === "target-race") await updateUser(fixture.target, { is_active: false });
  if (fixture.mode === "race") await updateUser(fixture.successor, { is_active: false });
  fireEvent.click(execute);
  if (fixture.mode === "target-race") {
    await screen.findByText("Assets or authority changed. Preview again; the stale plan was not applied.");
    expect(screen.queryByRole("button", { name: "Confirm handover" })).toBeNull();
    expect(refresh).not.toHaveBeenCalled();
  } else if (fixture.mode === "race") {
    await waitFor(() => expect(execute).toBeEnabled());
    expect(refresh).not.toHaveBeenCalled();
    expect(screen.queryByText(/Handover completed/)).toBeNull();
  } else {
    await screen.findByText(/Handover completed; the disposition receipt is retained/);
    await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
    await expect(previewOffboarding(fixture.target, fixture.successor, { drafts: "seal" }, "Second disposition forbidden")).rejects.toMatchObject({ error: { code: "CONFLICT" } });
  }
  expect(statuses).toEqual([fixture.mode === "race" ? 400 : fixture.mode === "target-race" ? 409 : 200]);
  expect((await listUsers()).data.find(u => u.id === fixture.target)!.is_active).toBe(fixture.mode === "race");
  cleanup();
  vi.restoreAllMocks();
});
