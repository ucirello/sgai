import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { act, render, screen, waitFor, cleanup, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route, useLocation } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { SidebarProvider } from "@/components/ui/sidebar";
import * as factoryStateModule from "@/lib/factory-state";
import { api } from "@/lib/api";
import * as sidebarResizeModule from "@/hooks/useSidebarResize";
import * as mobileModule from "@/hooks/use-mobile";
import { Dashboard } from "../Dashboard";

// Override pointer-events on body to allow interactions in tests
beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function waitForForksRedirect(expectedPath: string) {
  await waitFor(() => {
    expect(screen.getByTestId("route-path").textContent).toBe(expectedPath);
    expect(screen.getByTestId("redirect-target").textContent).toBe("Redirected to forks");
  });
}

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
  title: "",
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

const createMockWorkspace = (overrides: Record<string, unknown> = {}) => (
  (() => {
    const repositoryActionOverrides = typeof overrides === "object" && overrides !== null && "repositoryAction" in overrides
      ? overrides.repositoryAction as Record<string, unknown>
      : undefined;
    const hasPresentationOverride = Boolean(repositoryActionOverrides && "presentation" in repositoryActionOverrides);
    const workspace = {
      name: "workspace",
      dir: "/path/to/workspace",
      running: false,
      needsInput: false,
      inProgress: false,
      pinned: false,
      isRoot: false,
      isFork: false,
      title: "",
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

function createDefaultMockWorkspaces() {
  return [
    createMockWorkspace({
      name: "workspace-1",
      dir: "/path/to/workspace-1",
      title: "Workspace One Title",
    }),
    createMockWorkspace({
      name: "workspace-2",
      dir: "/path/to/workspace-2",
      running: true,
      inProgress: true,
      pinned: true,
      isRoot: true,
      title: "Workspace Two Title",
      currentAgent: "coordinator",
      currentModel: "opencode/glm-5",
      task: "Working on task",
      totalExecTime: "1m 30s",
      repositoryAction: createRepositoryAction({
        repositoryMode: "root",
        entryPoint: "hidden",
        allowedOperations: [],
        defaultOperation: "",
        disabledReason: "running",
        attachedForkCount: 1,
        running: true,
      }),
      forks: [
        {
          name: "workspace-2-fork-1",
          dir: "/path/to/workspace-2-fork-1",
          running: false,
          needsInput: true,
          inProgress: false,
          pinned: false,
          title: "Workspace Two Fork Title",
        },
      ],
    }),
    createMockWorkspace({
      name: "workspace-2-fork-1",
      dir: "/path/to/workspace-2-fork-1",
      needsInput: true,
      isFork: true,
      title: "Workspace Two Fork Title",
      repositoryAction: createRepositoryAction({
        repositoryMode: "fork",
        entryPoint: "choose",
        allowedOperations: ["detach", "delete"],
        defaultOperation: "",
      }),
    }),
    createMockWorkspace({
      name: "workspace-3",
      dir: "/path/to/workspace-3",
      needsInput: true,
      inProgress: true,
      computedTitle: "Needs Input Fallback Title",
      external: true,
    }),
    createMockWorkspace({
      name: "root-unpinned",
      dir: "/path/to/root-unpinned",
      isRoot: true,
      pinned: false,
      title: "Unpinned Root Title",
      repositoryAction: createRepositoryAction({
        repositoryMode: "root",
        entryPoint: "hidden",
        allowedOperations: [],
        defaultOperation: "",
        disabledReason: "forks-attached",
        attachedForkCount: 1,
      }),
      forks: [
        {
          name: "orphan-pinned-fork",
          dir: "/path/to/orphan-pinned-fork",
          running: false,
          needsInput: false,
          inProgress: false,
          pinned: true,
          title: "Orphan Pinned Fork Title",
        },
      ],
    }),
    createMockWorkspace({
      name: "orphan-pinned-fork",
      dir: "/path/to/orphan-pinned-fork",
      isFork: true,
      pinned: true,
      title: "Orphan Pinned Fork Title",
      repositoryAction: createRepositoryAction({
        repositoryMode: "fork",
        entryPoint: "choose",
        allowedOperations: ["detach", "delete"],
        defaultOperation: "",
      }),
    }),
  ];
}

let mockWorkspaces = createDefaultMockWorkspaces();
const mockTriggerFactoryRefresh = mock(() => {});

const mockDeleteWorkspace = mock(() => Promise.resolve({ deleted: true }));
const mockHandleMouseDown = mock(() => {});

function dashboardTestView(initialRoute = "/") {
  function RoutePathProbe() {
    const location = useLocation();
    return <div data-testid="route-path">{location.pathname}</div>;
  }

  return (
    <MemoryRouter initialEntries={[initialRoute]}>
        <TooltipProvider>
          <SidebarProvider>
          <Routes>
            <Route path="/workspaces/attach" element={<Dashboard><div data-testid="attach-page">Attach page</div></Dashboard>} />
            <Route path="/workspaces/:name/forks" element={<Dashboard><><RoutePathProbe /><div data-testid="redirect-target">Redirected to forks</div></></Dashboard>} />
            <Route path="/workspaces/:name/*" element={<Dashboard><RoutePathProbe /></Dashboard>} />
            <Route path="*" element={<Dashboard><div data-testid="dashboard-content">Content</div></Dashboard>} />
          </Routes>
        </SidebarProvider>
      </TooltipProvider>
    </MemoryRouter>
  );
}

function renderDashboard(initialRoute = "/") {
  return render(dashboardTestView(initialRoute));
}

describe("Dashboard", () => {
  beforeEach(() => {
    mockDeleteWorkspace.mockClear();
    mockTriggerFactoryRefresh.mockClear();
    mockHandleMouseDown.mockClear();
    mockWorkspaces = createDefaultMockWorkspaces();

    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: mockWorkspaces,
      fetchStatus: "idle",
      lastFetchedAt: Date.now(),
    }));
    spyOn(factoryStateModule, "triggerFactoryRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(api.workspaces, "deleteWorkspace").mockImplementation((...args) => mockDeleteWorkspace(...args));
    spyOn(sidebarResizeModule, "useSidebarResize").mockImplementation(() => ({
      sidebarWidth: 280,
      handleMouseDown: mockHandleMouseDown,
    }));
    spyOn(mobileModule, "useIsMobile").mockImplementation(() => false);
  });

  afterEach(() => {
    mock.restore();
    cleanup();
  });

  describe("display repository tree correctly", () => {
    it("renders workspace list with all workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const ws1Elements = screen.queryAllByText("Workspace One Title");
        expect(ws1Elements.length).toBeGreaterThan(0);
      });

      await waitFor(() => {
        const ws2Elements = screen.queryAllByText("Workspace Two Title");
        expect(ws2Elements.length).toBeGreaterThan(0);
      });
      
      await waitFor(() => {
        const ws3Elements = screen.queryAllByText("Needs Input Fallback Title");
        expect(ws3Elements.length).toBeGreaterThan(0);
      });
    });

    it("shows workspace titles when available", async () => {
      renderDashboard();

      await waitFor(() => {
        const ws1Elements = screen.queryAllByText("Workspace One Title");
        expect(ws1Elements.length).toBeGreaterThan(0);
      });
    });

    it("displays external workspace indicator", async () => {
      renderDashboard();

      await waitFor(() => {
        const externalIndicators = screen.queryAllByLabelText("External workspace");
        expect(externalIndicators.length).toBeGreaterThan(0);
      });
    });

    it("shows running indicator for active workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const runningIndicators = screen.queryAllByLabelText("Running");
        expect(runningIndicators.length).toBeGreaterThan(0);
      });
    });

    it("shows needs input indicator", async () => {
      renderDashboard();

      await waitFor(() => {
        const needsInputIndicators = screen.queryAllByLabelText("Waiting for response");
        expect(needsInputIndicators.length).toBeGreaterThan(0);
      });
    });
  });

  describe("show pinned repositories", () => {
    it("displays pinned section when workspaces are pinned", async () => {
      renderDashboard();

      await waitFor(() => {
        const pinnedSections = screen.queryAllByRole("region", { name: "Pinned" });
        expect(pinnedSections.length).toBeGreaterThan(0);
      });
    });

    it("shows pinned indicator on pinned workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const pinnedIndicators = screen.queryAllByLabelText("Pinned");
        expect(pinnedIndicators.length).toBeGreaterThan(0);
      });
    });
  });

  describe("handle fork nesting", () => {
    it("displays expand button for workspaces with forks", async () => {
      renderDashboard();

      await waitFor(() => {
        const expandButtons = screen.queryAllByRole("button", { name: /forks for /i });
        expect(expandButtons.length).toBeGreaterThan(0);
      });
    });

    it("expands to show nested forks when clicked", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const ws2Elements = screen.queryAllByText("Workspace Two Title");
        expect(ws2Elements.length).toBeGreaterThan(0);
      });

      const expandButtons = screen.getAllByRole("button", { name: /forks for /i });
      await user.click(expandButtons[0]);

      await waitFor(() => {
        const forkElements = screen.queryAllByText("Workspace Two Fork Title");
        expect(forkElements.length).toBeGreaterThan(0);
      });
    });
  });

  describe("update on state changes", () => {
    it("reflects workspace running state", async () => {
      renderDashboard();

      await waitFor(() => {
        const runningBadges = screen.queryAllByLabelText("Running");
        expect(runningBadges.length).toBeGreaterThan(0);
      });
    });

    it("shows needs input state for workspaces awaiting response", async () => {
      renderDashboard();

      await waitFor(() => {
        const needsInputIndicators = screen.queryAllByLabelText("Waiting for response");
        expect(needsInputIndicators.length).toBeGreaterThan(0);
      });
    });
  });

  describe("workspace actions", () => {
    it("shows detach button on detach-only workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const detachButtons = screen.queryAllByLabelText("Detach workspace-1");
        expect(detachButtons.length).toBeGreaterThan(0);
      });
    });

    it("opens detach confirmation dialog for detach-only workspaces", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const ws1Elements = screen.queryAllByText("Workspace One Title");
        expect(ws1Elements.length).toBeGreaterThan(0);
      });

      const detachButtons = screen.getAllByLabelText("Detach workspace-1");
      await user.click(detachButtons[0]);

      await waitFor(() => {
        const alertDialogs = screen.queryAllByRole("alertdialog");
        expect(alertDialogs.length).toBeGreaterThan(0);

        const detachWorkspaceElements = screen.queryAllByText(/Detach workspace/);
        expect(detachWorkspaceElements.length).toBeGreaterThan(0);
        expect(screen.queryAllByText(/will NOT be deleted/i).length).toBeGreaterThan(0);
      });
    });

    it("does not show hidden actions for pinned roots with attached forks", async () => {
      renderDashboard();

      await waitFor(() => {
        expect(screen.queryByLabelText("Delete workspace-2")).toBeNull();
        expect(screen.queryByLabelText("Detach workspace-2")).toBeNull();
        expect(screen.queryByLabelText("Choose action for workspace-2")).toBeNull();
      });
    });

    it("does not show tree actions for running repositories", async () => {
      mockWorkspaces = [
        createMockWorkspace({
          name: "running-ws",
          dir: "/path/to/running-ws",
          running: true,
          title: "Running Workspace",
          repositoryAction: createRepositoryAction({
            entryPoint: "hidden",
            allowedOperations: [],
            defaultOperation: "",
            disabledReason: "running",
            running: true,
          }),
        }),
      ];

      renderDashboard();

      await waitFor(() => {
        expect(screen.getAllByText("Running Workspace").length).toBeGreaterThan(0);
      });

      expect(screen.queryByLabelText("Detach running-ws")).toBeNull();
      expect(screen.queryByLabelText("Delete running-ws")).toBeNull();
      expect(screen.queryByLabelText("Choose action for running-ws")).toBeNull();
      expect(screen.queryByLabelText("Choose action for fork running-ws")).toBeNull();
    });

    it("renders duplicate-name workspaces as separate tree rows", async () => {
      mockWorkspaces = [
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/first-parent/shared-ws",
          title: "shared-ws",
          computedTitle: "",
        }),
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/second-parent/shared-ws",
          title: "shared-ws",
          computedTitle: "",
        }),
      ];

      renderDashboard();

      await waitFor(() => {
        expect(screen.getByText("shared-ws · first-parent")).toBeTruthy();
        expect(screen.getByText("shared-ws · second-parent")).toBeTruthy();
      });
    });

    it("targets the correct duplicate-name workspace when deleting from the tree", async () => {
      const user = userEvent.setup();

      mockWorkspaces = [
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/first-parent/shared-ws",
          title: "shared-ws",
          computedTitle: "",
          repositoryAction: createRepositoryAction({
            repositoryMode: "standalone",
            entryPoint: "confirm",
            allowedOperations: ["delete"],
            defaultOperation: "delete",
          }),
        }),
        createMockWorkspace({
          name: "shared-ws",
          dir: "/tmp/second-parent/shared-ws",
          title: "shared-ws",
          computedTitle: "",
          repositoryAction: createRepositoryAction({
            repositoryMode: "standalone",
            entryPoint: "confirm",
            allowedOperations: ["delete"],
            defaultOperation: "delete",
          }),
        }),
      ];

      renderDashboard();

      await waitFor(() => {
        expect(screen.getByText("shared-ws · second-parent")).toBeTruthy();
      });

      await user.click(screen.getByLabelText("Delete shared-ws · second-parent"));

      const dialog = await screen.findByRole("alertdialog");
      await user.click(within(dialog).getByRole("button", { name: /^Delete$/ }));

      await waitFor(() => {
        expect(mockDeleteWorkspace).toHaveBeenCalledWith("shared-ws", "delete", "/tmp/second-parent/shared-ws");
      });
    });
  });

  describe("in progress section", () => {
    it("shows in progress section for active workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const inProgressSections = screen.queryAllByRole("region", { name: "In progress" });
        expect(inProgressSections.length).toBeGreaterThan(0);
      });
    });
  });

  describe("accessibility", () => {
    it("all workspace links are keyboard accessible", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const workspaceLinks = screen.getAllByRole("link").filter(link => 
          link.getAttribute("href")?.startsWith("/workspaces/")
        );
        expect(workspaceLinks.length).toBeGreaterThan(0);
        
        const firstLink = workspaceLinks[0];
        expect(firstLink.getAttribute("tabindex")).not.toBe("-1");
        firstLink.focus();
        expect(document.activeElement).toBe(firstLink);
      });
    });

    it("workspace sections have proper ARIA landmarks", async () => {
      renderDashboard();

      await waitFor(() => {
        const pinnedRegion = screen.queryAllByRole("region", { name: "Pinned" });
        const inProgressRegion = screen.queryAllByRole("region", { name: "In progress" });
        
        expect(pinnedRegion.length + inProgressRegion.length).toBeGreaterThan(0);
      });
    });

    it("status indicators have accessible labels", async () => {
      renderDashboard();

      await waitFor(() => {
        const runningIndicator = screen.queryAllByLabelText("Running");
        const needsInputIndicator = screen.queryAllByLabelText("Waiting for response");
        const externalIndicator = screen.queryAllByLabelText("External workspace");

        expect(runningIndicator.length + needsInputIndicator.length + externalIndicator.length).toBeGreaterThan(0);
      });
    });
  });

  describe("fork vs workspace deletion", () => {
    it("opens the fork detach-or-delete dialog", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const expandButtons = screen.queryAllByRole("button", { name: /forks for /i });
        expect(expandButtons.length).toBeGreaterThan(0);
      });

      const expandButtons = screen.getAllByRole("button", { name: /forks for /i });
      await user.click(expandButtons[0]);

      await waitFor(() => {
        const forkElements = screen.queryAllByText("Workspace Two Fork Title");
        expect(forkElements.length).toBeGreaterThan(0);
      });

      const forkActionButtons = screen.getAllByLabelText(/Choose action for fork workspace-2-fork-1/);
      expect(forkActionButtons.length).toBeGreaterThan(0);
      await user.click(forkActionButtons[0]);

      await waitFor(() => {
        const confirmButtons = screen.queryAllByRole("button", { name: /^Delete$/ });
        expect(confirmButtons.length).toBeGreaterThan(0);
        const detachButtons = screen.queryAllByRole("button", { name: /^Detach$/ });
        expect(detachButtons.length).toBeGreaterThan(0);
      });

      const dialogs = screen.getAllByRole("alertdialog");
      const dialog = dialogs[dialogs.length - 1];
      expect(within(dialog).getByText("Choose fork action")).toBeTruthy();
      expect(within(dialog).getByText(/Choose what to do with fork 'workspace-2-fork-1'/i)).toBeTruthy();
      expect(within(dialog).getByText(/Delete permanently removes the fork from disk/i)).toBeTruthy();
    });

    it("detaches fork rows through the workspace action endpoint", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const expandButtons = screen.queryAllByRole("button", { name: /forks for /i });
        expect(expandButtons.length).toBeGreaterThan(0);
      });

      const expandButtons = screen.getAllByRole("button", { name: /forks for /i });
      await user.click(expandButtons[0]);

      await waitFor(() => {
        const forkElements = screen.queryAllByText("Workspace Two Fork Title");
        expect(forkElements.length).toBeGreaterThan(0);
      });

      const forkActionButtons = screen.getAllByLabelText(/Choose action for fork workspace-2-fork-1/);
      await user.click(forkActionButtons[0]);
      await user.click(screen.getByRole("button", { name: /^Detach$/ }));

      await waitFor(() => {
        expect(mockDeleteWorkspace).toHaveBeenCalledWith("workspace-2-fork-1", "detach", "/path/to/workspace-2-fork-1");
      });
    });

    it("keeps the root visible and turns it into detach-only after deleting the last fork", async () => {
      const user = userEvent.setup();
      const deleteCompletion = deferredValue<{ deleted: boolean }>();

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
        expect(operation).toBe("delete");
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
        return deleteCompletion.promise;
      });

      const view = renderDashboard("/workspaces/root-ws-fork-1/progress");

      mockTriggerFactoryRefresh.mockImplementationOnce(() => {
        view.rerender(dashboardTestView("/workspaces/root-ws-fork-1/progress"));
      });

      await user.click(screen.getByRole("button", { name: "Choose action for fork root-ws-fork-1" }));
      await user.click(await screen.findByRole("button", { name: /^Delete$/ }));

      await waitFor(() => {
        expect(mockDeleteWorkspace).toHaveBeenCalledWith("root-ws-fork-1", "delete", "/path/to/root-ws-fork-1");
      });

      await act(async () => {
        deleteCompletion.resolve({ deleted: true });
        await deleteCompletion.promise;
      });

      await waitFor(() => {
        expect(mockTriggerFactoryRefresh).toHaveBeenCalledTimes(1);
        expect(screen.getByRole("button", { name: "Detach root-ws" })).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Choose action for fork root-ws-fork-1" })).toBeNull();
      });

      await waitForForksRedirect("/workspaces/root-ws/forks");

      await waitFor(() => {
        expect(screen.getByTestId("route-path").textContent).toBe("/workspaces/root-ws/forks");
        expect(screen.getByText("Root Workspace")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Detach root-ws" })).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Choose action for fork root-ws-fork-1" })).toBeNull();
        expect(screen.queryByText("Last Fork")).toBeNull();
      });
    });

    it("shows detach button for standalone workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const detachButtons = screen.queryAllByLabelText("Detach workspace-1");
        expect(detachButtons.length).toBeGreaterThan(0);
      });
    });
  });

  describe("external repository handling", () => {
    it("displays external indicator for external repositories", async () => {
      renderDashboard();

      await waitFor(() => {
        const externalIndicators = screen.queryAllByLabelText("External workspace");
        expect(externalIndicators.length).toBeGreaterThan(0);
      });
    });

    it("uses detach copy for external repository removal affordances", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const detachButtons = screen.queryAllByLabelText("Detach workspace-3");
        expect(detachButtons.length).toBeGreaterThan(0);
      });

      const detachButtons = screen.getAllByLabelText("Detach workspace-3");
      await user.click(detachButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Detach workspace")).toBeTruthy();
        expect(screen.getByText(/This will remove 'workspace-3' from the SGAI workspace list\./)).toBeTruthy();
        expect(screen.getByRole("button", { name: /^Detach$/ })).toBeTruthy();
      });
    });

    it("routes the [ + ] button to the external attachment flow", async () => {
      const user = userEvent.setup();
      renderDashboard("/workspaces/workspace-2/messages");

      await waitFor(() => {
        expect(screen.getByTestId("route-path").textContent).toBe("/workspaces/workspace-2/messages");
      });

      await user.click(screen.getByRole("button", { name: "Attach external repository" }));

      await waitFor(() => {
        expect(screen.getByTestId("attach-page").textContent).toBe("Attach page");
      });
    });

    it("renders the [ + ] button as an external-only action", async () => {
      renderDashboard();

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Attach external repository" })).toBeTruthy();
      });

      expect(screen.getByText("Attach External")).toBeTruthy();
      expect(screen.queryByText("New Workspace")).toBeNull();
    });
  });

  describe("repository tree structure", () => {
    it("nests forks under their root repositories", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const ws2Elements = screen.queryAllByText("Workspace Two Title");
        expect(ws2Elements.length).toBeGreaterThan(0);
      });

      const expandButtons = screen.getAllByRole("button", { name: /forks for /i });
      await user.click(expandButtons[0]);

      await waitFor(() => {
        const forkElements = screen.queryAllByText("Workspace Two Fork Title");
        expect(forkElements.length).toBeGreaterThan(0);
      });
    });

    it("shows correct hierarchy for root and fork workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const ws2Elements = screen.queryAllByText("Workspace Two Title");
        expect(ws2Elements.length).toBeGreaterThan(0);
      });
    });
  });

  describe("pinned fork with unpinned root", () => {
    it("uses computedTitle for sidebar labels, including in-progress items, and avoids orphan pinned double-prefixes", async () => {
      const user = userEvent.setup();
      const workspaceWithForks = mockWorkspaces.find((workspace) => workspace.name === "workspace-2");
      const unpinnedRoot = mockWorkspaces.find((workspace) => workspace.name === "root-unpinned");
      const orphanPinnedFork = mockWorkspaces.find((workspace) => workspace.name === "orphan-pinned-fork");
      const inProgressWorkspace = mockWorkspaces.find((workspace) => workspace.name === "workspace-3");

      if (!workspaceWithForks || !workspaceWithForks.forks?.[0] || !unpinnedRoot || !unpinnedRoot.forks?.[0] || !orphanPinnedFork || !inProgressWorkspace) {
        throw new Error("Expected dashboard fixture workspaces are missing");
      }

      const originalWorkspaceWithForks = {
        title: workspaceWithForks.title,
        computedTitle: workspaceWithForks.computedTitle,
        forkTitle: workspaceWithForks.forks[0].title,
        forkComputedTitle: workspaceWithForks.forks[0].computedTitle,
      };
      const originalUnpinnedRoot = {
        title: unpinnedRoot.title,
        computedTitle: unpinnedRoot.computedTitle,
        forkTitle: unpinnedRoot.forks[0].title,
        forkComputedTitle: unpinnedRoot.forks[0].computedTitle,
      };
      const originalOrphanPinnedFork = {
        title: orphanPinnedFork.title,
        computedTitle: orphanPinnedFork.computedTitle,
      };
      const originalInProgressWorkspace = {
        title: inProgressWorkspace.title,
        computedTitle: inProgressWorkspace.computedTitle,
      };

      try {
        workspaceWithForks.title = "Legacy Root Title";
        workspaceWithForks.computedTitle = "workspace-2";
        workspaceWithForks.forks[0].title = "Legacy Nested Fork Title";
        workspaceWithForks.forks[0].computedTitle = "workspace-2/Nested Fork Title";

        unpinnedRoot.title = "Legacy Unpinned Root Title";
        unpinnedRoot.computedTitle = "root-unpinned";
        unpinnedRoot.forks[0].title = "Legacy Orphan Fork Title";
        unpinnedRoot.forks[0].computedTitle = "root-unpinned/Orphan Pinned Fork Title";

        orphanPinnedFork.title = "Legacy Orphan Fork Title";
        orphanPinnedFork.computedTitle = "root-unpinned/Orphan Pinned Fork Title";

        inProgressWorkspace.title = "Legacy In Progress Title";
        inProgressWorkspace.computedTitle = "Computed In Progress Title";

        renderDashboard();

        await waitFor(() => {
          const inProgressRegions = screen.queryAllByRole("region", { name: "In progress" });
          expect(inProgressRegions.length).toBeGreaterThan(0);
        });

        const inProgressRegion = screen.getAllByRole("region", { name: "In progress" })[0];

        await waitFor(() => {
          expect(within(inProgressRegion).getByText("Computed In Progress Title")).toBeTruthy();
        });

        expect(within(inProgressRegion).queryByText("Legacy In Progress Title")).toBeNull();

        await waitFor(() => {
          const rootLabels = screen.queryAllByText("root-unpinned");
          expect(rootLabels.length).toBeGreaterThan(0);
        });

        const expandButtons = screen.getAllByRole("button", { name: /forks for workspace-2/i });
        await user.click(expandButtons[0]);

        await waitFor(() => {
          const nestedForkLabels = screen.queryAllByText("workspace-2/Nested Fork Title");
          expect(nestedForkLabels.length).toBeGreaterThan(0);
        });

        await waitFor(() => {
          const orphanForkLabels = screen.queryAllByText("root-unpinned/Orphan Pinned Fork Title");
          expect(orphanForkLabels.length).toBeGreaterThan(0);
        });

        expect(screen.queryByText("root-unpinned/root-unpinned/Orphan Pinned Fork Title")).toBeNull();
        expect(screen.queryByText("Legacy Root Title")).toBeNull();
        expect(screen.queryByText("Legacy Nested Fork Title")).toBeNull();
        expect(screen.queryByText("Legacy Unpinned Root Title/Legacy Orphan Fork Title")).toBeNull();
      } finally {
        workspaceWithForks.title = originalWorkspaceWithForks.title;
        workspaceWithForks.computedTitle = originalWorkspaceWithForks.computedTitle;
        workspaceWithForks.forks[0].title = originalWorkspaceWithForks.forkTitle;
        workspaceWithForks.forks[0].computedTitle = originalWorkspaceWithForks.forkComputedTitle;

        unpinnedRoot.title = originalUnpinnedRoot.title;
        unpinnedRoot.computedTitle = originalUnpinnedRoot.computedTitle;
        unpinnedRoot.forks[0].title = originalUnpinnedRoot.forkTitle;
        unpinnedRoot.forks[0].computedTitle = originalUnpinnedRoot.forkComputedTitle;

        orphanPinnedFork.title = originalOrphanPinnedFork.title;
        orphanPinnedFork.computedTitle = originalOrphanPinnedFork.computedTitle;

        inProgressWorkspace.title = originalInProgressWorkspace.title;
        inProgressWorkspace.computedTitle = originalInProgressWorkspace.computedTitle;
      }
    });

    it("shows pinned forks with root title and fork title when root is not pinned", async () => {
      renderDashboard();

      await waitFor(() => {
        const pinnedSection = screen.queryAllByRole("region", { name: "Pinned" });
        expect(pinnedSection.length).toBeGreaterThan(0);
      });

      await waitFor(() => {
        const orphanFork = screen.queryAllByText("Unpinned Root Title/Orphan Pinned Fork Title");
        expect(orphanFork.length).toBeGreaterThan(0);
      });
    });

    it("shows tooltip with fork name and root name for orphan pinned forks", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const orphanFork = screen.queryAllByText("Unpinned Root Title/Orphan Pinned Fork Title");
        expect(orphanFork.length).toBeGreaterThan(0);
      });

      const orphanForkLabel = screen.getAllByText("Unpinned Root Title/Orphan Pinned Fork Title")[0];
      await user.hover(orphanForkLabel);

      await waitFor(() => {
        const tooltipForkName = screen.queryAllByText("Name: orphan-pinned-fork");
        expect(tooltipForkName.length).toBeGreaterThan(0);
      });

      await waitFor(() => {
        const tooltipRootName = screen.queryAllByText("Root: Unpinned Root Title");
        expect(tooltipRootName.length).toBeGreaterThan(0);
      });

      const tooltipForkName = screen.getAllByText("Name: orphan-pinned-fork")[0];
      const tooltipRootName = screen.getAllByText("Root: Unpinned Root Title")[0];

      expect(String(tooltipForkName.getAttribute("class") ?? "")).toContain("text-background");
      expect(String(tooltipForkName.getAttribute("class") ?? "")).not.toContain("text-background/80");
      expect(String(tooltipRootName.getAttribute("class") ?? "")).toContain("text-background");
      expect(String(tooltipRootName.getAttribute("class") ?? "")).not.toContain("text-background/80");
    });

    it("shows nested fork tooltip metadata when the sidebar link receives keyboard focus", async () => {
      const user = userEvent.setup();
      renderDashboard();

      const expandForksButton = screen.getByRole("button", {
        name: /expand forks for workspace two title/i,
      });

      await user.click(expandForksButton);

      await waitFor(() => {
        expect(screen.getByText("Workspace Two Fork Title")).toBeTruthy();
      });

      const forkLink = screen.getByRole("link", { name: /workspace two fork title/i });

      expect(forkLink.getAttribute("href")).toBe(
        "/workspaces/workspace-2-fork-1/progress?workspaceDir=%2Fpath%2Fto%2Fworkspace-2-fork-1"
      );
      act(() => {
        forkLink.focus();
      });
      expect(document.activeElement).toBe(forkLink);

      await waitFor(() => {
        const tooltipForkName = screen.queryAllByText("Name: workspace-2-fork-1");
        expect(tooltipForkName.length).toBeGreaterThan(0);
      });
    });
  });

  describe("fork deletion redirect", () => {
    it("keeps fork action affordances on nested fork rows", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const expandButtons = screen.queryAllByRole("button", { name: /forks for /i });
        expect(expandButtons.length).toBeGreaterThan(0);
      });

      const expandButtons = screen.getAllByRole("button", { name: /forks for /i });
      await user.click(expandButtons[0]);

      await waitFor(() => {
        const forkElements = screen.queryAllByText("Workspace Two Fork Title");
        expect(forkElements.length).toBeGreaterThan(0);
      });

      const forkActionButtons = screen.getAllByLabelText(/Choose action for fork workspace-2-fork-1/);
      await user.click(forkActionButtons[0]);

      await waitFor(() => {
        const confirmButtons = screen.queryAllByRole("button", { name: /^Delete$/ });
        expect(confirmButtons.length).toBeGreaterThan(0);
        const detachButtons = screen.queryAllByRole("button", { name: /^Detach$/ });
        expect(detachButtons.length).toBeGreaterThan(0);
      });

      const dialogs = screen.getAllByRole("alertdialog");
      const dialog = dialogs[dialogs.length - 1];
      expect(within(dialog).getByRole("button", { name: /^Delete$/ })).toBeTruthy();
      expect(within(dialog).getByRole("button", { name: /^Detach$/ })).toBeTruthy();
      expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeTruthy();
    });
  });
});
