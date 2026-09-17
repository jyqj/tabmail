import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SubmissionPane } from "./page";

const {
  submissionMock,
  submissionContentMock,
  submissionAttachmentsMock,
  downloadFileMock,
} = vi.hoisted(() => ({
  submissionMock: vi.fn(),
  submissionContentMock: vi.fn(),
  submissionAttachmentsMock: vi.fn(),
  downloadFileMock: vi.fn(),
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
}));

import type { Submission } from "@/lib/company";

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
