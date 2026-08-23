import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AddressSearch, type AddressSearchLabels } from "./home-interactive";

const { pushMock, listDomainsMock, suggestAddressMock } = vi.hoisted(() => ({
  pushMock: vi.fn(),
  listDomainsMock: vi.fn(),
  suggestAddressMock: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: pushMock,
  }),
}));

vi.mock("@/lib/api", () => ({
  listOpenDomains: (...args: unknown[]) => listDomainsMock(...args),
  suggestOpenAddress: (...args: unknown[]) => suggestAddressMock(...args),
}));

vi.mock("@/components/ui/button", () => ({
  Button: ({
    children,
    render: renderProp,
    ...props
  }: React.ButtonHTMLAttributes<HTMLButtonElement> & { render?: React.ReactElement }) =>
    renderProp ? React.cloneElement(renderProp, undefined, children) : <button {...props}>{children}</button>,
}));

vi.mock("@/components/ui/input", async () => {
  const ReactModule = await import("react");
  return {
    Input: ReactModule.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
      (props, ref) => <input ref={ref} {...props} />
    ),
  };
});

// The page translates on the server and hands the island finished strings, so
// the labels stand in for what a Server Component would pass.
const labels: AddressSearchLabels = {
  placeholder: "home.placeholder",
  openInbox: "home.openInbox",
  random: "home.random",
  generating: "home.generating",
  curlHint: "home.curlHint",
  noRegister: "home.noRegister",
  noDomains: "home.noDomains",
  randomFailed: "home.randomFailed",
};

describe("home address search", () => {
  beforeEach(() => {
    pushMock.mockReset();
    listDomainsMock.mockReset();
    suggestAddressMock.mockReset();
    listDomainsMock.mockResolvedValue({
      data: [
        {
          id: "zone-1",
          domain: "tabmail.dev",
          is_verified: true,
          mx_verified: true,
          allow_random_subdomains: false,
        },
      ],
    });
    suggestAddressMock.mockResolvedValue({ data: { address: "aaaaaaaa@tabmail.dev" } });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("支持输入地址后打开 inbox", async () => {
    render(<AddressSearch labels={labels} />);

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

    render(<AddressSearch labels={labels} />);

    fireEvent.click(screen.getByRole("button", { name: /home.random/i }));

    await waitFor(() => {
      expect(pushMock).toHaveBeenCalledWith("/inbox/aaaaaaaa%40tabmail.dev");
    });
    expect(screen.getByDisplayValue("aaaaaaaa@tabmail.dev")).toBeInTheDocument();
  });
});
