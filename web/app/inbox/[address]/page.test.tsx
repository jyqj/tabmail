import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import InboxPage from "./page";

const {
  listMessagesMock,
  getMessageMock,
  markMessageSeenMock,
  deleteMessageMock,
  purgeMailboxMock,
  getMessageSourceMock,
  issueTokenMock,
  streamMailboxEventsMock,
  toastSuccess,
  toastError,
  authStateRef,
  settingsRef,
} = vi.hoisted(() => ({
  listMessagesMock: vi.fn(),
  getMessageMock: vi.fn(),
  markMessageSeenMock: vi.fn(),
  deleteMessageMock: vi.fn(),
  purgeMailboxMock: vi.fn(),
  getMessageSourceMock: vi.fn(),
  issueTokenMock: vi.fn(),
  streamMailboxEventsMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  authStateRef: {
    current: null as {
      mailboxAddress: string | null;
      mailboxToken: string | null;
      setMailboxAuth: ReturnType<typeof vi.fn>;
      clearMailboxAuth: ReturnType<typeof vi.fn>;
    } | null,
  },
  settingsRef: {
    current: {
      autoRefresh: false,
      refreshInterval: 30,
      preferSSE: false,
      defaultTab: "html" as const,
      timeFormat: "relative" as const,
    },
  },
}));

vi.mock("next/navigation", () => ({
  useParams: () => ({ address: "user%40mail.test" }),
}));

vi.mock("@/lib/api", () => ({
  listMessages: (...args: unknown[]) => listMessagesMock(...args),
  getMessage: (...args: unknown[]) => getMessageMock(...args),
  markMessageSeen: (...args: unknown[]) => markMessageSeenMock(...args),
  deleteMessage: (...args: unknown[]) => deleteMessageMock(...args),
  purgeMailbox: (...args: unknown[]) => purgeMailboxMock(...args),
  getMessageSource: (...args: unknown[]) => getMessageSourceMock(...args),
  issueToken: (...args: unknown[]) => issueTokenMock(...args),
  streamMailboxEvents: (...args: unknown[]) => streamMailboxEventsMock(...args),
}));

vi.mock("@/contexts/auth-context", () => ({
  useAuth: () => authStateRef.current,
}));

vi.mock("@/lib/i18n", () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}));

vi.mock("@/lib/settings", () => ({
  useSettings: () => ({ settings: settingsRef.current }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: toastSuccess,
    error: toastError,
  },
}));

vi.mock("@/components/site-header", () => ({
  SiteHeader: () => <div>site-header</div>,
}));

vi.mock("@/components/ui/button", () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  ),
}));

vi.mock("@/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

vi.mock("@/components/inbox/message-list", () => ({
  MessageList: ({
    messages,
    selectedId,
    onSelect,
  }: {
    messages: Array<{ id: string; subject: string }>;
    selectedId: string | null;
    onSelect: (msg: { id: string; subject: string; seen?: boolean }) => void;
  }) => (
    <div>
      {messages.map((msg) => (
        <button key={msg.id} data-selected={selectedId === msg.id} onClick={() => onSelect(msg)}>
          {msg.subject}
        </button>
      ))}
    </div>
  ),
}));

vi.mock("@/components/inbox/message-detail", () => ({
  MessageDetail: ({
    message,
    rawSource,
    onDelete,
    onBack,
  }: {
    message: { subject: string };
    rawSource: string | null;
    onDelete: () => void;
    onBack?: () => void;
  }) => (
    <div>
      <div>detail:{message.subject}</div>
      <div>source:{rawSource ?? "none"}</div>
      <button onClick={onDelete}>delete-selected</button>
      <button onClick={onBack}>back-selected</button>
    </div>
  ),
}));

describe("InboxPage", () => {
  beforeEach(() => {
    authStateRef.current = {
      mailboxAddress: null,
      mailboxToken: null,
      setMailboxAuth: vi.fn(),
      clearMailboxAuth: vi.fn(),
    };
    settingsRef.current = {
      autoRefresh: false,
      refreshInterval: 30,
      preferSSE: false,
      defaultTab: "html",
      timeFormat: "relative",
    };
    listMessagesMock.mockReset();
    getMessageMock.mockReset();
    markMessageSeenMock.mockReset();
    deleteMessageMock.mockReset();
    purgeMailboxMock.mockReset();
    getMessageSourceMock.mockReset();
    issueTokenMock.mockReset();
    streamMailboxEventsMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载消息并可选中未读邮件查看详情", async () => {
    listMessagesMock.mockResolvedValue({
      data: [
        {
          id: "msg-1",
          subject: "Welcome",
          sender: "sender@test.dev",
          recipients: ["user@mail.test"],
          size: 12,
          seen: false,
          received_at: new Date().toISOString(),
          expires_at: new Date(Date.now() + 3600_000).toISOString(),
        },
      ],
      meta: { total: 1, page: 1, per_page: 30 },
    });
    getMessageMock.mockResolvedValue({
      data: {
        id: "msg-1",
        subject: "Welcome",
        sender: "sender@test.dev",
        recipients: ["user@mail.test"],
        size: 12,
        seen: false,
        received_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 3600_000).toISOString(),
        text_body: "hello",
        html_body: "<p>hello</p>",
      },
    });
    markMessageSeenMock.mockResolvedValue({ data: { seen: true } });
    getMessageSourceMock.mockResolvedValue("RAW-SOURCE");

    render(<InboxPage />);

    expect(await screen.findByRole("button", { name: "Welcome" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Welcome" }));

    await waitFor(() => {
      expect(getMessageMock).toHaveBeenCalledWith("user@mail.test", "msg-1");
    });
    expect(markMessageSeenMock).toHaveBeenCalledWith("user@mail.test", "msg-1");
    expect(await screen.findByText("detail:Welcome")).toBeInTheDocument();
    expect(await screen.findByText("source:RAW-SOURCE")).toBeInTheDocument();
  });

  it("鉴权失败时显示解锁表单，并在登录成功后重新拉取消息", async () => {
    listMessagesMock.mockRejectedValue(new Error("forbidden"));
    issueTokenMock.mockImplementation(async () => {
      listMessagesMock.mockResolvedValue({
        data: [
          {
            id: "msg-2",
            subject: "After Login",
            sender: "sender@test.dev",
            recipients: ["user@mail.test"],
            size: 12,
            seen: true,
            received_at: new Date().toISOString(),
            expires_at: new Date(Date.now() + 3600_000).toISOString(),
          },
        ],
        meta: { total: 1, page: 1, per_page: 30 },
      });
      return { data: { token: "mailbox-token" } };
    });

    render(<InboxPage />);

    expect(await screen.findByText("inbox.authTitle")).toBeInTheDocument();
    expect(toastError).toHaveBeenCalled();

    fireEvent.change(screen.getAllByPlaceholderText("inbox.password").at(-1) as HTMLInputElement, {
      target: { value: "Passw0rd!" },
    });
    fireEvent.click(screen.getAllByRole("button", { name: "inbox.unlock" }).at(-1) as HTMLButtonElement);

    await waitFor(() => {
      expect(issueTokenMock).toHaveBeenCalledWith("user@mail.test", "Passw0rd!");
    });
    expect(authStateRef.current?.setMailboxAuth).toHaveBeenCalledWith("user@mail.test", "mailbox-token");
    await waitFor(() => {
      expect(listMessagesMock.mock.calls.length).toBeGreaterThanOrEqual(2);
    });
    expect(screen.queryByText("inbox.authTitle")).not.toBeInTheDocument();
    expect(toastSuccess).toHaveBeenCalledWith("toast.tokenIssued");
  });
});
