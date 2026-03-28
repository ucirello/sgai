import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { act, render, screen, waitFor, cleanup, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route, createMemoryRouter, RouterProvider } from "react-router";
import * as ReactRouter from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { SidebarProvider } from "@/components/ui/sidebar";
import * as factoryStateModule from "@/lib/factory-state";
import { api } from "@/lib/api";
import * as useAdhocRunModule from "@/hooks/useAdhocRun";
import * as mobileModule from "@/hooks/use-mobile";
import * as markdownEditorModule from "@/components/MarkdownEditor";
import { WorkspaceDetail } from "../WorkspaceDetail";

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// Override pointer-events on body to allow interactions in tests
beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

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
  isRoot: false,
  isFork: false,
  title: "Test Workspace Title",
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
  goalContent: "# Test Goal",
  rawGoalContent: "# Test Goal",
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
  log: [],
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

const createMockWorkspace = (overrides = {}) => (
  (() => {
    const repositoryActionOverrides = typeof overrides === "object" && overrides !== null && "repositoryAction" in overrides
      ? overrides.repositoryAction as Record<string, unknown>
      : undefined;
    const hasPresentationOverride = Boolean(repositoryActionOverrides && "presentation" in repositoryActionOverrides);
    const workspace = {
      name: "test-workspace",
      dir: "/path/to/test-workspace",
      running: false,
      needsInput: false,
      inProgress: false,
      pinned: false,
      isRoot: false,
      isFork: false,
      title: "Test Workspace Title",
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
      goalContent: "# Test Goal",
      rawGoalContent: "# Test Goal",
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
      log: [],
      external: false,
      repositoryAction: createRepositoryAction(),
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

let mockWorkspaces = [createMockWorkspace()];
let mockFetchStatus: "idle" | "fetching" | "error" = "idle";

const mockStart = mock(() => Promise.resolve({ running: true }));
const mockStop = mock(() => Promise.resolve({ running: false }));
const mockTogglePin = mock(() => Promise.resolve({ pinned: true }));
const mockOpenEditor = mock(() => Promise.resolve({ opened: true }));
const mockDeleteWorkspace = mock(() => Promise.resolve({ deleted: true }));
const mockForkTemplate = mock(() => Promise.resolve({ content: "# New task\n\nShip it" }));
const mockFork = mock(() => Promise.resolve({ name: "test-workspace-fork" }));
const mockTriggerFactoryRefresh = mock(() => {});
const mockRespond = mock(() => Promise.resolve({ success: true }));
const mockReset = mock(() => Promise.resolve());
const mockNavigate = mock(() => {});
const mockStartActionRun = mock(() => {});
const mockStopActionRun = mock(() => {});
const mockActionRunState = {
  output: "",
  isRunning: false,
  runError: null as string | null,
};

const mockUseAdhocRun = () => ({
    output: mockActionRunState.output,
    isRunning: mockActionRunState.isRunning,
    runError: mockActionRunState.runError,
    startRun: mock(() => {}),
    startActionRun: mockStartActionRun,
    stopRun: mockStopActionRun,
    outputRef: { current: null },
  });

const mockMarkdownEditor = ({ value, onChange, disabled, placeholder }: {
    value: string;
    onChange: (v: string | undefined) => void;
    disabled: boolean;
    placeholder?: string;
  }) => (
    <div data-testid="markdown-editor">
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        data-testid="fork-editor-textarea"
        placeholder={placeholder}
      />
    </div>
  );

function workspaceDetailTestView(workspaceName = "test-workspace", tab = "progress") {
  return (
    <MemoryRouter initialEntries={[`/workspaces/${workspaceName}/${tab}`]}>
      <TooltipProvider>
        <SidebarProvider>
          <Routes>
            <Route path="/workspaces/:name/*" element={<WorkspaceDetail />} />
          </Routes>
        </SidebarProvider>
      </TooltipProvider>
    </MemoryRouter>
  );
}

function renderWorkspaceDetail(workspaceName = "test-workspace", tab = "progress") {
  return render(workspaceDetailTestView(workspaceName, tab));
}

function renderWorkspaceDetailRouter(initialPath = "/workspaces/test-workspace/progress") {
  const router = createMemoryRouter([
    {
      path: "/workspaces/:name/*",
      element: (
        <TooltipProvider>
          <SidebarProvider>
            <WorkspaceDetail />
          </SidebarProvider>
        </TooltipProvider>
      ),
    },
  ], {
    initialEntries: [initialPath],
  });

  return {
    router,
    ...render(<RouterProvider router={router} />),
  };
}

afterEach(() => {
  mock.restore();
  cleanup();
});

describe("WorkspaceDetail", () => {
  beforeEach(() => {
    mockWorkspaces = [createMockWorkspace()];
    mockFetchStatus = "idle";
    mockStart.mockClear();
    mockStop.mockClear();
    mockTogglePin.mockClear();
    mockOpenEditor.mockClear();
    mockDeleteWorkspace.mockClear();
    mockForkTemplate.mockClear();
    mockFork.mockClear();
    mockTriggerFactoryRefresh.mockClear();
    mockRespond.mockClear();
    mockReset.mockClear();
    mockNavigate.mockClear();
    mockStartActionRun.mockClear();
    mockStopActionRun.mockClear();
    mockActionRunState.output = "";
    mockActionRunState.isRunning = false;
    mockActionRunState.runError = null;

    spyOn(ReactRouter, "useNavigate").mockImplementation(() => mockNavigate);
    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: mockWorkspaces,
      fetchStatus: mockFetchStatus,
      lastFetchedAt: Date.now(),
    }));
    spyOn(factoryStateModule, "triggerFactoryRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(api.workspaces, "start").mockImplementation((...args) => mockStart(...args));
    spyOn(api.workspaces, "stop").mockImplementation((...args) => mockStop(...args));
    spyOn(api.workspaces, "togglePin").mockImplementation((...args) => mockTogglePin(...args));
    spyOn(api.workspaces, "openEditor").mockImplementation((...args) => mockOpenEditor(...args));
    spyOn(api.workspaces, "deleteWorkspace").mockImplementation((...args) => mockDeleteWorkspace(...args));
    spyOn(api.workspaces, "forkTemplate").mockImplementation((...args) => mockForkTemplate(...args));
    spyOn(api.workspaces, "fork").mockImplementation((...args) => mockFork(...args));
    spyOn(api.workspaces, "respond").mockImplementation((...args) => mockRespond(...args));
    spyOn(api.workspaces, "reset").mockImplementation((...args) => mockReset(...args));
    spyOn(useAdhocRunModule, "useAdhocRun").mockImplementation((...args) => mockUseAdhocRun(...args));
    spyOn(mobileModule, "useIsMobile").mockImplementation(() => false);
    spyOn(markdownEditorModule, "MarkdownEditor").mockImplementation((...args) => mockMarkdownEditor(...args));
  });

  describe("repository action buttons", () => {
    it("shows workspace action configuration errors on the progress tab", async () => {
      mockWorkspaces = [createMockWorkspace({
        actionConfigError: "invalid JSON syntax",
        actions: [],
      })];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/progress");

      await waitFor(() => {
        expect(screen.getByText(/action configuration error/i)).toBeTruthy();
        expect(screen.getByText(/invalid JSON syntax/i)).toBeTruthy();
      });
    });

    it("shows an actionable error for ambiguous duplicate-basename detail routes without workspaceDir", async () => {
      mockWorkspaces = [
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/first/shared-ws",
          title: "First Shared Workspace",
          task: "First task",
        }),
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/second/shared-ws",
          title: "Second Shared Workspace",
          task: "Second task",
        }),
      ];

      const { router } = renderWorkspaceDetailRouter("/workspaces/shared-ws");

      await waitFor(() => {
        expect(router.state.location.pathname).toBe("/workspaces/shared-ws");
        expect(screen.getByText("Workspace route is ambiguous.")).toBeTruthy();
      });
    });

    it("shows an ambiguous error for duplicate-basename forks routes", async () => {
      mockWorkspaces = [
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/first/shared-ws",
          isRoot: true,
          title: "Shared Workspace",
          forks: [{
            name: "shared-ws-first-fork",
            dir: "/tmp/first/shared-ws-first-fork",
            running: false,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "First Fork",
          }],
        }),
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/second/shared-ws",
          isRoot: true,
          title: "Shared Workspace",
          forks: [{
            name: "shared-ws-second-fork",
            dir: "/tmp/second/shared-ws-second-fork",
            running: false,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "Second Fork",
          }],
        }),
        createMockWorkspace({
          name: "shared-ws-first-fork",
          dir: "/tmp/first/shared-ws-first-fork",
          isFork: true,
          title: "First Fork",
          repositoryAction: createRepositoryAction({
            repositoryMode: "fork",
            entryPoint: "choose",
            allowedOperations: ["detach", "delete"],
            defaultOperation: "",
          }),
        }),
        createMockWorkspace({
          name: "shared-ws-second-fork",
          dir: "/tmp/second/shared-ws-second-fork",
          isFork: true,
          title: "Second Fork",
          repositoryAction: createRepositoryAction({
            repositoryMode: "fork",
            entryPoint: "choose",
            allowedOperations: ["detach", "delete"],
            defaultOperation: "",
          }),
        }),
      ];

      renderWorkspaceDetailRouter("/workspaces/shared-ws/forks");

      await waitFor(() => {
        expect(screen.getByText("Workspace route is ambiguous.")).toBeTruthy();
        expect(screen.queryByText("Second Fork")).toBeNull();
        expect(screen.queryByText("First Fork")).toBeNull();
      });
    });

    it("routes fork row actions to the selected fork workspace", async () => {
      const user = userEvent.setup();

      mockWorkspaces = [createMockWorkspace({
        isRoot: true,
        forks: [{
          name: "test-workspace-fork",
          dir: "/path/to/test-workspace-fork",
          running: false,
          needsInput: false,
          inProgress: false,
          pinned: false,
          title: "Fork 1",
        }],
        actions: [{
          name: "Run Tests",
          kind: "prompt",
          model: "model-1",
          prompt: "run tests",
          variables: [],
          description: "Run test suite",
        }],
      })];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/forks");

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Run Tests for fork Fork 1" })).toBeTruthy();
      });

      await user.click(screen.getByRole("button", { name: "Run Tests for fork Fork 1" }));

      expect(mockStartActionRun).toHaveBeenCalledWith("Run Tests", {}, "test-workspace-fork");
    });

    it("gives repeated fork-row icon buttons unique accessible names", async () => {
        const firstFork = {
          name: "test-workspace-fork",
          dir: "/path/to/test-workspace-fork",
          running: false,
          needsInput: false,
          inProgress: false,
          pinned: false,
          title: "Fork 1",
        };
        const secondFork = {
          name: "second-workspace-fork",
          dir: "/path/to/second-workspace-fork",
          running: false,
          needsInput: false,
          inProgress: false,
          pinned: false,
          title: "Fork 2",
        };

      mockWorkspaces = [createMockWorkspace({
        isRoot: true,
        forks: [firstFork, secondFork],
      }),
      createMockWorkspace({
        name: firstFork.name,
        dir: firstFork.dir,
        isFork: true,
        title: firstFork.title,
        repositoryAction: createRepositoryAction({
          repositoryMode: "fork",
          entryPoint: "choose",
          allowedOperations: ["detach", "delete"],
          defaultOperation: "",
        }),
      }),
      createMockWorkspace({
        name: secondFork.name,
        dir: secondFork.dir,
        isFork: true,
        title: secondFork.title,
        repositoryAction: createRepositoryAction({
          repositoryMode: "fork",
          entryPoint: "choose",
          allowedOperations: ["detach", "delete"],
          defaultOperation: "",
        }),
      })];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/forks");

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Respond to fork Fork 1" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Open fork Fork 1 in Editor" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Open fork Fork 1 in sgai" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Choose action for fork test-workspace-fork" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Respond to fork Fork 2" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Open fork Fork 2 in Editor" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Open fork Fork 2 in sgai" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Choose action for fork second-workspace-fork" })).toBeTruthy();
      });
    });

    it("targets fork-row icon actions by unique accessible name", async () => {
      const user = userEvent.setup();
      const needsInputFork = {
        name: "needs-input-fork",
        dir: "/path/to/needs-input-fork",
        running: false,
        needsInput: false,
        inProgress: false,
        pinned: false,
        title: "Needs Input Fork",
      };
      const plainFork = {
        name: "plain-fork",
        dir: "/path/to/plain-fork",
        running: false,
        needsInput: false,
        inProgress: false,
        pinned: false,
        title: "Plain Fork",
      };

      mockWorkspaces = [
        createMockWorkspace({
          isRoot: true,
          forks: [needsInputFork, plainFork],
        }),
        createMockWorkspace({
          name: "needs-input-fork",
          dir: "/path/to/needs-input-fork",
          needsInput: true,
          isFork: true,
          title: "Needs Input Fork",
          repositoryAction: createRepositoryAction({
            repositoryMode: "fork",
            entryPoint: "choose",
            allowedOperations: ["detach", "delete"],
            defaultOperation: "",
          }),
        }),
        createMockWorkspace({
          name: "plain-fork",
          dir: "/path/to/plain-fork",
          isFork: true,
          title: "Plain Fork",
          repositoryAction: createRepositoryAction({
            repositoryMode: "fork",
            entryPoint: "choose",
            allowedOperations: ["detach", "delete"],
            defaultOperation: "",
          }),
        }),
      ];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/forks");

      await user.click(await screen.findByRole("button", { name: "Respond to fork Needs Input Fork" }));
      expect(mockNavigate).toHaveBeenCalledWith("/workspaces/needs-input-fork/respond");

      await user.click(screen.getByRole("button", { name: "Open fork Plain Fork in Editor" }));

      await waitFor(() => {
        expect(mockOpenEditor).toHaveBeenCalledWith("plain-fork");
      });

      await user.click(screen.getByRole("button", { name: "Open fork Plain Fork in sgai" }));
      expect(mockNavigate).toHaveBeenCalledWith("/workspaces/plain-fork/progress");

      await user.click(screen.getByRole("button", { name: "Choose action for fork plain-fork" }));

      await waitFor(() => {
        expect(screen.getByText(/Choose fork action/)).toBeTruthy();
        expect(screen.getByText(/Choose what to do with fork 'plain-fork'/i)).toBeTruthy();
        expect(screen.getByText(/Delete permanently removes the fork from disk/i)).toBeTruthy();
      });
    });

    it("shows workspace action configuration errors on the forks tab", async () => {
      mockWorkspaces = [createMockWorkspace({
        isRoot: true,
        actionConfigError: "invalid JSON syntax",
        actions: [],
        forks: [{
          name: "test-workspace-fork",
          dir: "/path/to/test-workspace-fork",
          running: false,
          needsInput: false,
          inProgress: false,
          pinned: false,
          title: "Fork 1",
        }],
      })];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/forks");

      await waitFor(() => {
        expect(screen.getByText(/action configuration error/i)).toBeTruthy();
        expect(screen.getByText(/invalid JSON syntax/i)).toBeTruthy();
      });
    });

    it("disables both root and row action bars while a named action run is active", async () => {
      mockActionRunState.isRunning = true;
      mockWorkspaces = [createMockWorkspace({
        isRoot: true,
        actions: [{
          name: "Run Tests",
          kind: "prompt",
          model: "model-1",
          prompt: "run tests",
          variables: [],
          description: "Run test suite",
        }],
        forks: [{
          name: "test-workspace-fork",
          dir: "/path/to/test-workspace-fork",
          running: false,
          needsInput: false,
          inProgress: false,
          pinned: false,
          title: "Fork 1",
        }],
      })];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/forks");

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Run Tests for workspace Test Workspace Title" }).hasAttribute("disabled")).toBe(true);
        expect(screen.getByRole("button", { name: "Run Tests for fork Fork 1" }).hasAttribute("disabled")).toBe(true);
      });
    });
  });

  describe("start/stop buttons work", () => {
    it("shows a dedicated Fork tab for standalone workspaces", async () => {
      mockWorkspaces = [createMockWorkspace()];
      renderWorkspaceDetail();

      await waitFor(() => {
        expect(screen.getByRole("link", { name: "Fork" })).toBeTruthy();
      });

      expect(screen.queryByRole("button", { name: "Create Fork" })).toBeNull();
      expect(mockForkTemplate).not.toHaveBeenCalled();
    });

    it("renders the fork editor only on the Fork tab", async () => {
      mockWorkspaces = [createMockWorkspace()];

      renderWorkspaceDetail("test-workspace", "fork");

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Create Fork" })).toBeTruthy();
      });

      expect(mockForkTemplate).toHaveBeenCalledWith("test-workspace");
    });

    it("never shows the previous workspace draft while switching standalone workspaces", async () => {
      const nextWorkspaceTemplate = deferredValue<{ content: string }>();
      mockWorkspaces = [
        createMockWorkspace({ name: "test-workspace" }),
        createMockWorkspace({ name: "next-workspace", title: "Next Workspace", dir: "/path/to/next-workspace" }),
      ];

      mockForkTemplate.mockImplementation((workspaceName: string) => {
        if (workspaceName === "test-workspace") {
          return Promise.resolve({ content: "# First Goal\n\nFirst workspace task" });
        }
        if (workspaceName === "next-workspace") {
          return nextWorkspaceTemplate.promise;
        }
        return Promise.resolve({ content: "" });
      });

      const { router } = renderWorkspaceDetailRouter("/workspaces/test-workspace/fork");

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toContain("First workspace task");
      });

      await act(async () => {
        await router.navigate("/workspaces/next-workspace/fork");
      });

      await waitFor(() => {
        expect(mockForkTemplate).toHaveBeenCalledWith("next-workspace");
      });

      expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("");
      expect(screen.queryByDisplayValue(/First workspace task/)).toBeNull();
      expect(screen.getByRole("button", { name: "Create Fork" }).hasAttribute("disabled")).toBe(true);

      nextWorkspaceTemplate.resolve({ content: "# Second Goal\n\nSecond workspace task" });

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toContain("Second workspace task");
      });
    });

    it("does not show a Fork tab for fork workspaces", async () => {
      mockWorkspaces = [createMockWorkspace({ isFork: true })];
      renderWorkspaceDetail();

      await waitFor(() => {
        expect(screen.queryByRole("link", { name: "Fork" })).toBeNull();
      });

      expect(mockForkTemplate).not.toHaveBeenCalled();
    });

    it("lets forked roots open the fork editor", async () => {
      mockWorkspaces = [createMockWorkspace({ isRoot: true, forks: [{ name: "test-workspace-fork", dir: "/path/to/test-workspace-fork", running: false, needsInput: false, inProgress: false, pinned: false, title: "Fork 1" }] })];

      const { router } = renderWorkspaceDetailRouter("/workspaces/test-workspace/fork");

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Create Fork" })).toBeTruthy();
      });

      expect(screen.getByRole("link", { name: "Forks" })).toBeTruthy();
      expect(screen.getByRole("link", { name: "Fork" })).toBeTruthy();
      expect(router.state.location.pathname).toBe("/workspaces/test-workspace/fork");
    });

    it("redirects forked roots from progress routes to Forks", async () => {
      mockWorkspaces = [createMockWorkspace({ isRoot: true, forks: [{ name: "test-workspace-fork", dir: "/path/to/test-workspace-fork", running: false, needsInput: false, inProgress: false, pinned: false, title: "Fork 1" }] })];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/progress");

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith("/workspaces/test-workspace/forks", { replace: true });
      });

      expect(screen.getByRole("link", { name: "Forks" })).toBeTruthy();
      expect(screen.getByRole("link", { name: "Fork" })).toBeTruthy();
    });

    it("redirects unsupported tabs to Progress for standalone workspaces", async () => {
      mockWorkspaces = [createMockWorkspace()];

      renderWorkspaceDetailRouter("/workspaces/test-workspace/unknown");

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith("/workspaces/test-workspace/progress", { replace: true });
      });

      expect(screen.getByRole("link", { name: "Progress" })).toBeTruthy();
      expect(screen.queryByRole("heading", { name: /Unknown Tab — Not Yet Available/i })).toBeNull();
    });

    it("keeps hook order stable when detail loads after the initial skeleton", async () => {
      const originalConsoleError = console.error;
      const consoleErrorSpy = mock(() => {});
      console.error = consoleErrorSpy as typeof console.error;

      try {
        mockFetchStatus = "fetching";
        mockWorkspaces = [];

        const view = renderWorkspaceDetail();

        expect(screen.queryByText("Test Workspace Title")).toBeNull();

        mockFetchStatus = "idle";
        mockWorkspaces = [createMockWorkspace()];

        await act(async () => {
          view.rerender(workspaceDetailTestView());
        });

        await waitFor(() => {
          expect(screen.getByText("Test Workspace Title")).toBeTruthy();
        });

        const hookOrderErrors = consoleErrorSpy.mock.calls.flat().filter((value) => (
          typeof value === "string" && /Rendered (more|fewer) hooks/.test(value)
        ));

        expect(hookOrderErrors).toHaveLength(0);
      } finally {
        console.error = originalConsoleError;
      }
    });

    it("shows Start button when workspace is not running", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const startButtons = screen.queryAllByText("Start");
        expect(startButtons.length).toBeGreaterThan(0);
      });
    });

    it("navigates to Edit GOAL for an idle root workspace", async () => {
      const user = userEvent.setup();

      mockWorkspaces = [createMockWorkspace({ hasSgai: false, goalContent: "", rawGoalContent: "", isRoot: true })];

      renderWorkspaceDetail();

      const editGoalButton = await screen.findByRole("button", { name: "Edit GOAL" });
      await user.click(editGoalButton);

      expect(mockNavigate).toHaveBeenCalledWith("/workspaces/test-workspace/goal/edit");
    });

    it("renders the no-workspace Edit GOAL link without workspaceDir", async () => {
      mockWorkspaces = [createMockWorkspace({ hasSgai: false, isRoot: false })];

      renderWorkspaceDetail();

      const editGoalLink = await screen.findByRole("link", { name: "Edit GOAL" });
      expect(editGoalLink.getAttribute("href")).toBe("/workspaces/test-workspace/goal/edit");
    });

    it("shows Stop button when workspace is running", async () => {
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const stopButtons = screen.queryAllByText("Stop");
        expect(stopButtons.length).toBeGreaterThan(0);
      });
    });

    it("calls start API when Start button is clicked", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const startButtons = screen.queryAllByText("Start");
        expect(startButtons.length).toBeGreaterThan(0);
      });

      const startButtons = screen.getAllByText("Start");
      await user.click(startButtons[0]);

      await waitFor(() => {
        expect(mockStart).toHaveBeenCalledWith("test-workspace", false);
      });
    });

    it("calls stop API when Stop button is clicked", async () => {
      const user = userEvent.setup();
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const stopButtons = screen.queryAllByText("Stop");
        expect(stopButtons.length).toBeGreaterThan(0);
      });

      const stopButtons = screen.getAllByText("Stop");
      await user.click(stopButtons[0]);

      await waitFor(() => {
        expect(mockStop).toHaveBeenCalledWith("test-workspace");
      });
    });

    it("shows Self-drive button", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const selfDriveButtons = screen.queryAllByText("Self-drive");
        expect(selfDriveButtons.length).toBeGreaterThan(0);
      });
    });

    it("calls start API with auto=true when Self-drive is clicked", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const selfDriveButtons = screen.queryAllByText("Self-drive");
        expect(selfDriveButtons.length).toBeGreaterThan(0);
      });

      const selfDriveButtons = screen.getAllByText("Self-drive");
      await user.click(selfDriveButtons[0]);

      await waitFor(() => {
        expect(mockStart).toHaveBeenCalledWith("test-workspace", true);
      });
    });

    it("keeps Reset disabled while Self-drive is starting", async () => {
      const user = userEvent.setup();
      const startDeferred = deferredValue<{ running: boolean }>();
      mockStart.mockImplementation((workspaceName: string, auto: boolean) => {
        if (workspaceName === "test-workspace" && auto) {
          return startDeferred.promise;
        }
        return Promise.resolve({ running: true });
      });

      renderWorkspaceDetail();

      const selfDriveButton = await screen.findByRole("button", { name: "Self-drive" });
      const resetButton = await screen.findByRole("button", { name: "Reset" });

      await user.click(selfDriveButton);

      expect(resetButton.hasAttribute("disabled")).toBe(true);

      startDeferred.resolve({ running: true });

      await waitFor(() => {
        expect(mockStart).toHaveBeenCalledWith("test-workspace", true);
      });
    });
  });

  describe("state reloads on button click", () => {
    it("triggers factory refresh after start", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const startButtons = screen.queryAllByText("Start");
        expect(startButtons.length).toBeGreaterThan(0);
      });

      const startButtons = screen.getAllByText("Start");
      await user.click(startButtons[0]);

      await waitFor(() => {
        expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
      });
    });

    it("triggers factory refresh after stop", async () => {
      const user = userEvent.setup();
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const stopButtons = screen.queryAllByText("Stop");
        expect(stopButtons.length).toBeGreaterThan(0);
      });

      const stopButtons = screen.getAllByText("Stop");
      await user.click(stopButtons[0]);

      await waitFor(() => {
        expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
      });
    });

    it("triggers factory refresh after pin toggle", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const pinButtons = screen.queryAllByText("Pin");
        expect(pinButtons.length).toBeGreaterThan(0);
      });

      const pinButtons = screen.getAllByText("Pin");
      await user.click(pinButtons[0]);

      await waitFor(() => {
        expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
      });
    });
  });

  describe("workspace status display", () => {
    it("shows running badge when workspace is running", async () => {
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const runningBadges = screen.queryAllByText("running");
        expect(runningBadges.length).toBeGreaterThan(0);
      });
    });

    it("shows stopped badge when workspace is not running", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const stoppedBadges = screen.queryAllByText("stopped");
        expect(stoppedBadges.length).toBeGreaterThan(0);
      });
    });

    it("displays execution time", async () => {
      mockWorkspaces[0] = createMockWorkspace({ totalExecTime: "2m 30s" });

      renderWorkspaceDetail();

      await waitFor(() => {
        const execTimeElements = screen.queryAllByText("2m 30s");
        expect(execTimeElements.length).toBeGreaterThan(0);
        expect(execTimeElements[0]?.getAttribute("tabindex")).toBe("0");
      });
    });

    it("displays current agent and model", async () => {
      mockWorkspaces[0] = createMockWorkspace({
        running: true,
        currentAgent: "coordinator",
        currentModel: "opencode/glm-5",
      });

      renderWorkspaceDetail();

      await waitFor(() => {
        const coordinatorElements = screen.queryAllByText(/coordinator/);
        const glmElements = screen.queryAllByText(/glm-5/);
        expect(coordinatorElements.length).toBeGreaterThan(0);
        expect(glmElements.length).toBeGreaterThan(0);
      });
    });

    it("keeps truncated status text keyboard focusable for tooltip access", async () => {
      mockWorkspaces[0] = createMockWorkspace({
        running: true,
        currentAgent: "coordinator",
        currentModel: "opencode/glm-5",
        task: "Waiting for a very long human response that should stay reachable from the keyboard.",
      });

      renderWorkspaceDetail();

      await waitFor(() => {
        const agentModelBadge = screen.getByText("coordinator | glm-5");
        const statusLine = screen.getByText("Waiting for a very long human response that should stay reachable from the keyboard.");
        expect(agentModelBadge.getAttribute("tabindex")).toBe("0");
        expect(statusLine.getAttribute("tabindex")).toBe("0");
      });
    });
  });

  describe("pin functionality", () => {
    it("shows Pin button when workspace is not pinned", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const pinButtons = screen.queryAllByText("Pin");
        expect(pinButtons.length).toBeGreaterThan(0);
      });
    });

    it("shows Unpin button when workspace is pinned", async () => {
      mockWorkspaces[0] = createMockWorkspace({ pinned: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const unpinButtons = screen.queryAllByText("Unpin");
        expect(unpinButtons.length).toBeGreaterThan(0);
      });
    });

    it("calls togglePin API when clicked", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const pinButtons = screen.queryAllByText("Pin");
        expect(pinButtons.length).toBeGreaterThan(0);
      });

      const pinButtons = screen.getAllByText("Pin");
      await user.click(pinButtons[0]);

      await waitFor(() => {
        expect(mockTogglePin).toHaveBeenCalledWith("test-workspace");
      });
    });
  });

  describe("delete functionality", () => {
    it("shows Detach button when workspace policy is detach-only", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const detachButtons = screen.queryAllByRole("button", { name: "Detach" });
        expect(detachButtons.length).toBeGreaterThan(0);
      });
    });

    it("opens detach confirmation dialog", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const detachButtons = screen.queryAllByRole("button", { name: "Detach" });
        expect(detachButtons.length).toBeGreaterThan(0);
      });

      const detachButtons = screen.getAllByRole("button", { name: "Detach" });
      await user.click(detachButtons[0]);

      await waitFor(() => {
        const detachWorkspaceElements = screen.queryAllByText("Detach workspace");
        expect(detachWorkspaceElements.length).toBeGreaterThan(0);
        expect(screen.queryAllByText(/will NOT be deleted/i).length).toBeGreaterThan(0);
      });
    });

    it("uses the workspace action API to detach detach-only repositories", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const detachButtons = screen.queryAllByRole("button", { name: "Detach" });
        expect(detachButtons.length).toBeGreaterThan(0);
      });

      const detachButtons = screen.getAllByRole("button", { name: "Detach" });
      await user.click(detachButtons[0]);

      const dialog = await screen.findByRole("alertdialog");
      await user.click(within(dialog).getByRole("button", { name: /^Detach$/ }));

      await waitFor(() => {
        expect(mockDeleteWorkspace).toHaveBeenCalledWith("test-workspace", "detach");
      });
    });

    it("shows a choose-action entrypoint for fork repositories", async () => {
      const user = userEvent.setup();
      mockWorkspaces = [createMockWorkspace({
        isFork: true,
        repositoryAction: createRepositoryAction({
          repositoryMode: "fork",
          entryPoint: "choose",
          allowedOperations: ["detach", "delete"],
          defaultOperation: "",
        }),
      })];

      renderWorkspaceDetail();

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Choose action" })).toBeTruthy();
      });

      await user.click(screen.getByRole("button", { name: "Choose action" }));

      await waitFor(() => {
        expect(screen.getByText("Choose fork action")).toBeTruthy();
        expect(screen.getByRole("button", { name: /^Detach$/ })).toBeTruthy();
        expect(screen.getByRole("button", { name: /^Delete$/ })).toBeTruthy();
      });
    });

    it("does not infer a confirm action when backend omits the default operation", async () => {
      mockWorkspaces = [createMockWorkspace({
        repositoryAction: createRepositoryAction({
          entryPoint: "confirm",
          allowedOperations: ["detach"],
          defaultOperation: undefined,
        }),
      })];

      renderWorkspaceDetail();

      await waitFor(() => {
        expect(screen.getByText("Test Workspace Title")).toBeTruthy();
      });

      expect(screen.queryByRole("button", { name: "Detach" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Choose action" })).toBeNull();
    });

    it("keeps the root visible and makes it detach-only after detaching the last fork", async () => {
      const user = userEvent.setup();

      mockWorkspaces = [
        createMockWorkspace({
          name: "root-ws",
          dir: "/path/to/root-ws",
          isRoot: true,
          title: "Root Workspace",
          repositoryAction: createRepositoryAction({
            repositoryMode: "root",
            entryPoint: "hidden",
            allowedOperations: [],
            defaultOperation: "",
            attachedForkCount: 1,
          }),
          forks: [
            {
              name: "root-ws-fork-1",
              dir: "/path/to/root-ws-fork-1",
              running: false,
              needsInput: false,
              inProgress: false,
              pinned: false,
              title: "Last Fork",
            },
          ],
        }),
        createMockWorkspace({
          name: "root-ws-fork-1",
          dir: "/path/to/root-ws-fork-1",
          isFork: true,
          title: "Last Fork",
          repositoryAction: createRepositoryAction({
            repositoryMode: "fork",
            entryPoint: "choose",
            allowedOperations: ["detach", "delete"],
            defaultOperation: "",
          }),
        }),
      ];

      mockDeleteWorkspace.mockImplementation(async (name, operation) => {
        expect(name).toBe("root-ws-fork-1");
        expect(operation).toBe("detach");
        mockWorkspaces = [
          createMockWorkspace({
            name: "root-ws",
            dir: "/path/to/root-ws",
            isRoot: true,
            title: "Root Workspace",
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

      const view = renderWorkspaceDetail("root-ws", "forks");

      await user.click(await screen.findByRole("button", { name: "Choose action for fork root-ws-fork-1" }));
      await user.click(await screen.findByRole("button", { name: /^Detach$/ }));

      await waitFor(() => {
        expect(mockDeleteWorkspace).toHaveBeenCalledWith("root-ws-fork-1", "detach");
      });

      view.rerender(workspaceDetailTestView("root-ws", "forks"));

      await waitFor(() => {
        expect(screen.getByText("Root Workspace")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Detach" })).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Choose action" })).toBeNull();
      });
    });

    it("hides running fork-row actions when backend policy marks them hidden", async () => {
      mockWorkspaces = [
        createMockWorkspace({
          isRoot: true,
          forks: [{
            name: "running-fork",
            dir: "/path/to/running-fork",
            running: true,
            needsInput: false,
            inProgress: false,
            pinned: false,
            title: "Running Fork",
          }],
        }),
        createMockWorkspace({
          name: "running-fork",
          dir: "/path/to/running-fork",
          running: true,
          isFork: true,
          title: "Running Fork",
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

      renderWorkspaceDetailRouter("/workspaces/test-workspace/forks");

      await waitFor(() => {
        expect(screen.getByText("Running Fork")).toBeTruthy();
      });

      expect(screen.queryByRole("button", { name: "Choose action for fork running-fork" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Detach running-fork" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Delete running-fork" })).toBeNull();
    });

    it("hides repository actions when backend policy marks them hidden", async () => {
      mockWorkspaces = [createMockWorkspace({
        isRoot: true,
        repositoryAction: createRepositoryAction({
          repositoryMode: "root",
          entryPoint: "hidden",
          allowedOperations: [],
          defaultOperation: "",
          disabledReason: "topology-unavailable",
        }),
      })];

      renderWorkspaceDetail();

      await waitFor(() => {
        expect(screen.queryByRole("button", { name: "Detach" })).toBeNull();
        expect(screen.queryByRole("button", { name: "Choose action" })).toBeNull();
        expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
      });
    });
  });

  describe("needs input state", () => {
    it("shows Respond button when workspace needs input", async () => {
      mockWorkspaces[0] = createMockWorkspace({ needsInput: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const respondButtons = screen.queryAllByText("Respond");
        expect(respondButtons.length).toBeGreaterThan(0);
      });
    });
  });

  describe("error handling", () => {
    it("shows error message when start fails", async () => {
      const user = userEvent.setup();
      mockStart.mockImplementationOnce(() => Promise.reject(new Error("Failed to start workspace")));

      renderWorkspaceDetail();

      await waitFor(() => {
        const startButtons = screen.queryAllByText("Start");
        expect(startButtons.length).toBeGreaterThan(0);
      });

      const startButtons = screen.getAllByText("Start");
      await user.click(startButtons[0]);

      await waitFor(() => {
        const errorElements = screen.queryAllByText(/Failed to start workspace/);
        expect(errorElements.length).toBeGreaterThan(0);
        const errorAlert = errorElements.find(el => el.getAttribute("role") === "alert");
        expect(errorAlert).toBeTruthy();
      });
    });

    it("shows error message when stop fails", async () => {
      const user = userEvent.setup();
      mockWorkspaces[0] = createMockWorkspace({ running: true });
      mockStop.mockImplementationOnce(() => Promise.reject(new Error("Failed to stop workspace")));

      renderWorkspaceDetail();

      await waitFor(() => {
        const stopButtons = screen.queryAllByText("Stop");
        expect(stopButtons.length).toBeGreaterThan(0);
      });

      const stopButtons = screen.getAllByText("Stop");
      await user.click(stopButtons[0]);

      await waitFor(() => {
        const errorElements = screen.queryAllByText(/Failed to stop workspace/);
        expect(errorElements.length).toBeGreaterThan(0);
        const errorAlert = errorElements.find(el => el.getAttribute("role") === "alert");
        expect(errorAlert).toBeTruthy();
      });

      await waitFor(() => {
        const runningBadges = screen.queryAllByText("running");
        expect(runningBadges.length).toBeGreaterThan(0);
      });

      expect(screen.queryByText("stopped")).toBeNull();
    });

    it("shows error message when pin toggle fails", async () => {
      const user = userEvent.setup();
      mockTogglePin.mockImplementationOnce(() => Promise.reject(new Error("Failed to toggle pin")));

      renderWorkspaceDetail();

      await waitFor(() => {
        const pinButtons = screen.queryAllByText("Pin");
        expect(pinButtons.length).toBeGreaterThan(0);
      });

      const pinButtons = screen.getAllByText("Pin");
      await user.click(pinButtons[0]);

      await waitFor(() => {
        const errorElements = screen.queryAllByText(/Failed to toggle pin/);
        expect(errorElements.length).toBeGreaterThan(0);
        const errorAlert = errorElements.find(el => el.getAttribute("role") === "alert");
        expect(errorAlert).toBeTruthy();
      });
    });
  });

  describe("accessibility", () => {
    it("all action buttons are keyboard accessible", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const startButtons = screen.queryAllByRole("button", { name: /Start/ });
        expect(startButtons.length).toBeGreaterThan(0);
      });
    });

    it("buttons have accessible names", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const startButton = screen.queryAllByRole("button", { name: /Start/ });
        const selfDriveButton = screen.queryAllByRole("button", { name: /Self-drive/ });
        const pinButton = screen.queryAllByRole("button", { name: /Pin/ });
        const repositoryActionButton = screen.queryAllByRole("button", { name: /Choose action|Detach|Delete/ });

        expect(startButton.length).toBeGreaterThan(0);
        expect(selfDriveButton.length).toBeGreaterThan(0);
        expect(pinButton.length).toBeGreaterThan(0);
        expect(repositoryActionButton.length).toBeGreaterThan(0);
      });
    });

    it("status badges have accessible labels", async () => {
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const statusBadge = screen.queryAllByText("running");
        expect(statusBadge.length).toBeGreaterThan(0);
      });
    });
  });

  describe("button state management", () => {
    it("disables Start button when workspace is running", async () => {
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const startButtons = screen.queryAllByRole("button", { name: /Start/ });
        const disabledStartButtons = startButtons.filter(btn => btn.hasAttribute("disabled"));
        expect(disabledStartButtons.length).toBeGreaterThan(0);
      });
    });
  });

  describe("workspace information display", () => {
    it("displays workspace title", async () => {
      mockWorkspaces[0] = createMockWorkspace({ title: "My Test Workspace Title" });

      renderWorkspaceDetail();

      await waitFor(() => {
        const titleElements = screen.queryAllByText("My Test Workspace Title");
        expect(titleElements.length).toBeGreaterThan(0);
      });
    });

    it("displays current task when running", async () => {
      mockWorkspaces[0] = createMockWorkspace({
        running: true,
        task: "Writing tests for authentication",
      });

      renderWorkspaceDetail();

      await waitFor(() => {
        const taskElements = screen.queryAllByText(/Writing tests for authentication/);
        expect(taskElements.length).toBeGreaterThan(0);
      });
    });
  });

  describe("Open Editor functionality", () => {
    it("shows Open Editor button", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const openEditorButtons = screen.queryAllByText("Open in Editor");
        expect(openEditorButtons.length).toBeGreaterThan(0);
      });
    });

    it("calls openEditor API when button is clicked", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const openEditorButtons = screen.queryAllByText("Open in Editor");
        expect(openEditorButtons.length).toBeGreaterThan(0);
      });

      const openEditorButtons = screen.getAllByText("Open in Editor");
      await user.click(openEditorButtons[0]);

      await waitFor(() => {
        expect(mockOpenEditor).toHaveBeenCalledWith("test-workspace");
      });
    });

    it("shows error when openEditor fails", async () => {
      const user = userEvent.setup();
      mockOpenEditor.mockImplementationOnce(() => Promise.reject(new Error("Failed to open editor")));

      renderWorkspaceDetail();

      await waitFor(() => {
        const openEditorButtons = screen.queryAllByText("Open in Editor");
        expect(openEditorButtons.length).toBeGreaterThan(0);
      });

      const openEditorButtons = screen.getAllByText("Open in Editor");
      await user.click(openEditorButtons[0]);

      await waitFor(() => {
        const errorElements = screen.getAllByText(/Failed to open editor/);
        expect(errorElements.length).toBeGreaterThan(0);
      });
    });
  });

  describe("Self-drive button state", () => {
    it("disables Self-drive button when workspace is running", async () => {
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const selfDriveButtons = screen.queryAllByRole("button", { name: /Self-drive/i });
        expect(selfDriveButtons.length).toBeGreaterThan(0);
        const disabledButtons = selfDriveButtons.filter(btn => btn.hasAttribute("disabled"));
        expect(disabledButtons.length).toBeGreaterThan(0);
      });
    });
  });

  describe("Respond button functionality", () => {
    it("shows Respond button when workspace needs input", async () => {
      mockWorkspaces[0] = createMockWorkspace({ needsInput: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const respondButtons = screen.queryAllByRole("button", { name: /Respond/ });
        expect(respondButtons.length).toBeGreaterThan(0);
      });
    });

    it("Respond button navigates to respond page", async () => {
      const user = userEvent.setup();
      mockWorkspaces[0] = createMockWorkspace({ needsInput: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        const respondButtons = screen.queryAllByRole("button", { name: /Respond/ });
        expect(respondButtons.length).toBeGreaterThan(0);
      });

      const respondButtons = screen.getAllByRole("button", { name: /Respond/ });
      await user.click(respondButtons[0]);

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith("/workspaces/test-workspace/respond");
      });
    });
  });

  describe("critical actions without optimistic updates", () => {
    it("pin toggle calls API before triggering refresh", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const pinButtons = screen.queryAllByText("Pin");
        expect(pinButtons.length).toBeGreaterThan(0);
      });

      const pinButtons = screen.getAllByText("Pin");
      await user.click(pinButtons[0]);

      await waitFor(() => {
        expect(mockTogglePin).toHaveBeenCalledWith("test-workspace");
        expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
      });
    });
  });

  describe("reset functionality", () => {
    it("renders Reset as a destructive action when workspace is not running and not continuousMode", async () => {
      renderWorkspaceDetail();

      await waitFor(() => {
        const resetButton = screen.getByRole("button", { name: "Reset" });
        expect(resetButton.className.includes("bg-destructive")).toBe(true);
        expect(resetButton.className.includes("text-white")).toBe(true);
      });
    });

    it("hides Reset button when workspace is running", async () => {
      mockWorkspaces[0] = createMockWorkspace({ running: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        expect(screen.queryByRole("button", { name: "Reset" })).toBeNull();
      });
    });

    it("hides Reset button when continuousMode is active", async () => {
      mockWorkspaces[0] = createMockWorkspace({ continuousMode: true });

      renderWorkspaceDetail();

      await waitFor(() => {
        expect(screen.queryByRole("button", { name: "Reset" })).toBeNull();
      });
    });

    it("opens confirmation dialog when Reset button is clicked", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const resetButtons = screen.queryAllByRole("button", { name: "Reset" });
        expect(resetButtons.length).toBeGreaterThan(0);
      });

      const resetButtons = screen.getAllByRole("button", { name: "Reset" });
      await user.click(resetButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Reset workspace state?")).toBeTruthy();
        expect(screen.getByText(/This will reset the workflow state/)).toBeTruthy();
      });
    });

    it("calls reset API with correct workspace name when confirmed", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const resetButtons = screen.queryAllByRole("button", { name: "Reset" });
        expect(resetButtons.length).toBeGreaterThan(0);
      });

      const resetButtons = screen.getAllByRole("button", { name: "Reset" });
      await user.click(resetButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Reset workspace state?")).toBeTruthy();
      });

      const confirmButtons = screen.getAllByRole("button", { name: "Reset" });
      const confirmButton = confirmButtons.find(btn => btn.closest("[role='alertdialog']"));
      await user.click(confirmButton!);

      await waitFor(() => {
        expect(mockReset).toHaveBeenCalledWith("test-workspace");
      });
    });

    it("triggers factory refresh after reset", async () => {
      const user = userEvent.setup();

      renderWorkspaceDetail();

      await waitFor(() => {
        const resetButtons = screen.queryAllByRole("button", { name: "Reset" });
        expect(resetButtons.length).toBeGreaterThan(0);
      });

      const resetButtons = screen.getAllByRole("button", { name: "Reset" });
      await user.click(resetButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Reset workspace state?")).toBeTruthy();
      });

      const confirmButtons = screen.getAllByRole("button", { name: "Reset" });
      const confirmButton = confirmButtons.find(btn => btn.closest("[role='alertdialog']"));
      await user.click(confirmButton!);

      await waitFor(() => {
        expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
      });
    });

    it("shows error message when reset fails", async () => {
      const user = userEvent.setup();
      mockReset.mockImplementationOnce(() => Promise.reject(new Error("Failed to reset workspace")));

      renderWorkspaceDetail();

      await waitFor(() => {
        const resetButtons = screen.queryAllByRole("button", { name: "Reset" });
        expect(resetButtons.length).toBeGreaterThan(0);
      });

      const resetButtons = screen.getAllByRole("button", { name: "Reset" });
      await user.click(resetButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Reset workspace state?")).toBeTruthy();
      });

      const confirmButtons = screen.getAllByRole("button", { name: "Reset" });
      const confirmButton = confirmButtons.find(btn => btn.closest("[role='alertdialog']"));
      await user.click(confirmButton!);

      await waitFor(() => {
        const errorElements = screen.queryAllByText(/Failed to reset workspace/);
        expect(errorElements.length).toBeGreaterThan(0);
        const errorAlert = errorElements.find(el => el.getAttribute("role") === "alert");
        expect(errorAlert).toBeTruthy();
      });
    });
  });
});
