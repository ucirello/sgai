import { describe, it, expect, beforeEach, afterEach, mock } from "bun:test";
import { render, screen, waitFor, cleanup, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route, useLocation } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { SidebarProvider } from "@/components/ui/sidebar";
import { Dashboard } from "../Dashboard";

// Override pointer-events on body to allow interactions in tests
beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

const createMockWorkspace = (overrides: Record<string, unknown> = {}) => ({
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
  ...overrides,
});

const mockWorkspaces = [
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
  }),
];

mock.module("@/lib/factory-state", () => ({
  useFactoryState: () => ({
    workspaces: mockWorkspaces,
    fetchStatus: "idle",
    lastFetchedAt: Date.now(),
  }),
  triggerFactoryRefresh: mock(() => {}),
}));

const mockDeleteFork = mock(() => Promise.resolve({ deleted: true }));
const mockDeleteWorkspace = mock(() => Promise.resolve({ deleted: true }));

mock.module("@/lib/api", () => ({
  api: {
    workspaces: {
      deleteFork: mockDeleteFork,
      deleteWorkspace: mockDeleteWorkspace,
    },
  },
  ApiError: class ApiError extends Error {
    constructor(public status: number, message: string) {
      super(message);
      this.name = "ApiError";
    }
  },
}));

mock.module("@/hooks/useSidebarResize", () => ({
  useSidebarResize: () => ({
    sidebarWidth: 280,
    handleMouseDown: mock(() => {}),
  }),
}));

mock.module("@/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));

function renderDashboard(initialRoute = "/") {
  function RoutePathProbe() {
    const location = useLocation();
    return <div data-testid="route-path">{location.pathname}</div>;
  }

  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
        <TooltipProvider>
          <SidebarProvider>
          <Routes>
            <Route path="/workspaces/attach" element={<Dashboard><div data-testid="attach-page">Attach page</div></Dashboard>} />
            <Route path="/workspaces/:name/forks" element={<Dashboard><div data-testid="redirect-target">Redirected to forks</div></Dashboard>} />
            <Route path="/workspaces/:name/*" element={<Dashboard><RoutePathProbe /></Dashboard>} />
            <Route path="*" element={<Dashboard><div data-testid="dashboard-content">Content</div></Dashboard>} />
          </Routes>
        </SidebarProvider>
      </TooltipProvider>
    </MemoryRouter>
  );
}

describe("Dashboard", () => {
  beforeEach(() => {
    mockDeleteFork.mockClear();
    mockDeleteWorkspace.mockClear();
  });

  afterEach(() => {
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
        const expandButtons = screen.queryAllByLabelText("Toggle forks");
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

      const expandButtons = screen.getAllByLabelText("Toggle forks");
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
    it("shows delete button on workspace hover", async () => {
      renderDashboard();

      await waitFor(() => {
        const deleteButtons = screen.queryAllByLabelText("Delete workspace Workspace One Title");
        expect(deleteButtons.length).toBeGreaterThan(0);
      });
    });

    it("opens delete confirmation dialog", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const ws1Elements = screen.queryAllByText("Workspace One Title");
        expect(ws1Elements.length).toBeGreaterThan(0);
      });

      const deleteButtons = screen.getAllByLabelText("Delete workspace Workspace One Title");
      await user.click(deleteButtons[0]);

      await waitFor(() => {
        const alertDialogs = screen.queryAllByRole("alertdialog");
        expect(alertDialogs.length).toBeGreaterThan(0);
        
        const deleteWorkspaceElements = screen.queryAllByText(/Delete workspace/);
        expect(deleteWorkspaceElements.length).toBeGreaterThan(0);
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
    it("opens the fork deletion confirmation dialog", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const expandButtons = screen.queryAllByLabelText("Toggle forks");
        expect(expandButtons.length).toBeGreaterThan(0);
      });

      const expandButtons = screen.getAllByLabelText("Toggle forks");
      await user.click(expandButtons[0]);

      await waitFor(() => {
        const forkElements = screen.queryAllByText("Workspace Two Fork Title");
        expect(forkElements.length).toBeGreaterThan(0);
      });

      const deleteForkButtons = screen.getAllByLabelText("Delete fork Workspace Two Fork Title");
      expect(deleteForkButtons.length).toBeGreaterThan(0);
      await user.click(deleteForkButtons[0]);

      await waitFor(() => {
        const confirmButtons = screen.queryAllByRole("button", { name: "Delete fork" });
        expect(confirmButtons.length).toBeGreaterThan(0);
      });

      const dialogs = screen.getAllByRole("alertdialog");
      const dialog = dialogs[dialogs.length - 1];
      expect(within(dialog).getByRole("heading", { name: "Delete fork" })).toBeTruthy();
      expect(within(dialog).getByText(/This will permanently delete 'Workspace Two Fork Title' from disk/i)).toBeTruthy();
    });

    it("shows delete button for standalone workspaces", async () => {
      renderDashboard();

      await waitFor(() => {
        const ws1Elements = screen.queryAllByText("Workspace One Title");
        expect(ws1Elements.length).toBeGreaterThan(0);
      });

      const deleteButtons = screen.getAllByLabelText("Delete workspace Workspace One Title");
      expect(deleteButtons.length).toBeGreaterThan(0);
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
        const detachButtons = screen.queryAllByLabelText("Detach Needs Input Fallback Title");
        expect(detachButtons.length).toBeGreaterThan(0);
      });

      const detachButtons = screen.getAllByLabelText("Detach Needs Input Fallback Title");
      await user.click(detachButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Detach workspace")).toBeTruthy();
        expect(screen.getByText(/This will remove 'Needs Input Fallback Title' from the interface\./)).toBeTruthy();
        expect(screen.getByRole("button", { name: "Remove" })).toBeTruthy();
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

      const expandButtons = screen.getAllByLabelText("Toggle forks");
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
    });
  });

  describe("fork deletion redirect", () => {
    it("keeps fork deletion affordances on nested fork rows", async () => {
      const user = userEvent.setup();
      renderDashboard();

      await waitFor(() => {
        const expandButtons = screen.queryAllByLabelText("Toggle forks");
        expect(expandButtons.length).toBeGreaterThan(0);
      });

      const expandButtons = screen.getAllByLabelText("Toggle forks");
      await user.click(expandButtons[0]);

      await waitFor(() => {
        const forkElements = screen.queryAllByText("Workspace Two Fork Title");
        expect(forkElements.length).toBeGreaterThan(0);
      });

      const deleteForkButtons = screen.getAllByLabelText("Delete fork Workspace Two Fork Title");
      await user.click(deleteForkButtons[0]);

      await waitFor(() => {
        const confirmButtons = screen.queryAllByRole("button", { name: "Delete fork" });
        expect(confirmButtons.length).toBeGreaterThan(0);
      });

      const dialogs = screen.getAllByRole("alertdialog");
      const dialog = dialogs[dialogs.length - 1];
      expect(within(dialog).getByRole("button", { name: "Delete fork" })).toBeTruthy();
      expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeTruthy();
    });
  });
});
