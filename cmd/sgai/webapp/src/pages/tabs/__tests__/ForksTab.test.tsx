import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import * as ReactRouter from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import * as factoryStateModule from "@/lib/factory-state";
import { api } from "@/lib/api";
import * as useAdhocRunModule from "@/hooks/useAdhocRun";
import { ForksTab } from "../ForksTab";

const mockNavigate = mock(() => {});
const mockDeleteWorkspace = mock(() => Promise.resolve({ deleted: true }));

const createRepositoryAction = (overrides: Record<string, unknown> = {}) => ({
  repositoryMode: "standalone",
  entryPoint: "confirm",
  allowedOperations: ["detach"],
  defaultOperation: "detach",
  disabledReason: "",
  attachedForkCount: 0,
  running: false,
  needsInput: false,
  inProgress: false,
  pinned: false,
  isRoot: true,
  isFork: false,
  title: "Workspace One Title",
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
  agentTodos: [],
  forks: [],
  log: [],
  actions: [],
  external: false,
  ...overrides,
});

const createRepositoryPresentation = (
  workspaceName: string,
  action: {
    repositoryMode: string;
    entryPoint: string;
    allowedOperations: string[];
    defaultOperation?: string;
  },
) => {
  const operationLabel = (operation: string) => operation === "delete" ? "Delete" : "Detach";
  const operationTone = (operation: string) => operation === "delete" ? "destructive" : "neutral";

  if (action.entryPoint === "choose") {
    const repositoryNoun = action.repositoryMode === "fork" ? "fork" : "workspace";
    const subject = action.repositoryMode === "fork" ? `fork ${workspaceName}` : workspaceName;

    return {
      detailTriggerLabel: "Choose action",
      treeTriggerLabel: `Choose action for ${subject}`,
      forkRowTriggerLabel: `Choose action for ${subject}`,
      dialogTitle: action.repositoryMode === "fork" ? "Choose fork action" : "Choose workspace action",
      dialogDescription: `Choose what to do with ${repositoryNoun} '${workspaceName}'. ${action.allowedOperations.map((operation) => operation === "delete"
        ? `Delete permanently removes the ${repositoryNoun} from disk.`
        : `Detach removes the ${repositoryNoun} from the SGAI workspace list and keeps the files on disk.`).join(" ")}`,
      icon: "choose",
      tone: "neutral",
      operations: action.allowedOperations.map((operation) => ({
        operation,
        label: operationLabel(operation),
        icon: operation,
        tone: operationTone(operation),
      })),
    };
  }

  const confirmOperation = action.defaultOperation ?? action.allowedOperations[0] ?? "detach";

  return {
    detailTriggerLabel: operationLabel(confirmOperation),
    treeTriggerLabel: `${operationLabel(confirmOperation)} ${workspaceName}`,
    forkRowTriggerLabel: `${operationLabel(confirmOperation)} ${workspaceName}`,
    dialogTitle: confirmOperation === "delete" ? "Delete workspace" : "Detach workspace",
    dialogDescription: confirmOperation === "delete"
      ? `This will permanently delete '${workspaceName}' from disk. This action cannot be undone.`
      : `This will remove '${workspaceName}' from the SGAI workspace list. The files on disk will NOT be deleted.`,
    icon: confirmOperation,
    tone: operationTone(confirmOperation),
    operations: action.allowedOperations.map((operation) => ({
      operation,
      label: operationLabel(operation),
      icon: operation,
      tone: operationTone(operation),
    })),
  };
};

const createMockWorkspace = (overrides: Record<string, unknown> = {}) => (
  (() => {
    const repositoryActionOverrides = "repositoryAction" in overrides
      ? overrides.repositoryAction as Record<string, unknown>
      : undefined;
    const hasPresentationOverride = Boolean(repositoryActionOverrides && "presentation" in repositoryActionOverrides);
    const workspace = {
      name: "workspace-1",
      dir: "/path/to/workspace-1",
      running: false,
      needsInput: false,
      inProgress: false,
      pinned: false,
      isRoot: true,
      isFork: false,
      title: "Workspace One Title",
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
      agentTodos: [],
      forks: [],
      log: [],
      actions: [],
      external: false,
      repositoryAction: createRepositoryAction({
        repositoryMode: "root",
        entryPoint: "hidden",
        allowedOperations: [],
        defaultOperation: "",
      }),
      ...overrides,
    };

    workspace.repositoryAction = hasPresentationOverride
      ? workspace.repositoryAction
      : {
        ...workspace.repositoryAction,
        presentation: createRepositoryPresentation(workspace.name, workspace.repositoryAction),
      };

    return workspace;
  })());

