import type { ReactNode } from "react";
import {
  cleanup,
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AdminIngestPage from "./page";
import { I18nProvider } from "@/lib/i18n";
const { render: renderActual } = await vi.importActual<
  typeof import("@testing-library/react")
>("@testing-library/react");
function renderPage() {
  return renderActual(<AdminIngestPage />, { wrapper: I18nProvider });
}
import * as api from "@/lib/api/ingress-recovery";
import {
  deferred,
  job,
  listed,
  receipt,
  session,
  storeSession,
} from "@/test/ingress-fixtures";
import type { PermissionLevel } from "@/lib/permissions";
const fixture = vi.hoisted(() => ({
  auth: {
    level: "super_admin" as PermissionLevel,
    hydrated: true,
    user: { id: "operator-a" },
    accessToken: "operator-token-a",
    tenantId: "tenant-a",
  },
}));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => fixture.auth }));
vi.mock("@/components/layout/page-header", () => ({
  PageHeader: ({
    title,
    description,
    actions,
  }: {
    title: string;
    description?: string;
    actions?: ReactNode;
  }) => (
    <header>
      <h1>{title}</h1>
      <p>{description}</p>
      {actions}
    </header>
  ),
}));
afterEach(() => cleanup());
beforeEach(() => {
  fixture.auth = {
    level: "super_admin",
    hydrated: true,
    user: { id: session.userId },
    accessToken: session.accessToken,
    tenantId: session.tenantId!,
  };
  storeSession();
  vi.spyOn(api, "listIngress").mockResolvedValue(listed());
  vi.spyOn(api, "inspectIngress").mockResolvedValue({
    data: structuredClone(receipt),
  });
  vi.spyOn(api, "retryIngress").mockResolvedValue({ data: { requeued: true } });
});
async function inspect() {
  fireEvent.click(
    await screen.findByRole("button", { name: `Inspect ${job.id}` }),
  );
  return screen.findByRole("region", { name: "Mailbox outcomes" });
}
describe("ingress operations workflow", () => {
  it.each(["user", "admin", "public", "mailbox"] as const)(
    "does not mount protected requests for %s",
    async (level) => {
      fixture.auth.level = level;
      renderPage();
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Platform administrators only",
      );
      expect(api.listIngress).not.toHaveBeenCalled();
    },
  );
  it("applies exact filters explicitly and pages without keeping another receipt selected", async () => {
    vi.mocked(api.listIngress).mockResolvedValue(listed(31));
    renderPage();
    await inspect();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() =>
      expect(api.listIngress).toHaveBeenLastCalledWith(
        session,
        expect.objectContaining({ page: 2 }),
        expect.any(AbortSignal),
      ),
    );
    expect(
      screen.queryByRole("region", { name: "Mailbox outcomes" }),
    ).not.toBeInTheDocument();
    fireEvent.change(
      screen.getByRole("textbox", { name: "Exact envelope recipient" }),
      { target: { value: "alice+tag@example.test" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() =>
      expect(api.listIngress).toHaveBeenLastCalledWith(
        session,
        expect.objectContaining({
          page: 1,
          recipient: "alice+tag@example.test",
        }),
        expect.any(AbortSignal),
      ),
    );
  });
  it("shows the ledger and requires both review and reason, submitting once", async () => {
    const pending = deferred<{ data: { requeued: boolean } }>();
    vi.mocked(api.retryIngress).mockReturnValue(pending.promise);
    renderPage();
    const panel = await inspect();
    expect(
      await within(panel).findByText("Committed locally"),
    ).toBeInTheDocument();
    expect(within(panel).getByText("quota full")).toBeInTheDocument();
    const button = within(panel).getByRole("button", {
      name: "Requeue held destinations",
    });
    expect(button).toBeDisabled();
    fireEvent.change(
      within(panel).getByRole("textbox", { name: "Recovery reason" }),
      { target: { value: "Storage repaired" } },
    );
    fireEvent.click(within(panel).getByRole("checkbox"));
    fireEvent.click(button);
    fireEvent.click(button);
    expect(api.retryIngress).toHaveBeenCalledTimes(1);
    vi.mocked(api.inspectIngress).mockResolvedValue({
      data: {
        ...receipt,
        state: "pending",
        can_retry: false,
        retry_block_reason: "not_held",
        targets: receipt.targets.map((t) => ({
          ...t,
          state: t.state === "held" ? "pending" : t.state,
        })),
      },
    });
    await act(async () => pending.resolve({ data: { requeued: true } }));
    expect(
      await screen.findByText(/Recovery request committed/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Requeue held destinations" }),
    ).not.toBeInTheDocument();
  });
  it("explains legacy receipts instead of offering a fake retry action", async () => {
    vi.mocked(api.inspectIngress).mockResolvedValue({
      data: {
        ...receipt,
        recovery_managed: false,
        targets: [],
        can_retry: false,
        retry_block_reason: "legacy_receipt",
      },
    });
    renderPage();
    await inspect();
    expect(
      await screen.findByText(/Legacy receipt: no trustworthy/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "Recovery reason" }),
    ).not.toBeInTheDocument();
  });
  it("hides previous details and retry controls when a refresh fails", async () => {
    renderPage();
    const panel = await inspect();
    await within(panel).findByText("quota full");
    vi.mocked(api.inspectIngress).mockRejectedValue(new Error("offline"));
    fireEvent.click(within(panel).getByRole("button", { name: "Refresh" }));
    expect(await within(panel).findByRole("alert")).toHaveTextContent(
      "Unable to load",
    );
    expect(within(panel).queryByText("quota full")).not.toBeInTheDocument();
    expect(within(panel).queryByRole("checkbox")).not.toBeInTheDocument();
  });
  it("never replays network-failed mutations and refreshes before another review", async () => {
    vi.mocked(api.retryIngress).mockRejectedValue(new Error("network lost"));
    renderPage();
    const panel = await inspect();
    fireEvent.change(
      await within(panel).findByRole("textbox", { name: "Recovery reason" }),
      { target: { value: "Fixed" } },
    );
    fireEvent.click(within(panel).getByRole("checkbox"));
    fireEvent.click(
      within(panel).getByRole("button", { name: "Requeue held destinations" }),
    );
    expect(
      await screen.findByText(/NOT automatically replayed/),
    ).toBeInTheDocument();
    expect(api.retryIngress).toHaveBeenCalledTimes(1);
    expect(
      await screen.findByRole("textbox", { name: "Recovery reason" }),
    ).toHaveValue("");
  });
  it("clears confidential results/review text on account changes and ignores a late mutation", async () => {
    const pending = deferred<{ data: { requeued: boolean } }>();
    vi.mocked(api.retryIngress).mockReturnValue(pending.promise);
    const view = renderPage();
    const panel = await inspect();
    fireEvent.change(
      await within(panel).findByRole("textbox", { name: "Recovery reason" }),
      { target: { value: "Old private reason" } },
    );
    fireEvent.click(within(panel).getByRole("checkbox"));
    fireEvent.click(
      within(panel).getByRole("button", { name: "Requeue held destinations" }),
    );
    const signal = vi.mocked(api.retryIngress).mock.calls[0][3];
    fixture.auth = {
      ...fixture.auth,
      user: { id: "operator-b" },
      accessToken: "operator-token-b",
    };
    storeSession({
      ...session,
      userId: "operator-b",
      accessToken: "operator-token-b",
    });
    vi.mocked(api.listIngress).mockResolvedValue({
      data: [],
      meta: { page: 1, per_page: 30, total: 0 },
    });
    view.rerender(<AdminIngestPage />);
    expect(signal.aborted).toBe(true);
    expect(
      screen.queryByDisplayValue("Old private reason"),
    ).not.toBeInTheDocument();
    await act(async () => pending.resolve({ data: { requeued: true } }));
    expect(
      screen.queryByText(/Recovery request committed/),
    ).not.toBeInTheDocument();
  });
  it("does not paint a late list response into a replacement operator session", async () => {
    const pending = deferred<ReturnType<typeof listed>>();
    vi.mocked(api.listIngress).mockReturnValueOnce(pending.promise);
    const view = renderPage();
    fixture.auth = {
      ...fixture.auth,
      user: { id: "operator-b" },
      accessToken: "operator-token-b",
    };
    storeSession({
      ...session,
      userId: "operator-b",
      accessToken: "operator-token-b",
    });
    vi.mocked(api.listIngress).mockResolvedValue({
      data: [],
      meta: { page: 1, per_page: 30, total: 0 },
    });
    view.rerender(<AdminIngestPage />);
    await act(async () => pending.resolve(listed()));
    expect(
      await screen.findByText("No receipts match these filters."),
    ).toBeInTheDocument();
    expect(screen.queryByText("sender@example.test")).not.toBeInTheDocument();
  });
  it("hides mutations when the fresh snapshot rejects authorization", async () => {
    vi.mocked(api.inspectIngress).mockRejectedValue(
      new api.IngressRequestError(403, "denied"),
    );
    renderPage();
    await inspect();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "no longer has permission",
    );
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });
  it("renders Chinese operator guidance in Chinese sessions", async () => {
    localStorage.setItem("tabmail-locale", "zh");
    renderPage();
    expect(await screen.findByText("入站恢复")).toBeInTheDocument();
  });
});
