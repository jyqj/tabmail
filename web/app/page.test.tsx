import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import HomePage from "./page";

const { pushMock } = vi.hoisted(() => ({
  pushMock: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: pushMock,
  }),
}));

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock("@/components/site-header", () => ({
  SiteHeader: () => <div>site-header</div>,
}));

vi.mock("@/lib/i18n", () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}));

vi.mock("@/components/ui/button", () => ({
  Button: ({
    children,
    render,
    ...props
  }: React.ButtonHTMLAttributes<HTMLButtonElement> & { render?: React.ReactElement }) =>
    render ? React.cloneElement(render, undefined, children) : <button {...props}>{children}</button>,
}));

vi.mock("@/components/ui/input", async () => {
  const ReactModule = await import("react");
  return {
    Input: ReactModule.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
      (props, ref) => <input ref={ref} {...props} />
    ),
  };
});

vi.mock("@/components/ui/card", () => ({
  Card: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

describe("home page", () => {
  beforeEach(() => {
    pushMock.mockReset();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("支持输入地址后打开 inbox", async () => {
    render(<HomePage />);

    fireEvent.change(screen.getByPlaceholderText("home.placeholder"), {
      target: { value: "user@mail.test" },
    });
    fireEvent.click(screen.getByRole("button", { name: "home.openInbox" }));

    await waitFor(() => {
      expect(pushMock).toHaveBeenCalledWith("/inbox/user%40mail.test");
    });
  });

  it("支持生成随机地址并跳转", async () => {
    vi.spyOn(Math, "random").mockReturnValue(0);

    render(<HomePage />);

    fireEvent.click(screen.getByRole("button", { name: /home.random/i }));

    await waitFor(() => {
      expect(pushMock).toHaveBeenCalledWith("/inbox/aaaaaaaa%40tabmail.dev");
    });
    expect(screen.getByDisplayValue("aaaaaaaa@tabmail.dev")).toBeInTheDocument();
  });
});
