import React from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { useSWRConfig } from "swr";
import { sessionScope } from "@/lib/session";
import { parseSubmissionContent } from "@/lib/receipt-types";

import { MessagePane } from "@/features/mail/components/message-pane";
import { SubmissionPane } from "@/features/mail/components/submission-pane";

const {
  submissionMock,
  submissionContentMock,
  submissionAttachmentsMock,
  downloadFileMock,
  workMessageMock,
  companyMock,
  requestMock,
} = vi.hoisted(() => ({
  submissionMock: vi.fn(),
  submissionContentMock: vi.fn(),
  submissionAttachmentsMock: vi.fn(),
  downloadFileMock: vi.fn(),
  workMessageMock: vi.fn(),
  companyMock: vi.fn(),
  requestMock: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/components/company/compose", () => ({
  Compose: () => null,
}));

vi.mock("@/lib/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/company")>()),
  submission: (...args: unknown[]) => submissionMock(...args),
  submissionContent: (...args: unknown[]) => submissionContentMock(...args),
  submissionAttachments: (...args: unknown[]) =>
    submissionAttachmentsMock(...args),
  downloadCompanyFile: (...args: unknown[]) => downloadFileMock(...args),
  workMessage: (...args: unknown[]) => workMessageMock(...args),
  company: (...args: unknown[]) => companyMock(...args),
}));

vi.mock("@/lib/api/base", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/base")>()),
  request: (...args: unknown[]) => requestMock(...args),
}));

import type { Submission, SubmissionContent, WorkMailbox } from "@/lib/company";
import type { MessageDetail } from "@/lib/types";

const submissionId = "30000000-0000-4000-8000-000000000001";
const submissionTenantId = "10000000-0000-4000-8000-000000000001";
const submissionAttachmentId = "50000000-0000-4000-8000-000000000001";
const baseSubmission = (over: Partial<Submission> = {}): Submission => ({
  id: submissionId,
  tenant_id: submissionTenantId,
  state: "sent",
  status: "accepted",
  progress: { completeness: "known", counts: { total: 1, accepted: 1, pending: 0, temporary: 0, permanent: 0, uncertain: 0 } },
  created_at: new Date().toISOString(),
  delivery_uncertain: false,
  capabilities: { view_content: true, retry: false, retry_block_reason: "state_not_retryable" },
  ...over,
});
const baseSubmissionContent = (over: Partial<SubmissionContent> = {}): SubmissionContent => ({
  id: submissionId,
  subject: "quarterly report",
  from: "worker@company.test",
  to: ["dest@client.test"],
  cc: [],
  bcc: [],
  recipient_completeness: "complete",
  text_body: "see attached",
  created_at: new Date().toISOString(),
  content_redacted: false,
  ...over,
});
const retryableSubmission = (viewContent: boolean): Submission => baseSubmission({
  state: "failed",
  status: "needs_attention",
  progress: { completeness: "known", counts: { total: 1, accepted: 0, pending: 0, temporary: 0, permanent: 1, uncertain: 0 } },
  capabilities: { view_content: viewContent, retry: true, retry_block_reason: "" },
});

