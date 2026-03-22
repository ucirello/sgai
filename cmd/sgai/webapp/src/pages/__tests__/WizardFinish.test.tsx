import { beforeEach, describe, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/tooltip";

const mockFetchPreview = mock(() => Promise.resolve());
const mockSaveGoal = mock(() => Promise.resolve(true));
const mockGoToStep = mock(() => {});
const mockNavigate = mock(() => {});

mock.module("react-router", () => ({
  ...require("react-router"),
  useNavigate: () => mockNavigate,
}));

mock.module("@/hooks/useComposeWizard", () => ({
  useComposeWizard: () => ({
    wizardData: {
      title: "Generated Repo Title",
      description: "Body description",
      techStack: ["react"],
      safetyAnalysis: false,
      completionGate: "make test",
    },
    techStackItems: [{ id: "react", name: "React", selected: true }],
    preview: { content: "preview", etag: "etag-1" },
    isLoading: false,
    isSaving: false,
    saveError: null,
    draftSavedAt: null,
    isSavingDraft: false,
    fetchPreview: mockFetchPreview,
    saveGoal: mockSaveGoal,
    goToStep: mockGoToStep,
  }),
}));

mock.module("@/components/ComposePreview", () => ({
  ComposePreview: ({ title }: { title: string }) => <div>{title}</div>,
}));

import { MemoryRouter } from "react-router";
import { WizardFinish } from "../WizardFinish";

function renderWizardFinish() {
  return render(
    <MemoryRouter initialEntries={["/compose/finish?workspace=repo-dir"]}>
      <TooltipProvider>
        <WizardFinish />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("WizardFinish", () => {
  beforeEach(() => {
    cleanup();
    mockFetchPreview.mockClear();
    mockSaveGoal.mockClear();
    mockGoToStep.mockClear();
    mockNavigate.mockClear();
  });

  it("shows the repository title separately from the project description", async () => {
    renderWizardFinish();

    await waitFor(() => {
      expect(screen.getByText("Repository Title")).toBeTruthy();
    });

    expect(screen.getByText("Generated Repo Title")).toBeTruthy();
    expect(screen.getByText("Project Description")).toBeTruthy();
    expect(screen.getByText("Body description")).toBeTruthy();
  });

  it("exposes summary values through keyboard-focusable tooltips", async () => {
    const user = userEvent.setup();

    renderWizardFinish();

    await waitFor(() => {
      expect(screen.getByText("Generated Repo Title")).toBeTruthy();
    });

    await user.tab();

    const repositoryTitle = screen.getByText("Generated Repo Title");
    await user.tab();
    expect(document.activeElement).toBe(repositoryTitle);

    await waitFor(() => {
      const tooltips = screen.queryAllByRole("tooltip");
      expect(tooltips.length).toBeGreaterThan(0);
    });

    const description = screen.getByText("Body description");
    await user.tab();
    expect(document.activeElement).toBe(description);

    const technologyBadge = screen.getByText("React");
    await user.tab();
    expect(document.activeElement).toBe(technologyBadge);

    const completionGate = screen.getByText("make test");
    await user.tab();
    expect(document.activeElement).toBe(completionGate);
  });

  it("clears the pending redirect timeout when the page unmounts after save", async () => {
    const view = renderWizardFinish();

    fireEvent.click(screen.getByRole("button", { name: "Save GOAL.md" }));

    await waitFor(() => {
      expect(mockSaveGoal).toHaveBeenCalled();
    });

    view.unmount();

    await new Promise((resolve) => {
      setTimeout(resolve, 1600);
    });

    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
