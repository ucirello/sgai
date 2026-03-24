import { afterEach, beforeEach, describe, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ApiComposeDraftRequest } from "@/types";

const mockComposeGet = mock(() => Promise.resolve({
  workspace: "repo-dir",
  state: {
    title: "Generated Repo Title",
    description: "Body description",
    completionGate: "make test",
    agents: [{ name: "coordinator", selected: true, model: "openai/gpt-5.4" }],
    flow: "",
    tasks: "",
  },
  wizard: {
    currentStep: 1,
    title: "Generated Repo Title",
    description: "Body description",
    techStack: [],
    safetyAnalysis: false,
    completionGate: "make test",
  },
  techStackItems: [],
}));
const mockComposePreview = mock(() => Promise.resolve({ content: "preview", etag: "etag-1" }));
const mockComposeSaveDraft = mock((_workspace: string, _draft: ApiComposeDraftRequest) => Promise.resolve({ saved: true }));

mock.module("@/lib/api", () => ({
  api: {
    compose: {
      get: mockComposeGet,
      preview: mockComposePreview,
      saveDraft: mockComposeSaveDraft,
    },
  },
  ApiError: class ApiError extends Error {
    constructor(public status: number, message: string) {
      super(message);
      this.name = "ApiError";
    }
  },
}));

import { MemoryRouter } from "react-router";
import { useComposeWizard } from "../useComposeWizard";
import { computeAgentsAndFlowFromTechStack } from "../composeWizardTechStack";

function ComposeWizardHarness() {
  const { isLoading, wizardData, saveDraft } = useComposeWizard({
    workspace: "repo-dir",
    currentStep: 1,
  });

  if (isLoading) {
    return <div>Loading...</div>;
  }

  return (
    <div>
      <div data-testid="wizard-title">{wizardData.title}</div>
      <div data-testid="wizard-description">{wizardData.description}</div>
      <button type="button" onClick={() => { void saveDraft(); }}>
        Save Draft
      </button>
    </div>
  );
}

describe("useComposeWizard tech stack mapping", () => {
  it("maps Go tech stack to the renamed reviewer pair", () => {
    const { agents, flow } = computeAgentsAndFlowFromTechStack(["go"], false);

    expect(agents.map((agent) => agent.name)).toEqual([
      "coordinator",
      "go-developer",
      "go-reviewer",
    ]);
    expect(flow).toContain('"go-developer" -> "go-reviewer"');
    expect(flow).not.toContain("backend-go-developer");
    expect(flow).not.toContain("go-readability-reviewer");
  });

  it("routes Go safety-analysis flow from the renamed developer", () => {
    const { agents, flow } = computeAgentsAndFlowFromTechStack(["go"], true);

    expect(agents.map((agent) => agent.name)).toEqual([
      "coordinator",
      "go-developer",
      "go-reviewer",
      "stpa-analyst",
    ]);
    expect(flow).toContain('"go-developer" -> "go-reviewer"');
    expect(flow).toContain('"go-developer" -> "stpa-analyst"');
    expect(flow).toContain('"go-reviewer" -> "stpa-analyst"');
    expect(agents.map((agent) => agent.name)).not.toContain("backend-go-developer");
    expect(agents.map((agent) => agent.name)).not.toContain("go-readability-reviewer");
  });
});

describe("useComposeWizard repository title handling", () => {
  beforeEach(() => {
    cleanup();
    sessionStorage.clear();
    mockComposeGet.mockClear();
    mockComposePreview.mockClear();
    mockComposeSaveDraft.mockClear();
  });

  afterEach(() => {
    cleanup();
  });

  it("preserves the repository title separately from the description when saving drafts", async () => {
    render(
      <MemoryRouter>
        <ComposeWizardHarness />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("wizard-description").textContent).toBe("Body description");
    });

    fireEvent.click(screen.getByRole("button", { name: "Save Draft" }));

    await waitFor(() => {
      expect(mockComposeSaveDraft).toHaveBeenCalled();
    });

    const [, draft] = mockComposeSaveDraft.mock.calls[0];
    expect(draft.state.title).toBe("Generated Repo Title");
    expect(draft.wizard.title).toBe("Generated Repo Title");
    expect(draft.state.description).toBe("Body description");
  });

  it("falls back to the workspace name when compose state titles are blank", async () => {
    mockComposeGet.mockImplementationOnce(() => Promise.resolve({
      workspace: "repo-dir",
      state: {
        title: "   ",
        description: "Body description",
        completionGate: "make test",
        agents: [{ name: "coordinator", selected: true, model: "openai/gpt-5.4" }],
        flow: "",
        tasks: "",
      },
      wizard: {
        currentStep: 1,
        title: "\t",
        description: "Body description",
        techStack: [],
        safetyAnalysis: false,
        completionGate: "make test",
      },
      techStackItems: [],
    }));

    render(
      <MemoryRouter>
        <ComposeWizardHarness />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("wizard-title").textContent).toBe("repo-dir");
    });

    fireEvent.click(screen.getByRole("button", { name: "Save Draft" }));

    await waitFor(() => {
      expect(mockComposeSaveDraft).toHaveBeenCalled();
    });

    const [, draft] = mockComposeSaveDraft.mock.calls[0];
    expect(draft.state.title).toBe("repo-dir");
    expect(draft.wizard.title).toBe("repo-dir");
  });
});
