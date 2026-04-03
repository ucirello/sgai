import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { render, screen, cleanup } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import * as useAdhocRunModule from "@/hooks/useAdhocRun";
import * as factoryStateModule from "@/lib/factory-state";
import * as markdownEditorModule from "@/components/MarkdownEditor";
import { AdhocOutput } from "../AdhocOutput";

const mockWorkspace = {
  name: "test-workspace",
  dir: "/path/to/test-workspace",
  running: false,
  needsInput: false,
  inProgress: false,
  pinned: false,
  isRoot: false,
  isFork: false,
  title: "Adhoc Workspace Title",
  computedTitle: "",
  status: "",
  badgeClass: "",
  badgeText: "",
  hasSgai: true,
  hasEditedGoal: false,
  interactiveAuto: false,
  continuousMode: false,
  currentAgent: "",
  currentModel: "",
  task: "",
  goalContent: "",
  rawGoalContent: "",
  pmContent: "",
  hasProjectMgmt: false,
  svgHash: "",
  totalExecTime: "",
  latestProgress: "",
  humanMessage: "",
  agentSequence: [],
  cost: { totalCost: 0, totalTokens: { input: 0, output: 0, reasoning: 0, cacheRead: 0, cacheWrite: 0 }, byAgent: [] },
  modelStatuses: [],
  agentModels: [],
  events: [],
  messages: [],
  projectTodos: [],
  agentTodoSections: [],
  log: [],
  external: false,
};

const mockUseAdhocRun = () => ({
    selectedModel: "",
    setSelectedModel: mock(() => {}),
    prompt: "",
    setPrompt: mock(() => {}),
    output: "",
    isRunning: false,
    runError: null,
    startRun: mock(() => {}),
    stopRun: mock(() => {}),
    handleSubmit: mock((event: Event) => event.preventDefault()),
    outputRef: { current: null },
    promptHistory: [],
    selectFromHistory: mock(() => {}),
    clearHistory: mock(() => {}),
  });

const mockMarkdownEditor = ({ value, onChange, disabled, placeholder }: { value: string; onChange: (value: string | undefined) => void; disabled?: boolean; placeholder?: string }) => (
  <textarea
    data-testid="markdown-editor"
    value={value}
    onChange={(event) => onChange(event.target.value)}
    disabled={disabled}
    placeholder={placeholder}
  />
);

function renderAdhocOutput() {
  return render(
    <MemoryRouter initialEntries={["/workspaces/test-workspace/adhoc"]}>
      <TooltipProvider>
        <Routes>
          <Route path="/workspaces/:name/adhoc" element={<AdhocOutput />} />
        </Routes>
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("AdhocOutput", () => {
  beforeEach(() => {
    document.body.style.pointerEvents = "auto";

    spyOn(useAdhocRunModule, "useAdhocRun").mockImplementation((...args) => mockUseAdhocRun(...args));
    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: [mockWorkspace],
      fetchStatus: "idle",
      lastFetchedAt: Date.now(),
    }));
    spyOn(markdownEditorModule, "MarkdownEditor").mockImplementation((...args) => mockMarkdownEditor(...args));
  });

  afterEach(() => {
    mock.restore();
    cleanup();
  });

  it("shows the canonical repository title", () => {
    renderAdhocOutput();

    expect(screen.getByText("Back to Adhoc Workspace Title")).toBeTruthy();
    expect(screen.getByText("Adhoc Workspace Title")).toBeTruthy();
  });
});
