import { describe, it, expect, beforeEach, mock } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ForksTab } from "../ForksTab";

const mockNavigate = mock(() => {});

const createMockWorkspace = (overrides: Record<string, unknown> = {}) => ({
  name: "workspace-1",
  dir: "/path/to/workspace-1",
  running: false,
  needsInput: false,
  inProgress: false,
  pinned: false,
  isRoot: true,
  isFork: false,
  description: "Workspace 1",
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
  agentTodos: [],
  changes: { description: "", diffLines: [] },
  commits: [],
  forks: [],
  log: [],
  actions: [],
  external: false,
  ...overrides,
});

const factoryState = {
  workspaces: [createMockWorkspace()],
  fetchStatus: "idle",
};

mock.module("react-router", () => ({
  ...require("react-router"),
  useNavigate: () => mockNavigate,
}));

mock.module("@/lib/factory-state", () => ({
  useFactoryState: () => ({
    workspaces: factoryState.workspaces,
    fetchStatus: factoryState.fetchStatus,
    lastFetchedAt: Date.now(),
  }),
  triggerFactoryRefresh: mock(() => {}),
}));

mock.module("@/hooks/useAdhocRun", () => ({
  useAdhocRun: () => ({
    models: { models: [{ id: "model-1", name: "Model 1" }] },
    modelsLoading: false,
    modelsError: null,
    selectedModel: "",
    setSelectedModel: mock(() => {}),
    prompt: "",
    setPrompt: mock(() => {}),
    output: "",
    isRunning: false,
    runError: null,
    handleSubmit: mock((event: Event) => event.preventDefault()),
    handleKeyDown: mock(() => {}),
    stopRun: mock(() => {}),
    outputRef: { current: null },
    promptHistory: [],
    selectFromHistory: mock(() => {}),
    clearHistory: mock(() => {}),
  }),
}));

describe("ForksTab", () => {
  beforeEach(() => {
    cleanup();
    mockNavigate.mockClear();
    factoryState.workspaces = [createMockWorkspace()];
    factoryState.fetchStatus = "idle";
  });

  it("offers a create fork action from the empty state", async () => {
    const user = userEvent.setup();

    render(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText("No forks yet. Create a fork to start work.")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Create Fork" }));

    expect(mockNavigate).toHaveBeenCalledWith("/workspaces/workspace-1/progress");
  });

  it("stays stable when loading transitions to a resolved workspace", async () => {
    const user = userEvent.setup();
    factoryState.workspaces = [];
    factoryState.fetchStatus = "fetching";

    const view = render(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: "Create Fork" })).toBeNull();

    factoryState.workspaces = [createMockWorkspace()];
    factoryState.fetchStatus = "idle";

    view.rerender(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText("No forks yet. Create a fork to start work.")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Create Fork" }));

    expect(mockNavigate).toHaveBeenCalledWith("/workspaces/workspace-1/progress");
  });
});
