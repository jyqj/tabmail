import React from "react";
import { readFileSync } from "node:fs";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { expect, test } from "vitest";
import useSWR from "swr";

test("R5 batch independent realm loopback PostgreSQL", async () => {
  const path = process.env.TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE;
  if (!path) throw new Error("explicit private fixture required");
  const fixture: { case_id: string; api_url: string; token: string } = JSON.parse(readFileSync(path, "utf8"));
  expect(process.env.VITEST_WORKER_ID).toMatch(/^\d+$/);
  // Each child must start with an empty auth realm even under concurrent owners.
  expect(sessionStorage.getItem("r5-auth")).toBeNull();
  sessionStorage.setItem("r5-auth", fixture.token);
  const denied = await fetch(fixture.api_url, { headers: { Authorization: "Bearer foreign-owner" } });
  expect(denied.status).toBe(401);
  function View() {
    const { data } = useSWR("identical-swr-key-across-all-independent-realms", async () => {
      const response = await fetch(fixture.api_url, { headers: { Authorization: `Bearer ${sessionStorage.getItem("r5-auth")}` } });
      expect(response.status).toBe(200);
      return (await response.json()) as { identity: string };
    });
    return <p data-testid="database-identity">{data?.identity}</p>;
  }
  try {
    render(<View />);
    await waitFor(() => expect(screen.getByTestId("database-identity").textContent).toBe(fixture.case_id));
    cleanup();
    render(<View />);
    await waitFor(() => expect(screen.getByTestId("database-identity").textContent).toBe(fixture.case_id));
  } finally {
    cleanup(); sessionStorage.clear();
  }
});
