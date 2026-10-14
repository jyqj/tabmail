import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { AuthDialog } from "./auth-dialog";

beforeEach(() => {
  vi.stubGlobal("navigator", { locks: { request: vi.fn(async (_name: string, run: () => Promise<unknown>) => run()) } });
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => new Response(JSON.stringify(
    String(input).includes("/auth/login")
      ? { data: { access_token: "candidate-token", user: { id: "candidate", tenant_id: "tenant", email: "candidate@example.test", display_name: "Candidate", role: "user" } } }
      : { data: { can_send: true } },
  ), { status: 200, headers: { "Content-Type": "application/json" } }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it("keeps ordinary login completion feedback under the real AuthProvider", async () => {
  render(<AuthProvider><AuthDialog /></AuthProvider>);
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)); });
  fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
  fireEvent.change(await screen.findByLabelText("Email address"), { target: { value: "candidate@example.test" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "synthetic-password" } });
  fireEvent.keyDown(screen.getByLabelText("Password"), { key: "Enter" });
  await waitFor(() => expect(screen.getByText("Candidate")).toBeVisible());
  expect(localStorage.getItem("tabmail_access_token")).toBe("candidate-token");
  await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Welcome, Candidate"));
});
