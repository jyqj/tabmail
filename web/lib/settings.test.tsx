import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { SettingsProvider, useSettings } from "./settings";

function SettingsProbe() {
  const { settings, update } = useSettings();

  return (
    <div>
      <div data-testid="autoRefresh">{String(settings.autoRefresh)}</div>
      <div data-testid="refreshInterval">{String(settings.refreshInterval)}</div>
      <div data-testid="preferSSE">{String(settings.preferSSE)}</div>
      <div data-testid="defaultTab">{settings.defaultTab}</div>
      <div data-testid="timeFormat">{settings.timeFormat}</div>

      <button
        onClick={() =>
          update({
            autoRefresh: false,
            refreshInterval: 30,
            preferSSE: false,
            defaultTab: "source",
            timeFormat: "absolute",
          })
        }
      >
        update-settings
      </button>
    </div>
  );
}

describe("settings", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
  });

  it("默认返回内置 defaults", () => {
    render(
      <SettingsProvider>
        <SettingsProbe />
      </SettingsProvider>
    );

    expect(screen.getByTestId("autoRefresh")).toHaveTextContent("true");
    expect(screen.getByTestId("refreshInterval")).toHaveTextContent("10");
    expect(screen.getByTestId("preferSSE")).toHaveTextContent("true");
    expect(screen.getByTestId("defaultTab")).toHaveTextContent("html");
    expect(screen.getByTestId("timeFormat")).toHaveTextContent("relative");
  });

  it("会从 localStorage 合并设置并支持 update 持久化", async () => {
    localStorage.setItem(
      "tabmail-settings",
      JSON.stringify({ refreshInterval: 15, defaultTab: "text" })
    );

    render(
      <SettingsProvider>
        <SettingsProbe />
      </SettingsProvider>
    );

    expect(screen.getByTestId("refreshInterval")).toHaveTextContent("15");
    expect(screen.getByTestId("defaultTab")).toHaveTextContent("text");
    expect(screen.getByTestId("autoRefresh")).toHaveTextContent("true");

    fireEvent.click(screen.getByRole("button", { name: "update-settings" }));

    await waitFor(() => {
      expect(screen.getByTestId("autoRefresh")).toHaveTextContent("false");
    });
    expect(screen.getByTestId("refreshInterval")).toHaveTextContent("30");
    expect(screen.getByTestId("preferSSE")).toHaveTextContent("false");
    expect(screen.getByTestId("defaultTab")).toHaveTextContent("source");
    expect(screen.getByTestId("timeFormat")).toHaveTextContent("absolute");

    expect(JSON.parse(localStorage.getItem("tabmail-settings") || "{}")).toEqual({
      autoRefresh: false,
      refreshInterval: 30,
      preferSSE: false,
      defaultTab: "source",
      timeFormat: "absolute",
    });
  });
});
