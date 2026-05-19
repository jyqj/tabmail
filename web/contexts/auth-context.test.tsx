import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { AuthProvider, useAuth } from "./auth-context";

function AuthProbe() {
  const auth = useAuth();

  return (
    <div>
      <div data-testid="level">{auth.level}</div>
      <div data-testid="accessToken">{auth.accessToken ?? ""}</div>
      <div data-testid="tenantId">{auth.tenantId ?? ""}</div>
      <div data-testid="mailboxAddress">{auth.mailboxAddress ?? ""}</div>
      <div data-testid="mailboxToken">{auth.mailboxToken ?? ""}</div>

      <button
        onClick={() =>
          auth.loginWithTokens(" access-token ", " refresh-token ", {
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
      <button onClick={() => auth.setMailboxAuth("User@Mail.Test ", " mailbox-token ")}>set-mailbox</button>
      <button onClick={() => auth.clearMailboxAuth()}>clear-mailbox</button>
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
      </AuthProvider>
    );

    expect(screen.getByTestId("level")).toHaveTextContent("public");
    expect(screen.getByTestId("accessToken")).toHaveTextContent("");
    expect(screen.getByTestId("mailboxToken")).toHaveTextContent("");
  });

  it("支持 JWT / tenant / mailbox 状态写入与清理", async () => {
    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>
    );

    fireEvent.click(screen.getByRole("button", { name: "set-jwt" }));
    await waitFor(() => {
      expect(screen.getByTestId("level")).toHaveTextContent("user");
    });
    expect(localStorage.getItem("tabmail_access_token")).toBe(" access-token ");
    expect(localStorage.getItem("tabmail_refresh_token")).toBe(" refresh-token ");

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

    fireEvent.click(screen.getByRole("button", { name: "set-mailbox" }));
    await waitFor(() => {
      expect(screen.getByTestId("level")).toHaveTextContent("mailbox");
    });
    expect(localStorage.getItem("tabmail_mailbox_address")).toBe("user@mail.test");
    expect(localStorage.getItem("tabmail_mailbox_token")).toBe("mailbox-token");

    fireEvent.click(screen.getByRole("button", { name: "clear-mailbox" }));
    await waitFor(() => {
      expect(screen.getByTestId("level")).toHaveTextContent("public");
    });
    expect(localStorage.getItem("tabmail_mailbox_address")).toBeNull();
    expect(localStorage.getItem("tabmail_mailbox_token")).toBeNull();
  });
});
