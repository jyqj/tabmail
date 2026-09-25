import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Compose } from "./compose";

const { companyMock, submitDraftMock } = vi.hoisted(() => ({ companyMock: vi.fn(), submitDraftMock: vi.fn() }));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/company")>()),
  company: (...args: unknown[]) => companyMock(...args),
  submitDraft: (...args: unknown[]) => submitDraftMock(...args),
}));

import type { MailDraft, WorkMailbox } from "@/lib/company";

const baseMailbox = (): WorkMailbox => ({
  mailbox: {
    id: "mb-1",
    tenant_id: "tenant-1",
    zone_id: "zone-1",
    local_part: "shared",
    resolved_domain: "company.test",
    full_address: "shared@company.test",
    access_mode: "public",
    retention_hours_override: null,
    expires_at: null,
    created_at: new Date().toISOString(),
  },
  can_read: true,
  can_organize: false,
  can_send: true,
  template_only: false,
  revision: 0,
});

const baseDraft = (over: Partial<MailDraft> = {}): MailDraft => ({
  mailbox_id: "mb-1",
  revision: 0,
  payload: {
    to: ["dest@client.test"],
    subject: "",
    text_body: "",
    template_version_id: "tv-1",
  },
  ...over,
});

function renderCompose(draft: MailDraft) {
  return render(
    <Compose
      mailboxes={[baseMailbox()]}
      initial={draft}
      onClose={() => {}}
      onSent={() => {}}
    />,
  );
}

describe("Compose template eligibility", () => {
  beforeEach(() => {
    companyMock.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => cleanup());

  it("shows the revoked notice, blocks sending, and can unpin the template", async () => {
    const saved: MailDraft = {
      id: "d-1",
      mailbox_id: "mb-1",
      revision: 1,
      payload: {
        to: ["dest@client.test"],
        subject: "",
        text_body: "",
        template_vars: {},
      },
    };
    companyMock.mockImplementation((path: string, opts?: { method?: string }) =>
      path === "/mailboxes/mb-1/templates"
        ? Promise.resolve([])
        : opts?.method === "POST"
          ? Promise.resolve(saved)
          : Promise.resolve(undefined),
    );
    renderCompose(
      baseDraft({
        template_version: { id: "tv-1", status: "revoked" },
      }),
    );
    expect(
      await screen.findByText(
        "This template version has been emergency-revoked; pick another version",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Unpin template" }));
    await waitFor(() =>
      expect(
        companyMock.mock.calls.some(
          (c) =>
            c[0] === "/drafts" &&
            (c[1] as { method?: string }).method === "POST",
        ),
      ).toBe(true),
    );
    const save = companyMock.mock.calls.find(
      (c) =>
        c[0] === "/drafts" && (c[1] as { method?: string }).method === "POST",
    );
    expect(save).toBeTruthy();
    const body = (save![1] as { body: { payload: Record<string, unknown> } })
      .body;
    expect(body.payload.template_version_id).toBeUndefined();
    expect(body.payload.template_vars).toEqual({});
  });

  it("renders a missing pin without an error and leaves sending available", async () => {
    companyMock.mockResolvedValue([]);
    renderCompose(
      baseDraft({
        template_version: {
          id: "tv-1",
          name: "Quarterly notice",
          version: 3,
          status: "missing",
        },
      }),
    );
    await screen.findByText("Quarterly notice · v3");
    expect(
      screen.queryByText(/emergency-revoked|verification failed/),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
  });
});


describe("Pinned template version survives newer publication", () => {
  afterEach(() => cleanup());
  it("edits, previews and submits authorized v1 when the picker only contains v2", async () => {
    const snapshot = { subject: "Hello {{customer}}", text_body: "Dear {{customer}}", html_body: "",
      variables: [{ name: "customer", type: "text" as const, required: true, max_length: 80 }] };
    const draft: MailDraft = {
      id: "saved-v1", mailbox_id: "mb-1", revision: 4,
      payload: { to: ["dest@client.test"], subject: "", text_body: "", template_version_id: "v1", template_vars: { customer: "Alice" } },
      template_version: { id: "v1", name: "Welcome", version: 1, status: "usable", snapshot },
    };
    companyMock.mockReset().mockImplementation((path: string, opts?: { method?: string; body?: { payload?: MailDraft["payload"] } }) => {
      if (path === "/mailboxes/mb-1/templates") return Promise.resolve([{ id: "v2", name: "Welcome", version: 2, snapshot }]);
      if (path === "/templates/preview") return Promise.resolve({ subject: "Preview v1", text_body: "Dear Alice", html_body: "" });
      if (path === "/drafts/saved-v1") return Promise.resolve({ ...draft, revision: 5, payload: opts?.body?.payload ?? draft.payload });
      return Promise.resolve([]);
    });
    submitDraftMock.mockReset().mockResolvedValue({ id: "sent-job" });
    const onSent = vi.fn();
    render(<Compose mailboxes={[{ ...baseMailbox(), template_only: true }]} initial={draft} onClose={vi.fn()} onSent={onSent} />);
    await screen.findByText("Welcome · v2");
    expect(screen.getByRole("option", { name: "Welcome · v1" })).toBeInTheDocument();
    expect(screen.getByLabelText("customer * · text")).toHaveValue("Alice");
    expect(screen.queryByLabelText("Message")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Server preview" }));
    await screen.findByText("Preview v1");
    expect(companyMock).toHaveBeenCalledWith("/templates/preview", expect.objectContaining({ body: expect.objectContaining({ template_version_id: "v1" }) }));
    fireEvent.change(screen.getByLabelText("customer * · text"), { target: { value: "Bob" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(submitDraftMock).toHaveBeenCalledWith("saved-v1", 5, "saved-v1.5"));
    expect(companyMock).toHaveBeenCalledWith("/drafts/saved-v1", expect.objectContaining({ body: expect.objectContaining({ payload: expect.objectContaining({ template_version_id: "v1", template_vars: { customer: "Bob" } }) }) }));
    expect(onSent).toHaveBeenCalledOnce();
  });
});
