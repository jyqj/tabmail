import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { revokeMock, companyMock, useAPIMock } = vi.hoisted(() => ({
  revokeMock: vi.fn(),
  companyMock: vi.fn(),
  useAPIMock: vi.fn(),
}));

vi.mock("@/lib/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/company")>()),
  company: (...args: unknown[]) => companyMock(...args),
  revokeTemplateVersion: (...args: unknown[]) => revokeMock(...args),
}));

vi.mock("@/hooks/use-api", () => ({ useAPI: useAPIMock }));

import { TemplateVersionsView } from "./versions";
import type { MailTemplate, TemplateVersion } from "@/lib/company";

const template: MailTemplate = {
  id: "tpl-1",
  name: "Welcome",
  revision: 7,
  retired: false,
  draft: { subject: "Hello", text_body: "Body", html_body: "", variables: [] },
};

const version = (over: Partial<TemplateVersion>): TemplateVersion => ({
  id: "ver-2",
  template_id: "tpl-1",
  name: "Welcome",
  version: 2,
  snapshot: template.draft,
  published_at: "2026-09-17T00:00:00Z",
  content_hash: "a".repeat(64),
  ...over,
});

const liveVersions = [
  version({ id: "ver-2", version: 2 }),
  version({ id: "ver-1", version: 1, revoked_at: "2026-09-17T12:00:00Z" }),
];

function wireUseAPI(data: TemplateVersion[] = liveVersions) {
  const mutate = vi.fn(async () => data);
  useAPIMock.mockImplementation(() => ({
    data,
    error: undefined,
    isLoading: false,
    mutate,
  }));
  return mutate;
}

describe("TemplateVersionsView revoke flow", () => {
  beforeEach(() => {
    revokeMock.mockReset().mockResolvedValue({ revoked: true });
    companyMock.mockReset().mockResolvedValue(liveVersions);
  });

  afterEach(() => cleanup());

  it("offers revoke only on live versions, never reusing the retire wording", async () => {
    wireUseAPI();
    render(<TemplateVersionsView template={template} refreshKey={0} />);
    await waitFor(() =>
      expect(screen.getByTestId("revoke-version-2")).toBeTruthy(),
    );
    expect(screen.queryByTestId("revoke-version-1")).toBeNull();
    expect(screen.getByText(/Revoked/)).toBeTruthy();
    // Retire copy lives in the library view ("Retire (including queued
    // sends)"); the version action must never reuse it.
    expect(screen.queryByText(/Retire/)).toBeNull();
    expect(screen.queryByText(/停用/)).toBeNull();
  });

  it("aborts without calling the API when the confirm is dismissed", async () => {
    wireUseAPI();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<TemplateVersionsView template={template} refreshKey={0} />);
    await waitFor(() =>
      expect(screen.getByTestId("revoke-version-2")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("revoke-version-2"));
    await waitFor(() => expect(confirm).toHaveBeenCalled());
    expect(confirm.mock.calls[0][0]).toMatch(/[Rr]evoke this version\?/);
    expect(revokeMock).not.toHaveBeenCalled();
  });

  it("revokes with the current revision, states the irreversibility and refreshes", async () => {
    const mutate = wireUseAPI();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const onRevoked = vi.fn().mockResolvedValue(undefined);
    render(
      <TemplateVersionsView
        template={template}
        refreshKey={0}
        onRevoked={onRevoked}
      />,
    );
    await waitFor(() =>
      expect(screen.getByTestId("revoke-version-2")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("revoke-version-2"));
    await waitFor(() => expect(onRevoked).toHaveBeenCalled());
    // The confirm must spell out: irreversible, undelivered sends stop,
    // delivered outcomes untouched.
    expect(confirm.mock.calls[0][0]).toMatch(/Irreversible/);
    expect(confirm.mock.calls[0][0]).toMatch(/not started yet/);
    expect(confirm.mock.calls[0][0]).toMatch(/Already-delivered/);
    expect(revokeMock).toHaveBeenCalledWith("tpl-1", 2, 7);
    expect(mutate).toHaveBeenCalled();
  });
});
