import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import {
  cleanup,
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import MailPage from "./page";
const mocks = vi.hoisted(() => ({
  send: vi.fn(),
  preview: vi.fn(),
  memberRole: "restricted",
}));
vi.mock("@/contexts/auth-context", () => ({
  useAuth: () => ({ user: { id: "employee" } }),
}));
vi.mock("@/hooks/use-company", () => ({
  useCompany: () => ({
    data: {
      data: {
        company: { tenant_id: "tenant", name: "Company" },
        member: {
          user_id: "employee",
          display_name: "Alice",
          email: "alice@company.test",
          is_active: true,
          company_role: mocks.memberRole,
        },
      },
    },
    mutate: vi.fn(),
  }),
}));
vi.mock("@/components/inbox/message-detail", () => ({
  MessageDetail: () => null,
}));
vi.mock("@/lib/api/company", async () => {
  const actual = await vi.importActual<object>("@/lib/api/company");
  return {
    ...actual,
    sendCompanyMail: mocks.send,
    previewTemplate: mocks.preview,
  };
});
vi.mock("swr", () => ({
  default: (key: unknown[]) => {
    const name = key?.[0];
    const data =
      name === "mail-grants"
        ? [
            {
              mailbox_id: "mailbox",
              user_id: "employee",
              address: "alice@company.test",
              can_read: true,
              can_send: true,
              template_only: false,
            },
          ]
        : name === "mail-templates"
          ? [
              {
                id: "version",
                name: "Welcome",
                version: 1,
                status: "published",
                mailbox_ids: ["mailbox"],
                variables: { customer: 80 },
              },
            ]
          : [];
    return { data: { data, meta: { total: 0 } }, mutate: vi.fn() };
  },
}));
describe("employee workbench", () => {
  afterEach(cleanup);
  beforeEach(() => {
    mocks.send.mockReset();
    mocks.send.mockResolvedValue({ data: { state: "pending" } });
    mocks.memberRole = "restricted";
  });
  it("offers only authorized exact senders and enforces template selection", async () => {
    render(<MailPage />);
    fireEvent.click(screen.getByRole("button", { name: "写邮件" }));
    expect(screen.getByLabelText("发件身份")).toHaveTextContent(
      "alice@company.test",
    );
    expect(
      screen.queryByRole("textbox", { name: "发件身份" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交发送" })).toBeDisabled();
    expect(screen.queryByLabelText("正文")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("收件人"), {
      target: { value: "customer@example.test" },
    });
    fireEvent.change(screen.getByLabelText("邮件模板"), {
      target: { value: "version" },
    });
    fireEvent.change(screen.getByLabelText("customer"), {
      target: { value: "Customer" },
    });
    fireEvent.click(screen.getByRole("button", { name: "提交发送" }));
    await waitFor(() =>
      expect(mocks.send).toHaveBeenCalledWith(
        expect.objectContaining({
          from: "alice@company.test",
          templateId: "version",
          variables: { customer: "Customer" },
          templateRequired: true,
        }),
      ),
    );
  });
  it("allows ordinary employees to compose with their granted address", () => {
    mocks.memberRole = "employee";
    render(<MailPage />);
    fireEvent.click(screen.getByRole("button", { name: "写邮件" }));
    expect(screen.getByLabelText("正文")).toBeInTheDocument();
    expect(screen.getByLabelText("主题")).toBeInTheDocument();
  });
});
