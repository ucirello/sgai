import { afterEach, beforeEach, describe, expect, it, mock } from "bun:test";
import { cleanup, render, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

const mockNavigate = mock(() => {});
const mockComposeGet = mock(() => Promise.resolve({
  workspace: "repo-dir",
  state: {
    title: "Repository Title",
    description: "Existing description",
    completionGate: "",
    agents: [],
    flow: "",
    tasks: "",
  },
  wizard: {
    currentStep: 1,
    title: "Repository Title",
    description: "Existing description",
    techStack: [],
    safetyAnalysis: false,
    completionGate: "",
  },
  techStackItems: [],
}));
const mockComposeTemplates = mock(() => Promise.resolve({
  templates: [{
    id: "starter",
    name: "Starter",
    description: "Starter template",
    icon: "✨",
    agents: [],
    flow: "",
  }],
}));
const mockComposeSaveDraft = mock(() => Promise.resolve({ saved: true }));

mock.module("react-router", () => ({
  ...require("react-router"),
  useNavigate: () => mockNavigate,
}));

mock.module("@/lib/api", () => ({
  api: {
    compose: {
      get: mockComposeGet,
      templates: mockComposeTemplates,
      saveDraft: mockComposeSaveDraft,
    },
  },
}));

import { ComposeTemplateRedirect } from "../ComposeTemplate";

function renderComposeTemplate() {
  return render(
    <MemoryRouter initialEntries={["/compose/template/starter?workspace=repo-dir"]}>
      <Routes>
        <Route path="/compose/template/:id" element={<ComposeTemplateRedirect />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ComposeTemplateRedirect repository titles", () => {
  beforeEach(() => {
    mockNavigate.mockClear();
    mockComposeGet.mockClear();
    mockComposeTemplates.mockClear();
    mockComposeSaveDraft.mockClear();
  });

  afterEach(() => {
    cleanup();
  });

  it("preserves the existing repository title instead of replacing it with the workspace name", async () => {
    renderComposeTemplate();

    await waitFor(() => {
      expect(mockComposeSaveDraft).toHaveBeenCalled();
    });

    const [, draft] = mockComposeSaveDraft.mock.calls[0] as [string, { state: { title: string }; wizard: { title?: string } }];

    expect(draft.state.title).toBe("Repository Title");
    expect(draft.wizard.title).toBe("Repository Title");
    expect(draft.state.title).not.toBe("repo-dir");
    expect(draft.wizard.title).not.toBe("repo-dir");
  });

  it("falls back to the workspace name when existing compose titles are blank", async () => {
    mockComposeGet.mockImplementationOnce(() => Promise.resolve({
      workspace: "repo-dir",
      state: {
        title: "   ",
        description: "Existing description",
        completionGate: "",
        agents: [],
        flow: "",
        tasks: "",
      },
      wizard: {
        currentStep: 1,
        title: "\n\t",
        description: "Existing description",
        techStack: [],
        safetyAnalysis: false,
        completionGate: "",
      },
      techStackItems: [],
    }));

    renderComposeTemplate();

    await waitFor(() => {
      expect(mockComposeSaveDraft).toHaveBeenCalled();
    });

    const [, draft] = mockComposeSaveDraft.mock.calls[0] as [string, { state: { title: string }; wizard: { title?: string } }];
    expect(draft.state.title).toBe("repo-dir");
    expect(draft.wizard.title).toBe("repo-dir");
  });
});
