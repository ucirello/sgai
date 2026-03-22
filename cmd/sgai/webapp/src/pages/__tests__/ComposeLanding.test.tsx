import { afterEach, beforeEach, describe, expect, it, mock } from "bun:test";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";

const mockComposeTemplates = mock(() => Promise.resolve({ templates: [] }));

let mockWorkspaces = [{ name: "repo-dir", title: "Repository Title" }];

mock.module("@/lib/api", () => ({
  api: {
    compose: {
      templates: mockComposeTemplates,
    },
  },
}));

mock.module("@/lib/factory-state", () => ({
  useFactoryState: () => ({
    workspaces: mockWorkspaces,
    fetchStatus: "idle",
    lastFetchedAt: Date.now(),
  }),
  triggerFactoryRefresh: mock(() => {}),
}));

import { ComposeLanding } from "../ComposeLanding";

function renderComposeLanding(initialEntry = "/compose?workspace=repo-dir") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <TooltipProvider>
        <Routes>
          <Route path="/compose" element={<ComposeLanding />} />
        </Routes>
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("ComposeLanding repository labels", () => {
  beforeEach(() => {
    mockComposeTemplates.mockClear();
    mockWorkspaces = [{ name: "repo-dir", title: "Repository Title" }];
  });

  afterEach(() => {
    cleanup();
  });

  it("shows template details in an app tooltip when the template card receives keyboard focus", async () => {
    mockComposeTemplates.mockImplementationOnce(() => Promise.resolve({
      templates: [
        {
          id: "template-1",
          name: "Very Long Template Name That Needs A Tooltip",
          description: "Long template description that should be available through the app tooltip on keyboard focus.",
          icon: "🧪",
          agents: [],
          flow: "",
        },
      ],
    }));

    renderComposeLanding();

    const templateCard = await screen.findByRole("link", {
      name: /Very Long Template Name That Needs A Tooltip/i,
    });

    templateCard.focus();

    expect(document.activeElement).toBe(templateCard);

    await waitFor(() => {
      const tooltips = screen.queryAllByRole("tooltip");
      expect(tooltips.length).toBeGreaterThan(0);
    });

    expect(
      screen.getAllByText("Long template description that should be available through the app tooltip on keyboard focus.").length,
    ).toBeGreaterThan(1);
  });

  it("shows the repository title in the back link while keeping routing keyed on name", async () => {
    renderComposeLanding();

    const backLink = await screen.findByRole("link", { name: "Back to Repository Title" });

    expect(backLink.getAttribute("href")).toBe("/workspaces/repo-dir");
  });

  it("falls back to the workspace name when no title is available", async () => {
    mockWorkspaces = [{ name: "repo-dir", title: "" }];

    renderComposeLanding();

    expect(await screen.findByRole("link", { name: "Back to repo-dir" })).toBeTruthy();
  });
});
