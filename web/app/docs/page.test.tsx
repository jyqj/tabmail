import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import DocsPage from "./page";

const {
  getBaseUrlMock,
  writeTextMock,
  toastSuccess,
  toastError,
} = vi.hoisted(() => ({
  getBaseUrlMock: vi.fn(),
  writeTextMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  getBaseUrl: () => getBaseUrlMock(),
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
  Button: ({
    children,
    render,
    ...props
  }: React.ButtonHTMLAttributes<HTMLButtonElement> & { render?: React.ReactElement }) =>
    render ? React.cloneElement(render, undefined, children) : <button {...props}>{children}</button>,
}));

vi.mock("@/components/ui/card", () => ({
  Card: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/components/ui/badge", () => ({
  Badge: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("@/components/ui/tabs", async () => {
  const ReactModule = await import("react");
  const TabsContext = ReactModule.createContext<{
    activeValue: string;
    onTabChange: (value: string) => void;
  } | null>(null);

  return {
    Tabs: ({
      value,
      onValueChange,
      children,
    }: {
      value: string;
      onValueChange: (value: string) => void;
      children: React.ReactNode;
    }) => (
      <TabsContext.Provider value={{ activeValue: value, onTabChange: onValueChange }}>
        <div data-testid="tabs-root" data-value={value}>{children}</div>
      </TabsContext.Provider>
    ),
    TabsList: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    TabsTrigger: ({
      children,
      value,
    }: {
      children: React.ReactNode;
      value: string;
    }) => {
      const ctx = ReactModule.useContext(TabsContext);
      return <button type="button" onClick={() => ctx?.onTabChange(value)}>{children}</button>;
    },
    TabsContent: ({
      children,
      value,
    }: {
      children: React.ReactNode;
      value: string;
    }) => {
      const ctx = ReactModule.useContext(TabsContext);
      return ctx?.activeValue === value ? <div>{children}</div> : null;
    },
  };
});

describe("docs page", () => {
  beforeEach(() => {
    getBaseUrlMock.mockReset();
    writeTextMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
    getBaseUrlMock.mockReturnValue("https://api.tabmail.test");
    Object.assign(navigator, {
      clipboard: {
        writeText: writeTextMock,
      },
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("默认展示 swagger 并渲染基于 base url 的链接", async () => {
    render(<DocsPage />);

    expect(screen.getByText("TabMail API Portal")).toBeInTheDocument();
    expect(screen.getByText("https://api.tabmail.test")).toBeInTheDocument();
    const iframe = screen.getByTitle("Swagger UI");
    expect(iframe).toHaveAttribute("src", "https://api.tabmail.test/docs");
    expect(screen.getByText("https://api.tabmail.test/openapi.yaml")).toBeInTheDocument();
  });

  it("支持切换 quickstart 并复制 curl 示例", async () => {
    writeTextMock.mockResolvedValue(undefined);

    render(<DocsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Quickstart" }));

    expect(await screen.findByText("Auth matrix")).toBeInTheDocument();
    expect(screen.getAllByText("Health").length).toBeGreaterThan(0);

    const copyButtons = screen.getAllByRole("button", { name: "Copy" });
    fireEvent.click(copyButtons[0]);

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith('curl "$BASE_URL/health"');
    });
    expect(toastSuccess).toHaveBeenCalledWith("Health curl copied");
  });
});
