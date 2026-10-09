import React from "react";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { OffboardingPanel } from "@/features/company/offboarding-panel";
import { previewOffboarding } from "@/features/company/api";
import { listUsers } from "@/lib/api";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
const host = vi.hoisted(() => ({ user: null as AuthUser | null, tenantId: "" }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => host }));
const packetPath = process.env.TABMAIL_PAIR_PACKET!;
const f = JSON.parse(readFileSync(packetPath, "utf8")) as { url: string; token: string; user: AuthUser; selected: string; target: string; successor: string; mode: string };
test(`independent shipping UI over real HTTP and owned PG: ${f.mode}`, async () => {
 process.env.NEXT_PUBLIC_API_URL = f.url;
 host.user = f.user; host.tenantId = f.selected || f.user.tenant_id;
 installSession(f.token, f.user); localStorage.setItem("tabmail_tenant_id", host.tenantId);
 vi.spyOn(window, "confirm").mockReturnValue(true);
 const traffic: { path: string; status: number }[] = [];
 const realFetch = globalThis.fetch.bind(globalThis);
 vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
  const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
  if (url.origin !== f.url) throw new Error("request outside owned HTTP origin");
  const response = await realFetch(input, init);
  traffic.push({ path: url.pathname, status: response.status }); return response;
 });
 const rows = (await listUsers()).data;
 const refresh = vi.fn(async () => {});
 render(<OffboardingPanel employees={rows} refresh={refresh} />);
 const options = (label: string) => Array.from((screen.getByLabelText(label) as HTMLSelectElement).options).map(o => o.value);
 expect(options("Employee to offboard")).toContain(f.target);
 expect(options("Employee to offboard")).not.toContain(f.user.id);
 expect(options("Mailbox successor")).not.toContain(f.user.id);
 if (f.mode === "frozen") expect(options("Mailbox successor")).not.toContain(f.target);
 fireEvent.change(screen.getByLabelText("Employee to offboard"), { target: { value: f.target } });
 expect(options("Mailbox successor")).not.toContain(f.target);
 fireEvent.change(screen.getByLabelText("Mailbox successor"), { target: { value: f.successor } });
 fireEvent.change(screen.getByLabelText("Reason / ticket (at least 8 characters)"), { target: { value: "Independent paired review" } });
 fireEvent.click(screen.getByRole("button", { name: "Preview handover impact" }));
 const execute = await screen.findByRole("button", { name: "Confirm handover" });
 expect((await listUsers()).data.find(u => u.id === f.target)!.is_active).toBe(f.mode !== "frozen");
 if (["successor-role", "target-freeze"].includes(f.mode)) {
  writeFileSync(packetPath + ".mutate", "preview-observed");
  await waitFor(() => expect(existsSync(packetPath + ".done")).toBe(true));
 }
 fireEvent.click(execute);
 if (f.mode === "target-freeze") {
  await screen.findByText("Assets or authority changed. Preview again; the stale plan was not applied.");
  expect(screen.queryByRole("button", { name: "Confirm handover" })).toBeNull(); expect(refresh).not.toHaveBeenCalled();
 } else if (f.mode === "successor-role") {
  await waitFor(() => expect(traffic.filter(x => x.path.endsWith("/offboard"))).toHaveLength(1));
  await waitFor(() => expect(execute).toBeEnabled());
  expect(refresh).not.toHaveBeenCalled(); expect(screen.queryByText(/Handover completed/)).toBeNull();
 } else {
  await screen.findByText(/Handover completed; the disposition receipt is retained/);
  await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
  await expect(previewOffboarding(f.target, f.successor, { drafts: "seal" }, "Independent second disposition")).rejects.toMatchObject({ error: { code: "CONFLICT" } });
 }
 expect(traffic.filter(x => x.path.endsWith("/offboard")).map(x => x.status)).toEqual([f.mode === "target-freeze" ? 409 : f.mode === "successor-role" ? 403 : 200]);
 console.log(JSON.stringify({ mode: f.mode, offboarding: traffic.filter(x => x.path.includes("offboard")) }));
 cleanup(); vi.restoreAllMocks();
});
