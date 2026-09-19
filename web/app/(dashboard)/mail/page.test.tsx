import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { MessagePane, SubmissionPane } from "./page";

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

import type { Submission, WorkMailbox } from "@/lib/company";
import type { MessageDetail } from "@/lib/types";

const baseSubmission = (over: Partial<Submission> = {}): Submission => ({
  id: "job-1",
  mailbox_id: "mb-1",
  from: "worker@company.test",
  subject: "quarterly report",
  recipients: [{ address: "dest@client.test", state: "accepted" }],
  status: "accepted",
  draft_consumed: false,
  attachment_count: 1,
  created_at: new Date().toISOString(),
  content_redacted: false,
  delivery_uncertain: false,
  ...over,
});

describe("SubmissionPane sent-content view", () => {
  beforeEach(() => {
    submissionMock.mockReset().mockResolvedValue(baseSubmission());
    submissionContentMock.mockReset().mockResolvedValue({
      id: "job-1",
      subject: "quarterly report",
      from: "worker@company.test",
      to: ["dest@client.test"],
      text_body: "see attached",
      created_at: new Date().toISOString(),
      content_redacted: false,
    });
    submissionAttachmentsMock.mockReset().mockResolvedValue([
      {
        id: "att-1",
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
    render(<SubmissionPane id="job-1" />);
    await screen.findByText(/worker@company\.test/);
    expect(submissionContentMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "View content" }));
    await screen.findByText("see attached");
    expect(submissionContentMock).toHaveBeenCalledWith("job-1");
    expect(submissionAttachmentsMock).toHaveBeenCalledWith("job-1");

    const download = await screen.findByRole("button", {
      name: /report\.csv/,
    });
    fireEvent.click(download);
    await waitFor(() =>
      expect(downloadFileMock).toHaveBeenCalledWith(
        "/submissions/job-1/attachments/att-1/download",
        "report.csv",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Hide content" }));
    expect(screen.queryByText("see attached")).toBeNull();
  });

  it("shows the redaction notice instead of a body when content is redacted", async () => {
    submissionContentMock.mockResolvedValue({
      id: "job-1",
      subject: "quarterly report",
      from: "worker@company.test",
      to: ["dest@client.test"],
      created_at: new Date().toISOString(),
      content_redacted: true,
    });
    render(<SubmissionPane id="job-1" />);
    fireEvent.click(await screen.findByRole("button", { name: "View content" }));
    await screen.findByText("The message body is not visible to you.");
    expect(screen.queryByText("see attached")).toBeNull();
  });

  it("keeps the content collapsed until requested", async () => {
    render(<SubmissionPane id="job-1" />);
    await screen.findByText(/worker@company\.test/);
    await waitFor(() => expect(submissionContentMock).not.toHaveBeenCalled());
    expect(screen.queryByText("see attached")).toBeNull();
  });
});

const baseMailbox = (over: Partial<WorkMailbox> = {}): WorkMailbox => ({
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
    render(<SubmissionPane id="job-1" />);
    await screen.findByText(/worker@company\.test/);
    expect(
      screen.getByRole("button", { name: "View content" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Retry unfinished recipients/ }),
    ).toBeNull();
  });

  it("shows retry but not content when the server grants retry only", async () => {
    submissionMock.mockResolvedValue(
      baseSubmission({
        capabilities: { view_content: false, retry: true },
      }),
    );
    render(<SubmissionPane id="job-1" />);
    await screen.findByText(/worker@company\.test/);
    expect(
      screen.queryByRole("button", { name: "View content" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: /Retry unfinished recipients/ }),
    ).toBeInTheDocument();
  });

  it("surfaces the uncertain-outcome notice on a delivery_uncertain conflict", async () => {
    submissionMock.mockResolvedValue(
      baseSubmission({
        capabilities: { view_content: true, retry: true },
      }),
    );
    requestMock.mockRejectedValue({
      error: {
        code: "CONFLICT",
        message: "retry blocked",
        reason: "delivery_uncertain",
      },
    });
    render(<SubmissionPane id="job-1" />);
    await screen.findByText(/worker@company\.test/);
    fireEvent.click(
      screen.getByRole("button", { name: /Retry unfinished recipients/ }),
    );
    await waitFor(() =>
      expect(vi.mocked(toast.error)).toHaveBeenCalledWith(
        expect.stringContaining("Uncertain outcome"),
      ),
    );
    expect(requestMock).toHaveBeenCalledWith(
      "/api/v1/outbound/job-1/retry",
      expect.objectContaining({ method: "POST" }),
    );
  });
});
