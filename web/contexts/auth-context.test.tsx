import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api/auth", () => ({
  logoutSession: vi.fn().mockResolvedValue({}),
}));
vi.mock("@/lib/api/permissions", () => ({
  getMyPermissions: vi.fn().mockResolvedValue({ data: { can_send: true } }),
}));
import { AuthProvider, useAuth } from "./auth-context";

function AuthProbe() {
  const auth = useAuth();

  return (
    <div>
      <div data-testid="level">{auth.level}</div>
      <div data-testid="accessToken">{auth.accessToken ?? ""}</div>
      <div data-testid="tenantId">{auth.tenantId ?? ""}</div>

      <button
        onClick={() =>
          auth.loginWithTokens(" access-token ", {
            id: "user-1",
            email: "user@mail.test",
            display_name: "User",
            role: "user",
            tenant_id: "tenant-1",
          })
        }
      >
        set-jwt
      </button>
      <button onClick={() => auth.setTenantId(" tenant-1 ")}>set-tenant</button>
      <button onClick={() => auth.logout()}>logout</button>
    </div>
  );
}

describe("auth-context", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
  });

  it("默认从 localStorage 读取并推导 public level", () => {
    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    );

    expect(screen.getByTestId("level")).toHaveTextContent("public");
    expect(screen.getByTestId("accessToken")).toHaveTextContent("");
  });

  it("支持 JWT / tenant 状态写入与清理", async () => {
    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "set-jwt" }));
    await waitFor(() => {
      expect(screen.getByTestId("level")).toHaveTextContent("user");
    });
    expect(localStorage.getItem("tabmail_access_token")).toBe(" access-token ");

    fireEvent.click(screen.getByRole("button", { name: "set-tenant" }));
    await waitFor(() => {
      expect(screen.getByTestId("tenantId")).toHaveTextContent("tenant-1");
    });
    expect(localStorage.getItem("tabmail_tenant_id")).toBe("tenant-1");

    fireEvent.click(screen.getByRole("button", { name: "logout" }));
    await waitFor(() => {
      expect(screen.getByTestId("level")).toHaveTextContent("public");
    });
    expect(localStorage.getItem("tabmail_tenant_id")).toBeNull();
  });
});
