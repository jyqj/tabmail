import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  CompanyMailSendPolicyField,
  MailboxSendPolicyEditor,
} from "./send-policy";

const { toastSuccess, toastError, setPolicyMock } = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  setPolicyMock: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: toastSuccess, error: toastError },
}));

vi.mock("@/lib/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/company")>()),
  setMailboxSendPolicy: (...args: unknown[]) => setPolicyMock(...args),
}));

import type { Mailbox } from "@/lib/types";
import type { WorkMailbox } from "@/lib/company";

const workMailbox = (sendPolicy?: string): WorkMailbox => ({
  mailbox: {
    id: "mb-1",
    full_address: "support@company.test",
    kind: "shared",
    send_policy: sendPolicy,
  } as Mailbox,
  can_read: true,
  can_organize: true,
  can_send: true,
  template_only: false,
  revision: 3,
});

describe("MailboxSendPolicyEditor", () => {
  beforeEach(() => {
    toastSuccess.mockReset();
    toastError.mockReset();
    setPolicyMock.mockReset().mockResolvedValue({ updated: true });
  });

  afterEach(() => cleanup());

  it("renders the effective policy with its description", () => {
    render(
      <MailboxSendPolicyEditor
        mailbox={workMailbox("template_required")}
        refresh={vi.fn()}
      />,
    );
    expect(screen.getByText(/Effective send policy/)).toBeTruthy();
    expect(
      screen.getByText(/Sending is only allowed through published templates/),
    ).toBeTruthy();
  });

  it("falls back to the free default when no effective policy arrives", () => {
    render(
      <MailboxSendPolicyEditor mailbox={workMailbox()} refresh={vi.fn()} />,
    );
    expect(
      screen.getByText(/any member with send-as rights may compose directly/),
    ).toBeTruthy();
  });

  it("stores the chosen override, refreshes and toasts", async () => {
    const refresh = vi.fn().mockResolvedValue(undefined);
    render(
      <MailboxSendPolicyEditor mailbox={workMailbox("free")} refresh={refresh} />,
    );
    fireEvent.change(screen.getByLabelText("Send policy override"), {
      target: { value: "disabled" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Save send policy override" }),
    );
    await waitFor(() => {
      expect(setPolicyMock).toHaveBeenCalledWith("mb-1", "disabled", 3);
      expect(refresh).toHaveBeenCalled();
    });
    expect(toastSuccess).toHaveBeenCalledWith("Send policy updated");
  });

  it("clears the override (inherit company default) by sending an empty policy", async () => {
    render(
      <MailboxSendPolicyEditor
        mailbox={workMailbox("disabled")}
        refresh={vi.fn()}
      />,
    );
    expect(
      (screen.getByLabelText("Send policy override") as HTMLSelectElement)
        .value,
    ).toBe("");
    fireEvent.click(
      screen.getByRole("button", { name: "Save send policy override" }),
    );
    await waitFor(() => {
      expect(setPolicyMock).toHaveBeenCalledWith("mb-1", "", 3);
    });
  });
});

describe("CompanyMailSendPolicyField", () => {
  afterEach(() => cleanup());

  it("offers the three policy values and reports the selection", () => {
    const onChange = vi.fn();
    render(
      <CompanyMailSendPolicyField value="free" onChange={onChange} />,
    );
    const select = screen.getByLabelText(
      "Company default send policy",
    ) as HTMLSelectElement;
    expect(select.value).toBe("free");
    expect(
      Array.from(select.options).map((o) => o.value),
    ).toEqual(["free", "template_required", "disabled"]);
    fireEvent.change(select, { target: { value: "template_required" } });
    expect(onChange).toHaveBeenCalledWith("template_required");
  });
});


describe("Policy optimistic concurrency", () => {
  afterEach(() => cleanup());
  it("does not silently rebase a pending choice when the mailbox changes", async () => {
    function Harness() {
      const [revision, setRevision] = React.useState(3);
      return <>
        <button onClick={() => setRevision(4)}>External change</button>
        <MailboxSendPolicyEditor mailbox={{ ...workMailbox("free"), revision }} refresh={vi.fn()} />
      </>;
    }
    render(<Harness />);
    fireEvent.change(screen.getByLabelText("Send policy override"), { target: { value: "disabled" } });
    fireEvent.click(screen.getByText("External change"));
    expect(screen.getByRole("button", { name: "Save send policy override" })).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("Mailbox changed");
    fireEvent.click(screen.getByRole("button", { name: "Review latest version" }));
    expect(screen.getByLabelText("Send policy override")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Save send policy override" })).toBeEnabled();
  });
});