const factoryState = {
  workspaces: [createMockWorkspace()],
  fetchStatus: "idle",
};

const mockTriggerFactoryRefresh = mock(() => {});
const mockOpenEditor = mock(() => Promise.resolve({ opened: true }));

const mockUseAdhocRun = () => ({
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
  });

function forksTabTestView(workspaceName = "workspace-1") {
  return (
    <MemoryRouter>
      <TooltipProvider>
        <ForksTab workspaceName={workspaceName} />
      </TooltipProvider>
    </MemoryRouter>
  );
}

describe("ForksTab", () => {
  beforeEach(() => {
    cleanup();
    mockNavigate.mockClear();
    mockDeleteWorkspace.mockClear();
    mockTriggerFactoryRefresh.mockClear();
    mockOpenEditor.mockClear();
    factoryState.workspaces = [createMockWorkspace()];
    factoryState.fetchStatus = "idle";

    spyOn(ReactRouter, "useNavigate").mockImplementation(() => mockNavigate);
    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: factoryState.workspaces,
      fetchStatus: factoryState.fetchStatus,
      lastFetchedAt: Date.now(),
    }));
    spyOn(factoryStateModule, "triggerFactoryRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(api.workspaces, "deleteWorkspace").mockImplementation((...args) => mockDeleteWorkspace(...args));
    spyOn(api.workspaces, "openEditor").mockImplementation((...args) => mockOpenEditor(...args));
    spyOn(useAdhocRunModule, "useAdhocRun").mockImplementation((...args) => mockUseAdhocRun(...args));
  });

  afterEach(() => {
    mock.restore();
  });

  it("does not render commit preview controls for fork rows", () => {
    factoryState.workspaces = [
      createMockWorkspace({
        forks: [
          {
            name: "workspace-1-fork-1",
            dir: "/path/to/workspace-1-fork-1",
            running: false,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "Fork One Title",
          },
        ],
      }),
    ];

    render(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText("Fork One Title")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /expand commits/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /collapse commits/i })).toBeNull();
  });

  it("uses nested fork needs-input state when no standalone fork entry exists", () => {
    factoryState.workspaces = [
      createMockWorkspace({
        forks: [
          {
            name: "workspace-1-fork-1",
            dir: "/path/to/workspace-1-fork-1",
            running: false,
            needsInput: true,
            inProgress: false,
            pinned: false,
            title: "Fork 1",
          },
        ],
      }),
    ];

    render(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    const respondButton = screen.getByRole("button", { name: "Respond to fork Fork 1" });
    expect(respondButton.hasAttribute("disabled")).toBe(false);
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

    expect(mockNavigate).toHaveBeenCalledWith("/workspaces/workspace-1/progress?workspaceDir=%2Fpath%2Fto%2Fworkspace-1");
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

    expect(mockNavigate).toHaveBeenCalledWith("/workspaces/workspace-1/progress?workspaceDir=%2Fpath%2Fto%2Fworkspace-1");
  });

  it("stays healthy after deleting the last fork without a hard refresh", async () => {
    const user = userEvent.setup();
    factoryState.workspaces = [
      createMockWorkspace({
        repositoryAction: createRepositoryAction({
          repositoryMode: "root",
          entryPoint: "hidden",
          allowedOperations: [],
          defaultOperation: "",
          attachedForkCount: 1,
        }),
        forks: [
          {
            name: "workspace-1-fork-1",
            dir: "/path/to/workspace-1-fork-1",
            running: false,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "Fork 1",
          },
        ],
      }),
      createMockWorkspace({
        name: "workspace-1-fork-1",
        dir: "/path/to/workspace-1-fork-1",
        isRoot: false,
        isFork: true,
        title: "Fork 1",
        repositoryAction: createRepositoryAction({
          repositoryMode: "fork",
          entryPoint: "choose",
          allowedOperations: ["detach", "delete"],
          defaultOperation: "",
        }),
      }),
    ];

    mockDeleteWorkspace.mockImplementation(async (name, operation) => {
      expect(name).toBe("workspace-1-fork-1");
      expect(operation).toBe("delete");
      factoryState.workspaces = [
        createMockWorkspace({
          repositoryAction: createRepositoryAction({
            repositoryMode: "root",
            entryPoint: "confirm",
            allowedOperations: ["detach"],
            defaultOperation: "detach",
            attachedForkCount: 0,
          }),
          forks: [],
        }),
      ];
      return { deleted: true };
    });

    const view = render(forksTabTestView());

    await user.click(screen.getByRole("button", { name: "Choose action for fork workspace-1-fork-1" }));
    await user.click(await screen.findByRole("button", { name: /^Delete$/ }));

    await waitFor(() => {
      expect(mockDeleteWorkspace).toHaveBeenCalledWith("workspace-1-fork-1", "delete", "/path/to/workspace-1-fork-1");
    });

    view.rerender(forksTabTestView());

    expect(screen.getByText("No forks yet. Create a fork to start work.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Create Fork" })).toBeTruthy();
  });

  it("offers detach and delete choices for fork rows", async () => {
    const user = userEvent.setup();
    factoryState.workspaces = [
      createMockWorkspace({
        forks: [
          {
            name: "workspace-1-fork-1",
            dir: "/path/to/workspace-1-fork-1",
            running: false,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "Fork 1",
          },
        ],
      }),
      createMockWorkspace({
        name: "workspace-1-fork-1",
        dir: "/path/to/workspace-1-fork-1",
        isRoot: false,
        isFork: true,
        title: "Fork 1",
        repositoryAction: createRepositoryAction({
          repositoryMode: "fork",
          entryPoint: "choose",
          allowedOperations: ["detach", "delete"],
          defaultOperation: "",
        }),
      }),
    ];

    render(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    await user.click(screen.getByRole("button", { name: "Choose action for fork workspace-1-fork-1" }));

    expect(screen.getByText("Choose fork action")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Detach$/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Delete$/ })).toBeTruthy();
  });

  it("hides running fork row actions when backend policy marks them hidden", async () => {
    factoryState.workspaces = [
      createMockWorkspace({
        forks: [
          {
            name: "workspace-1-fork-1",
            dir: "/path/to/workspace-1-fork-1",
            running: true,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "Fork 1",
          },
        ],
      }),
      createMockWorkspace({
        name: "workspace-1-fork-1",
        dir: "/path/to/workspace-1-fork-1",
        running: true,
        isRoot: false,
        isFork: true,
        title: "Fork 1",
        repositoryAction: createRepositoryAction({
          repositoryMode: "fork",
          entryPoint: "hidden",
          allowedOperations: [],
          defaultOperation: "",
          disabledReason: "running",
          running: true,
        }),
      }),
    ];

    render(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText("Fork 1")).toBeTruthy();
    });

    expect(screen.queryByRole("button", { name: "Choose action for fork workspace-1-fork-1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Detach workspace-1-fork-1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete workspace-1-fork-1" })).toBeNull();
  });

  it("detaches fork rows through the workspace action endpoint", async () => {
    const user = userEvent.setup();
    factoryState.workspaces = [
      createMockWorkspace({
        forks: [
          {
            name: "workspace-1-fork-1",
            dir: "/path/to/workspace-1-fork-1",
            running: false,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "Fork 1",
          },
        ],
      }),
      createMockWorkspace({
        name: "workspace-1-fork-1",
        dir: "/path/to/workspace-1-fork-1",
        isRoot: false,
        isFork: true,
        title: "Fork 1",
        repositoryAction: createRepositoryAction({
          repositoryMode: "fork",
          entryPoint: "choose",
          allowedOperations: ["detach", "delete"],
          defaultOperation: "",
        }),
      }),
    ];

    render(
      <MemoryRouter>
        <TooltipProvider>
          <ForksTab workspaceName="workspace-1" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    await user.click(screen.getByRole("button", { name: "Choose action for fork workspace-1-fork-1" }));
    await user.click(screen.getByRole("button", { name: /^Detach$/ }));

    expect(mockDeleteWorkspace).toHaveBeenCalledWith("workspace-1-fork-1", "detach", "/path/to/workspace-1-fork-1");
  });
});