describe("SubmissionPane sent-content view", () => {
  beforeEach(() => {
    submissionMock.mockReset().mockResolvedValue(baseSubmission());
    submissionContentMock.mockReset().mockResolvedValue(baseSubmissionContent());
    submissionAttachmentsMock.mockReset().mockResolvedValue([
      {
        id: submissionAttachmentId,
        filename: "report.csv",
        content_type: "text/csv",
        size: 2048,
        state: "ready",
      },
    ]);
    downloadFileMock.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => cleanup());

  it("reveals the sent body and attachment download on demand", async () => {
    render(<SubmissionPane id={submissionId} />);
    await screen.findByText(`Task: ${submissionId}`);
    expect(submissionContentMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "View content (re-authorized)" }));
    await screen.findByText("see attached");
    expect(submissionContentMock).toHaveBeenCalledWith(submissionId, expect.any(AbortSignal));
    expect(submissionAttachmentsMock).toHaveBeenCalledWith(submissionId, expect.any(AbortSignal));

    const download = await screen.findByRole("button", {
      name: /report\.csv/,
    });
    fireEvent.click(download);
    await waitFor(() =>
      expect(downloadFileMock).toHaveBeenCalledWith(
        `/submissions/${submissionId}/attachments/${submissionAttachmentId}/download`,
        "report.csv",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Hide content" }));
    expect(screen.queryByText("see attached")).toBeNull();
  });

  it("shows the redaction notice instead of a body when content is redacted", async () => {
    // Invalid success-wire rejection, not a readable/redacted success DTO or
    // a substitute for backend current-read/lifecycle denial coverage.
    submissionContentMock.mockImplementation(async () => parseSubmissionContent({
      ...baseSubmissionContent({ html_body: "<p>private invalid-wire HTML</p>" }),
      content_redacted: true,
    }, submissionId));
    render(<SubmissionPane id={submissionId} />);
    fireEvent.click(await screen.findByRole("button", { name: "View content (re-authorized)" }));
    await screen.findByText("Content and attachments are currently unavailable; recheck read access and message retention.");
    expect(screen.queryByText("see attached")).toBeNull();
    expect(screen.queryByTitle("HTML email preview")).toBeNull();
    expect(document.body).not.toHaveTextContent("private invalid-wire HTML");
  });

  it("keeps the content collapsed until requested", async () => {
    render(<SubmissionPane id={submissionId} />);
    await screen.findByText(`Task: ${submissionId}`);
    await waitFor(() => expect(submissionContentMock).not.toHaveBeenCalled());
    expect(screen.queryByText("see attached")).toBeNull();
  });
});

const baseMailbox = (over: Partial<WorkMailbox> = {}): WorkMailbox => ({
  mailbox: {
    kind: "shared",
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
  can_send: false,
  template_only: false,
  revision: 0,
  ...over,
});

const baseMessage = (over: Partial<MessageDetail> = {}): MessageDetail => ({
  id: "msg-1",
  tenant_id: "tenant-1",
  mailbox_id: "mb-1",
  zone_id: "zone-1",
  sender: "sender@client.test",
  recipients: ["shared@company.test"],
  subject: "invoice attached",
  size: 128,
  seen: false,
  starred: false,
  received_at: new Date().toISOString(),
  expires_at: null,
  text_body: "see invoice",
  html_body: "",
  ...over,
});

describe("MessagePane personal-state vs shared-organizing actions", () => {
  beforeEach(() => {
    workMessageMock.mockReset().mockResolvedValue(baseMessage());
    companyMock.mockReset().mockResolvedValue([]);
  });

  afterEach(() => cleanup());

  it("shows personal-state actions but not organizing actions to a read-only member", async () => {
    render(
      <MessagePane
        mailbox={baseMailbox({ can_read: true, can_organize: false })}
        id="msg-1"
        onMutation={() => {}}
        onCompose={() => {}}
      />,
    );
    await screen.findByText(/sender@client\.test/);
    expect(
      screen.getByRole("button", { name: "Mark read" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Star" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Move to trash" }),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
    await waitFor(() =>
      expect(companyMock).toHaveBeenCalledWith(
        "/mailboxes/mb-1/messages/msg-1/actions",
        expect.objectContaining({ method: "POST", body: { action: "seen" } }),
      ),
    );
  });

  it("shows both action groups to an organizer", async () => {
    render(
      <MessagePane
        mailbox={baseMailbox({ can_read: true, can_organize: true })}
        id="msg-1"
        onMutation={() => {}}
        onCompose={() => {}}
      />,
    );
    await screen.findByText(/sender@client\.test/);
    expect(
      screen.getByRole("button", { name: "Mark read" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Star" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Archive" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Move to trash" }),
    ).toBeInTheDocument();
  });
});

describe("SubmissionPane capabilities", () => {
  beforeEach(() => {
    submissionMock.mockReset();
    submissionContentMock.mockReset().mockResolvedValue(undefined);
    submissionAttachmentsMock.mockReset().mockResolvedValue([]);
    downloadFileMock.mockReset().mockResolvedValue(undefined);
    requestMock.mockReset();
    vi.mocked(toast.success).mockClear();
    vi.mocked(toast.error).mockClear();
  });

  afterEach(() => cleanup());

  it("shows content but not retry when the server grants view only", async () => {
    submissionMock.mockResolvedValue(
      baseSubmission({
        capabilities: {
          view_content: true,
          retry: false,
          retry_block_reason: "sender_authority",
        },
      }),
    );
    render(<SubmissionPane id={submissionId} />);
    await screen.findByText(`Task: ${submissionId}`);
    expect(
      screen.getByRole("button", { name: "View content (re-authorized)" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Safely retry unfinished targets/ }),
    ).toBeNull();
  });

  it("shows retry but not content when the server grants retry only", async () => {
    submissionMock.mockResolvedValue(
      retryableSubmission(false),
    );
    render(<SubmissionPane id={submissionId} />);
    await screen.findByText(`Task: ${submissionId}`);
    expect(
      screen.queryByRole("button", { name: "View content (re-authorized)" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: /Safely retry unfinished targets/ }),
    ).toBeInTheDocument();
  });

  it("surfaces the uncertain-outcome notice on a delivery_uncertain conflict", async () => {
    submissionMock.mockResolvedValue(
      retryableSubmission(true),
    );
    requestMock.mockRejectedValue({
      error: {
        code: "CONFLICT",
        message: "retry blocked",
        reason: "delivery_uncertain",
      },
    });
    render(<SubmissionPane id={submissionId} />);
    await screen.findByText(`Task: ${submissionId}`);
    fireEvent.click(
      screen.getByRole("button", { name: /Safely retry unfinished targets/ }),
    );
    await waitFor(() =>
      expect(vi.mocked(toast.error)).toHaveBeenCalledWith(
        expect.stringContaining("verify next-hop evidence for uncertain outcomes"),
      ),
    );
    expect(requestMock).toHaveBeenCalledWith(
      `/api/v1/outbound/${submissionId}/retry`,
      expect.objectContaining({ method: "POST" }),
    );
  });
});


function RefreshableSubmission() {
  const { mutate } = useSWRConfig();
  return <>
    <SubmissionPane id={submissionId} />
    <button onClick={() => void mutate(["session", sessionScope(), ["submission", submissionId]])}>Refresh receipt</button>
  </>;
}

describe("Revocation while a sent message is open", () => {
  beforeEach(() => {
    submissionMock.mockReset().mockResolvedValue(baseSubmission({ capabilities: { view_content: true, retry: false, retry_block_reason: "state_not_retryable" } }));
    submissionContentMock.mockReset().mockResolvedValue(baseSubmissionContent({ text_body: "private body" }));
    submissionAttachmentsMock.mockReset().mockResolvedValue([{ id: submissionAttachmentId, filename: "private.csv", size: 1 }]);
  });
  afterEach(() => { cleanup(); vi.useRealTimers(); });

  it("unmounts open content, clears cached bytes and requires an explicit reopen after regrant", async () => {
    render(<RefreshableSubmission />);
    fireEvent.click(await screen.findByRole("button", { name: "View content (re-authorized)" }));
    await screen.findByText("private body");
    await screen.findByRole("button", { name: /private.csv/ });
    submissionMock.mockResolvedValue(baseSubmission({ capabilities: { view_content: false, retry: false, retry_block_reason: "sender_authority" } }));
    fireEvent.click(screen.getByText("Refresh receipt"));
    await waitFor(() => expect(screen.queryByText("private body")).toBeNull());
    expect(screen.queryByRole("button", { name: /private.csv/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /View content|Hide content/ })).toBeNull();
    submissionContentMock.mockResolvedValue(baseSubmissionContent({ text_body: "newly authorized body", bcc: null, recipient_completeness: "legacy_unknown" }));
    submissionAttachmentsMock.mockResolvedValue([]);
    submissionMock.mockResolvedValue(baseSubmission({ capabilities: { view_content: true, retry: false, retry_block_reason: "state_not_retryable" } }));
    fireEvent.click(screen.getByText("Refresh receipt"));
    const view = await screen.findByRole("button", { name: "View content (re-authorized)" });
    expect(screen.queryByText("private body")).toBeNull();
    fireEvent.click(view);
    await screen.findByText("newly authorized body");
    expect(screen.queryByText("private body")).toBeNull();
  });

  it("does not leave cached attachment metadata visible after a denied revalidation", async () => {
    // The live content reader has no body/files SWR cache. Drive its actual
    // defensive poll with a controlled interval clock; retain denial assertions.
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    render(<RefreshableSubmission />);
    fireEvent.click(await screen.findByRole("button", { name: "View content (re-authorized)" }));
    await screen.findByRole("button", { name: /private.csv/ });
    submissionAttachmentsMock.mockRejectedValue({ error: { code: "FORBIDDEN", message: "revoked" } });
    await act(async () => { await vi.advanceTimersByTimeAsync(15000); });
    await waitFor(() => expect(screen.queryByRole("button", { name: /private.csv/ })).toBeNull());
    expect(screen.queryByText("private body")).toBeNull();
    vi.useRealTimers();
  });
});

describe("Template-only fallback sender", () => {
  afterEach(() => cleanup());
  it("sends the reply preparation request with the template-only identity, not the unreadable sender", async () => {
    workMessageMock.mockReset().mockResolvedValue(baseMessage());
    companyMock.mockReset().mockResolvedValue([]);
    const source = baseMailbox();
    const sender = baseMailbox({ mailbox: { ...source.mailbox, id: "template-sender" }, can_send: true, template_only: true });
    render(<MessagePane mailbox={source} mailboxes={[source, sender]} id="msg-1" onMutation={vi.fn()} onCompose={vi.fn()} />);
    fireEvent.click(await screen.findByRole("button", { name: /^Reply$/ }));
    await waitFor(() => expect(companyMock).toHaveBeenCalledWith("/mailboxes/mb-1/messages/msg-1/compose", { method: "POST", body: { mode: "reply", from_mailbox_id: "template-sender" } }));
  });
});
