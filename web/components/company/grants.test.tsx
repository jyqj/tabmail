import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GrantEditor } from "./grants";
import type { WorkMailbox } from "@/lib/company";
import type { AdminUser, Mailbox } from "@/lib/types";

const { companyMock } = vi.hoisted(() => ({ companyMock: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/lib/company", async (original) => ({
  ...(await original<typeof import("@/lib/company")>()),
  company: (...args: unknown[]) => companyMock(...args),
}));
const mailbox: WorkMailbox = {
  mailbox: { id: "mb-1", kind: "shared", full_address: "support@company.test" } as Mailbox,
  revision: 9, can_read: false, can_send: false, can_organize: false, template_only: false,
};
const employees = [{ id: "employee", email: "e@company.test", display_name: "Employee" } as AdminUser];
const grant = { user_id: "employee", can_read: true, can_send: false, can_organize: false, template_only: false };

describe("GrantEditor optimistic concurrency", () => {
  beforeEach(() => companyMock.mockReset());
  afterEach(cleanup);
  it("uses the atomic grant snapshot version rather than the parent mailbox version", async () => {
    companyMock.mockImplementation(async (_path, options) => options?.method === "PUT" ? { updated: true } : { revision: 12, grants: [grant] });
    render(<GrantEditor mailbox={mailbox} employees={employees} refresh={vi.fn().mockResolvedValue(undefined)} />);
    await screen.findByText(/e@company.test: read/);
    fireEvent.change(screen.getByLabelText("Grant to member"), { target: { value: "employee" } });
    fireEvent.click(screen.getByLabelText("Send as"));
    fireEvent.click(screen.getByRole("button", { name: "Save mailbox grant" }));
    await waitFor(() => expect(companyMock).toHaveBeenCalledWith("/mailboxes/mb-1/grants", {
      method: "PUT", body: { ...grant, can_send: true, revision: 12 },
    }));
  });
  it("never replays a conflicted form; reload and member review use the new snapshot", async () => {
    let revision = 12;
    const puts: unknown[] = [];
    companyMock.mockImplementation(async (_path, options) => {
      if (options?.method === "PUT") {
        puts.push(options.body);
        revision = 13;
        throw { error: { code: "CONFLICT", message: "stale revision" } };
      }
      return { revision, grants: [{ ...grant, can_read: revision === 12 }] };
    });
    render(<GrantEditor mailbox={mailbox} employees={employees} refresh={vi.fn().mockResolvedValue(undefined)} />);
    await screen.findByText(/e@company.test: read/);
    fireEvent.change(screen.getByLabelText("Grant to member"), { target: { value: "employee" } });
    fireEvent.click(screen.getByRole("button", { name: "Save mailbox grant" }));
    await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: "Save mailbox grant" })).toBeDisabled();
    expect(puts).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Reload permissions" }));
    await waitFor(() => expect(screen.getByLabelText("Grant to member")).toHaveValue(""));
    expect(screen.getByRole("button", { name: "Save mailbox grant" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Grant to member"), { target: { value: "employee" } });
    expect(screen.getByLabelText("Read")).not.toBeChecked();
    expect(puts).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Save mailbox grant" }));
    await waitFor(() => expect(puts).toHaveLength(2));
    expect(puts[1]).toEqual({ ...grant, can_read: false, revision: 13 });
  });
});
