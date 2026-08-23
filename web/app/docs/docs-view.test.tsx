import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { DocsEndpoints, DocsTabs, DocsViewProvider, type DocsLinks, type DocsTabLabels } from "./docs-view";

const { writeTextMock, toastSuccess, toastError } = vi.hoisted(() => ({
  writeTextMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    success: toastSuccess,
    error: toastError,
  },
}));

vi.mock("@/components/ui/button", () => ({
  Button: ({
    children,
    render: renderProp,
    ...props
  }: React.ButtonHTMLAttributes<HTMLButtonElement> & { render?: React.ReactElement }) =>
    renderProp ? React.cloneElement(renderProp, undefined, children) : <button {...props}>{children}</button>,
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

// The page resolves the API origin and every label on the server, so the test
// hands the islands the same finished values a Server Component would.
const origin = "https://api.tabmail.test";

const links: DocsLinks = {
  docs: "/docs",
  redoc: "/redoc",
  openapi: "/openapi.yaml",
  health: "/health",
};

const tabLabels: DocsTabLabels = {
  swaggerUi: "Swagger UI",
  redoc: "ReDoc",
  quickstart: "Quickstart",
  deploy: "Deploy",
  domains: "Domains",
  api: "API",
  ops: "Ops",
  liveRendered: "Live rendered",
};

function renderDocs() {
  return render(
    <DocsViewProvider>
      <DocsEndpoints
        origin={origin}
        links={links}
        labels={{
          baseUrl: "Base URL",
          swaggerUi: "Swagger UI",
          redoc: "ReDoc",
          openapi: "OpenAPI",
          health: "Health",
        }}
      />
      <DocsTabs origin={origin} links={links} labels={tabLabels} />
    </DocsViewProvider>
  );
}

describe("docs view", () => {
  beforeEach(() => {
    writeTextMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
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
    renderDocs();

    expect(screen.getByText(origin)).toBeInTheDocument();
    const iframe = screen.getByTitle("Swagger UI");
    expect(iframe).toHaveAttribute("src", `${origin}/docs`);
    expect(screen.getByText(`${origin}/openapi.yaml`)).toBeInTheDocument();
  });

  it("支持切换 quickstart 并复制 curl 示例", async () => {
    writeTextMock.mockResolvedValue(undefined);

    renderDocs();

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
