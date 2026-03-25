import { describe, it, expect, beforeEach, afterEach, mock, spyOn, vi } from "bun:test";
import { StrictMode, useEffect, useState } from "react";
import { act, render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route, RouterProvider, createMemoryRouter, useNavigate } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { SidebarProvider } from "@/components/ui/sidebar";
import * as factoryStateModule from "@/lib/factory-state";
import * as workspacePageStateModule from "@/lib/workspace-page-state";
import { api } from "@/lib/api";
import * as markdownEditorModule from "@/components/MarkdownEditor";
import * as useAdhocRunModule from "@/hooks/useAdhocRun";
import * as mobileModule from "@/hooks/use-mobile";
import { EditGoal } from "../EditGoal";
import { WorkspaceDetail } from "../WorkspaceDetail";

const mockWorkspace = {
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
  goalContent: "# Test Goal\n\nThis is a test goal.",
  rawGoalContent: "# Test Goal\n\nThis is a test goal.",
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
};

let mockWorkspaces = [mockWorkspace];
let mockFetchStatus: "idle" | "fetching" | "error" = "idle";
let mockLastFetchedAt: number | null = 1;

const mockGetGoal = mock(() => Promise.resolve({ content: "# Test Goal\n\nThis is a test goal." }));
const mockUpdateGoal = mock(() => Promise.resolve({ updated: true, workspace: "test-workspace" }));
const mockTriggerFactoryRefresh = mock(() => {});

const mockMarkdownEditor = ({
  value,
  onChange,
  disabled,
  onSubmitShortcut,
}: {
  value: string;
  onChange: (v: string) => void;
  disabled: boolean;
  onSubmitShortcut?: () => void;
}) => (
  <div data-testid="markdown-editor">
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
          e.preventDefault();
          if (!disabled) {
            onSubmitShortcut?.();
          }
        }
      }}
      disabled={disabled}
      data-testid="markdown-textarea"
    />
  </div>
);

const mockUseAdhocRun = () => ({
  output: "",
  isRunning: false,
  runError: null,
  startRun: mock(() => {}),
  startActionRun: mock(() => {}),
  stopRun: mock(() => {}),
  outputRef: { current: null },
});

function createEditGoalTree(workspaceName = "test-workspace", strictMode = false) {
  const tree = (
    <MemoryRouter initialEntries={[`/workspaces/${encodeURIComponent(workspaceName)}/goal/edit`]}>
      <TooltipProvider>
        <Routes>
          <Route path="/workspaces/:name/goal/edit" element={<EditGoal />} />
        </Routes>
      </TooltipProvider>
    </MemoryRouter>
  );

  return strictMode ? <StrictMode>{tree}</StrictMode> : tree;
}

function renderEditGoal(workspaceName = "test-workspace", strictMode = false) {
  return render(createEditGoalTree(workspaceName, strictMode));
}

function renderEditGoalIntegrationRouter(workspaceName = "test-workspace") {
  const workspaceDetailElement = (
    <TooltipProvider>
      <SidebarProvider>
        <WorkspaceDetail />
      </SidebarProvider>
    </TooltipProvider>
  );

  const router = createMemoryRouter([
    {
      path: "/workspaces/:name/goal/edit",
      element: (
        <TooltipProvider>
          <EditGoal />
        </TooltipProvider>
      ),
    },
    {
      path: "/workspaces/:name",
      element: workspaceDetailElement,
    },
    {
      path: "/workspaces/:name/*",
      element: workspaceDetailElement,
    },
  ], {
    initialEntries: [`/workspaces/${encodeURIComponent(workspaceName)}/goal/edit`],
  });

  return {
    router,
    ...render(<RouterProvider router={router} />),
  };
}

function getSaveButton() {
  const buttons = screen.getAllByRole("button", { name: /Save GOAL\.md|Saving\.\.\.|Saved!/ });
  return buttons[buttons.length - 1];
}

async function waitForContentToLoad() {
  await waitFor(() => {
    const textareas = screen.queryAllByTestId("markdown-textarea");
    expect(textareas.length).toBeGreaterThan(0);
    const textarea = textareas[0] as HTMLTextAreaElement;
    expect(textarea.value).toBeTruthy();
    expect(textarea.value).toContain("# Test Goal");
  });
}

function getFactoryRefreshCallCount() {
  return mockTriggerFactoryRefresh.mock.calls.length;
}

async function waitForSaveToComplete(previousRefreshCallCount = 0) {
  await waitFor(() => {
    expect(getFactoryRefreshCallCount()).toBeGreaterThan(previousRefreshCallCount);
    expect(screen.getByRole("button", { name: "Saved!" })).toBeTruthy();
  });
}

async function advanceSaveRedirect() {
  await act(async () => {
    vi.advanceTimersByTime(1000);
    await Promise.resolve();
  });
}

async function advanceNextTimerTurn() {
  await act(async () => {
    vi.advanceTimersByTime(0);
    await Promise.resolve();
  });
}

async function waitForWorkspaceDetailRedirect(
  router: ReturnType<typeof createMemoryRouter>,
  expectedPath: string,
  expectedTitle: string,
) {
  await waitFor(() => {
    expect(router.state.location.pathname).toBe(expectedPath);
    expect(router.state.location.search).toBe("");
    expect(screen.getByText(expectedTitle)).toBeTruthy();
  });
}

describe("EditGoal", () => {
  beforeEach(() => {
    mockWorkspaces = [mockWorkspace];
    mockFetchStatus = "idle";
    mockLastFetchedAt = 1;
    mockGetGoal.mockClear();
    mockUpdateGoal.mockClear();
    mockTriggerFactoryRefresh.mockClear();

    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: mockWorkspaces,
      fetchStatus: mockFetchStatus,
      lastFetchedAt: mockLastFetchedAt,
    }));
    spyOn(factoryStateModule, "getFactoryStateSnapshot").mockImplementation(() => ({
      workspaces: mockWorkspaces,
      fetchStatus: mockFetchStatus,
      lastFetchedAt: mockLastFetchedAt,
    }));
    spyOn(factoryStateModule, "triggerFactoryRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(workspacePageStateModule, "useWorkspacePageState").mockImplementation((workspaceName: string) => ({
      workspace: mockWorkspaces.find((workspace) => workspace.name === workspaceName) ?? null,
      fetchStatus: mockFetchStatus,
      lastFetchedAt: mockLastFetchedAt,
    }));
    spyOn(api.workspaces, "getGoal").mockImplementation((...args) => mockGetGoal(...args));
    spyOn(api.workspaces, "updateGoal").mockImplementation((...args) => mockUpdateGoal(...args));
    spyOn(markdownEditorModule, "MarkdownEditor").mockImplementation((...args) => mockMarkdownEditor(...args));
    spyOn(useAdhocRunModule, "useAdhocRun").mockImplementation((...args) => mockUseAdhocRun(...args));
    spyOn(mobileModule, "useIsMobile").mockImplementation(() => false);
  });

  afterEach(() => {
    mock.restore();
    vi.useRealTimers();
    cleanup();
  });

  describe("save/load functionality", () => {
    it("loads goal content on mount", async () => {
      renderEditGoal();

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledWith("test-workspace");
      });
    });

    it("loads goal content from the route even when workspace metadata is unavailable", async () => {
      mockWorkspaces = [];

      renderEditGoal();

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledWith("test-workspace");
      });

      await waitFor(() => {
        expect((screen.getByTestId("markdown-textarea") as HTMLTextAreaElement).value).toContain("# Test Goal");
      });
    });

    it("saves goal content when Save button is clicked", async () => {
      const user = userEvent.setup();

      renderEditGoal();

      await waitFor(() => {
        expect(getSaveButton()).toBeTruthy();
      });

      await user.click(getSaveButton());

      await waitFor(() => {
        expect(mockUpdateGoal).toHaveBeenCalled();
      });
    });

    it("keeps unsaved editor content and avoids refetching GOAL.md when workspace state refreshes", async () => {
      const view = renderEditGoal();

      await waitForContentToLoad();
      expect(mockGetGoal).toHaveBeenCalledTimes(1);

      const textarea = screen.getByTestId("markdown-textarea") as HTMLTextAreaElement;
      fireEvent.change(textarea, { target: { value: "# Local Draft\n\nDo not overwrite this." } });
      expect(textarea.value).toBe("# Local Draft\n\nDo not overwrite this.");

      mockFetchStatus = "fetching";
      view.rerender(createEditGoalTree());

      mockFetchStatus = "idle";
      mockLastFetchedAt = 2;
      view.rerender(createEditGoalTree());

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledTimes(1);
        expect((screen.getByTestId("markdown-textarea") as HTMLTextAreaElement).value).toBe("# Local Draft\n\nDo not overwrite this.");
      });
    });

    it("completes the initial GOAL load when factory state refreshes before the request resolves", async () => {
      let resolveGoal: ((value: { content: string }) => void) | null = null;
      mockGetGoal.mockImplementationOnce(() => new Promise((resolve) => {
        resolveGoal = resolve;
      }));
      mockLastFetchedAt = null;

      const view = renderEditGoal();

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledTimes(1);
      });

      mockLastFetchedAt = 2;
      view.rerender(createEditGoalTree());

      resolveGoal?.({ content: "# Loaded After Refresh" });

      await waitFor(() => {
        expect((screen.getByTestId("markdown-textarea") as HTMLTextAreaElement).value).toBe("# Loaded After Refresh");
      });
    });

    it("completes the initial GOAL load in StrictMode without getting stuck in the loading skeleton", async () => {
      renderEditGoal("test-workspace", true);

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledTimes(1);
      });

      await waitFor(() => {
        expect((screen.getByTestId("markdown-textarea") as HTMLTextAreaElement).value).toContain("# Test Goal");
      });
    });

    it("rejects ambiguous duplicate-basename goal-edit routes instead of loading a specific workspace", async () => {
      mockWorkspaces = [
        {
          ...mockWorkspace,
          name: "shared-ws",
          dir: "/tmp/first/shared-ws",
          title: "First Shared Workspace",
        },
        {
          ...mockWorkspace,
          name: "shared-ws",
          dir: "/tmp/second/shared-ws",
          title: "Second Shared Workspace",
        },
      ];

      renderEditGoal("shared-ws");

      await waitFor(() => {
        expect(screen.getByText(/ambiguous/i)).toBeTruthy();
      });

      expect(mockGetGoal).not.toHaveBeenCalled();
      expect(mockUpdateGoal).not.toHaveBeenCalled();
      expect(screen.queryByRole("button", { name: /Save GOAL\.md/ })).toBeNull();
      expect(screen.queryByTestId("markdown-editor")).toBeNull();
      expect(screen.getByRole("link", { name: "Back to Workspaces" }).getAttribute("href")).toBe("/");

      await waitFor(() => {
        expect(screen.getByTestId("edit-goal-route-error")).toBe(document.activeElement);
      });
    });

    it("keeps ambiguous duplicate-basename goal-edit routes on the error page in the router", async () => {
      mockWorkspaces = [
        {
          ...mockWorkspace,
          name: "shared-ws",
          dir: "/tmp/first/shared-ws",
          title: "First Shared Workspace",
        },
        {
          ...mockWorkspace,
          name: "shared-ws",
          dir: "/tmp/second/shared-ws",
          title: "Second Shared Workspace",
        },
      ];

      const { router } = renderEditGoalIntegrationRouter("shared-ws");

      await waitFor(() => {
        expect(screen.getByText(/ambiguous/i)).toBeTruthy();
      });

      expect(router.state.location.pathname).toBe("/workspaces/shared-ws/goal/edit");
      expect(screen.queryByText("First Shared Workspace")).toBeNull();
      expect(screen.queryByText("Second Shared Workspace")).toBeNull();
      expect(mockGetGoal).not.toHaveBeenCalled();
    });

    it("moves focus to the page heading after the goal loads", async () => {
      renderEditGoal();

      await waitFor(() => {
        const heading = screen.getByRole("heading", { name: "Edit GOAL.md" });
        expect(heading).toBe(document.activeElement);
      });
    });

    it("shows saving state during save", async () => {
      const user = userEvent.setup();
      mockUpdateGoal.mockImplementationOnce(
        () => new Promise((resolve) => setTimeout(() => resolve({ updated: true, workspace: "test-workspace" }), 500)),
      );

      renderEditGoal();

      await waitForContentToLoad();

      await user.click(getSaveButton());

      await waitFor(() => {
        const savingButtons = screen.queryAllByText("Saving...");
        expect(savingButtons.length).toBeGreaterThan(0);
      });
    });

    it("shows success state after save", async () => {
      const user = userEvent.setup();

      renderEditGoal();

      await waitFor(() => {
        expect(getSaveButton()).toBeTruthy();
      });

      await user.click(getSaveButton());

      await waitFor(() => {
        const savedButtons = screen.getAllByRole("button", { name: /Saved!/ });
        expect(savedButtons.length).toBeGreaterThan(0);
      });
    });

    it("triggers factory refresh after save", async () => {
      const user = userEvent.setup();

      renderEditGoal();

      await waitFor(() => {
        expect(getSaveButton()).toBeTruthy();
      });

      await user.click(getSaveButton());

      await waitFor(() => {
        expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
      });
    });

    it("disables save button during saving", async () => {
      const user = userEvent.setup();
      mockUpdateGoal.mockImplementationOnce(
        () => new Promise((resolve) => setTimeout(() => resolve({ updated: true, workspace: "test-workspace" }), 500)),
      );

      renderEditGoal();

      await waitForContentToLoad();

      await user.click(getSaveButton());

      await waitFor(() => {
        const savingButtons = screen.queryAllByText("Saving...");
        expect(savingButtons.length).toBeGreaterThan(0);
      });
    });
  });

  describe("keyboard shortcuts", () => {
    it("saves exactly once on Ctrl+S / Cmd+S inside the editor", async () => {
      renderEditGoal();

      await waitForContentToLoad();

      const textarea = screen.getByTestId("markdown-textarea") as HTMLTextAreaElement;
      textarea.focus();
      fireEvent.keyDown(textarea, { key: "s", ctrlKey: true, bubbles: true });

      await waitFor(() => {
        expect(mockUpdateGoal).toHaveBeenCalledTimes(1);
      });
    });

    it("does not save on Ctrl+S / Cmd+S outside the editor", async () => {
      renderEditGoal();

      await waitForContentToLoad();

      fireEvent.keyDown(window, { key: "s", ctrlKey: true });

      expect(mockUpdateGoal).not.toHaveBeenCalled();
    });
  });

  describe("navigation", () => {
    it("waits for detail navigation that commits on the next fake-timer turn", async () => {
      function DelayedRouteCommit() {
        const navigate = useNavigate();
        const [isCommitQueued, setIsCommitQueued] = useState(false);

        useEffect(() => {
          if (!isCommitQueued) {
            return;
          }

          const timeoutId = setTimeout(() => {
            navigate("/done");
          }, 0);

          return () => clearTimeout(timeoutId);
        }, [isCommitQueued, navigate]);

        return (
          <button
            type="button"
            onClick={() => {
              setTimeout(() => {
                setIsCommitQueued(true);
              }, 1000);
            }}
          >
            Queue redirect
          </button>
        );
      }

      const router = createMemoryRouter([
        {
          path: "/",
          element: <DelayedRouteCommit />,
        },
        {
          path: "/done",
          element: <div>Done</div>,
        },
      ], {
        initialEntries: ["/"],
      });

      render(<RouterProvider router={router} />);

      vi.useFakeTimers();

      fireEvent.click(screen.getByRole("button", { name: "Queue redirect" }));

      await act(async () => {
        vi.advanceTimersByTime(1000);
        await Promise.resolve();
      });

      await advanceNextTimerTurn();

      await waitForWorkspaceDetailRedirect(router, "/done", "Done");
    });

    it("shows back link to workspace", async () => {
      renderEditGoal();

      await waitFor(() => {
        const backLinks = screen.getAllByLabelText("Back to Test Workspace Title");
        expect(backLinks.length).toBeGreaterThan(0);
      });
    });

    it("keeps the back link on the basename detail URL without workspaceDir", async () => {
      renderEditGoal();

      await waitFor(() => {
        const backLink = screen.getByLabelText("Back to Test Workspace Title");
        expect(backLink.getAttribute("href")).toBe("/workspaces/test-workspace");
        expect(backLink.getAttribute("href")).not.toContain("workspaceDir");
      });
    });

    it("redirects after save to the basename detail page without workspaceDir", async () => {
      const { router } = renderEditGoalIntegrationRouter();

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledWith("test-workspace");
      });
      await waitForContentToLoad();

      vi.useFakeTimers();

      const refreshCallCountBeforeSave = getFactoryRefreshCallCount();

      fireEvent.click(getSaveButton());

      await waitFor(() => {
        expect(mockUpdateGoal).toHaveBeenCalledWith("test-workspace", "# Test Goal\n\nThis is a test goal.");
      });

      await waitForSaveToComplete(refreshCallCountBeforeSave);

      await advanceSaveRedirect();
      await advanceNextTimerTurn();

      await waitForWorkspaceDetailRedirect(
        router,
        "/workspaces/test-workspace",
        "Test Workspace Title",
      );
    });

    it("blurs the focused editor before the save redirect runs", async () => {
      const { router } = renderEditGoalIntegrationRouter();

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledWith("test-workspace");
      });
      await waitForContentToLoad();

      vi.useFakeTimers();

      const textarea = screen.getByTestId("markdown-textarea") as HTMLTextAreaElement;
      textarea.focus();
      expect(textarea).toBe(document.activeElement);

      const refreshCallCountBeforeSave = getFactoryRefreshCallCount();

      fireEvent.keyDown(textarea, { key: "s", ctrlKey: true, bubbles: true });

      await waitFor(() => {
        expect(mockUpdateGoal).toHaveBeenCalledWith("test-workspace", "# Test Goal\n\nThis is a test goal.");
      });

      await waitFor(() => {
        expect(textarea).not.toBe(document.activeElement);
      });

      await waitForSaveToComplete(refreshCallCountBeforeSave);

      await advanceSaveRedirect();
      await advanceNextTimerTurn();

      await waitForWorkspaceDetailRedirect(
        router,
        "/workspaces/test-workspace",
        "Test Workspace Title",
      );
    });
  });

  describe("title display", () => {
    it("shows workspace title in header", async () => {
      renderEditGoal();

      await waitFor(() => {
        const titles = screen.getAllByText("Test Workspace Title");
        expect(titles.length).toBeGreaterThan(0);
      });
    });
  });

  describe("validation", () => {
    it("disables save when content is empty", async () => {
      mockGetGoal.mockImplementationOnce(() => Promise.resolve({ content: "" }));

      renderEditGoal();

      await waitFor(() => {
        const saveButton = getSaveButton();
        expect(saveButton).toBeTruthy();
        expect(saveButton.hasAttribute("disabled")).toBe(true);
      });
    });

    it("enables save when content is valid", async () => {
      mockGetGoal.mockImplementationOnce(() => Promise.resolve({ content: "# Test Goal" }));

      renderEditGoal();

      await waitFor(() => {
        const saveButtons = screen.queryAllByRole("button", { name: /Save GOAL\.md/ });
        expect(saveButtons.length).toBeGreaterThan(0);
      });

      const saveButtons = screen.getAllByRole("button", { name: /Save GOAL\.md/ });
      expect(saveButtons[0].hasAttribute("disabled")).toBe(false);
    });
  });

  describe("error handling", () => {
    it("shows error message when save fails", async () => {
      const user = userEvent.setup();
      const errorMessage = "Failed to save GOAL.md";
      mockUpdateGoal.mockImplementationOnce(() => Promise.reject(new Error(errorMessage)));

      renderEditGoal();

      await waitForContentToLoad();

      await user.click(getSaveButton());

      await waitFor(
        () => {
          const errorElements = screen.queryAllByText(/Failed to save GOAL\.md/);
          expect(errorElements.length).toBeGreaterThan(0);
        },
        { timeout: 3000 },
      );
    });

    it("shows error message when load fails", async () => {
      mockGetGoal.mockImplementationOnce(() => Promise.reject(new Error("Failed to load")));

      renderEditGoal();

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalled();
      });

      await waitFor(
        () => {
          const errorElements = screen.queryAllByText(/Failed to load/);
          expect(errorElements.length).toBeGreaterThan(0);
        },
        { timeout: 3000 },
      );
    });

    it("keeps the editor fail-closed after an initial load failure and recovers on retry", async () => {
      const user = userEvent.setup();

      mockGetGoal
        .mockImplementationOnce(() => Promise.reject(new Error("Failed to load")))
        .mockImplementationOnce(() => Promise.resolve({ content: "# Recovered Goal" }));

      renderEditGoal();

      await waitFor(() => {
        expect(screen.getByText("Failed to load GOAL.md")).toBeTruthy();
      });

      expect(screen.queryByRole("button", { name: /Save GOAL\.md/i })).toBeNull();
      expect(screen.queryByTestId("markdown-editor")).toBeNull();

      const retryButton = screen.getByRole("button", { name: /Retry/i });
      await user.click(retryButton);

      await waitFor(() => {
        expect(mockGetGoal).toHaveBeenCalledTimes(2);
      });

      await waitFor(() => {
        expect((screen.getByTestId("markdown-textarea") as HTMLTextAreaElement).value).toBe("# Recovered Goal");
      });

      expect(screen.queryByText("Failed to load GOAL.md")).toBeNull();
      expect(screen.getByRole("button", { name: /Save GOAL\.md/i })).toBeTruthy();
    });
  });

  describe("editor text input", () => {
    it("editor accepts agent name text", async () => {
      renderEditGoal();

      await waitFor(() => {
        const textareas = screen.queryAllByTestId("markdown-textarea");
        expect(textareas.length).toBeGreaterThan(0);
      });

      const textareas = screen.getAllByTestId("markdown-textarea");
      fireEvent.change(textareas[0], { target: { value: '"coordinator"' } });

      await waitFor(() => {
        expect((textareas[0] as HTMLTextAreaElement).value).toBe('"coordinator"');
      });
    });

    it("editor accepts model name text", async () => {
      renderEditGoal();

      await waitFor(() => {
        const textareas = screen.queryAllByTestId("markdown-textarea");
        expect(textareas.length).toBeGreaterThan(0);
      });

      const textareas = screen.getAllByTestId("markdown-textarea");
      fireEvent.change(textareas[0], { target: { value: '"opencode/glm-5"' } });

      await waitFor(() => {
        expect((textareas[0] as HTMLTextAreaElement).value).toBe('"opencode/glm-5"');
      });
    });

    it("editor renders and accepts input", async () => {
      renderEditGoal();

      await waitFor(() => {
        const textareas = screen.queryAllByTestId("markdown-textarea");
        expect(textareas.length).toBeGreaterThan(0);
      });

      const textareas = screen.getAllByTestId("markdown-textarea");
      expect(textareas[0]).toBeTruthy();
    });
  });

  describe("editor renders frontmatter content", () => {
    it("editor renders frontmatter with flow and models", async () => {
      renderEditGoal();

      await waitFor(() => {
        const textareas = screen.queryAllByTestId("markdown-textarea");
        expect(textareas.length).toBeGreaterThan(0);
      });

      const textareas = screen.getAllByTestId("markdown-textarea");
      fireEvent.change(textareas[0], { target: { value: "---\nflow: |\n  \"a\" -> \"b\"\nmodels:\n  \"coordinator\": \"opencode/glm-5\"\n---\n# Goal" } });

      await waitFor(() => {
        expect((textareas[0] as HTMLTextAreaElement).value).toContain("flow:");
      });
    });

    it("editor renders flow syntax content", async () => {
      renderEditGoal();

      await waitFor(() => {
        const textareas = screen.queryAllByTestId("markdown-textarea");
        expect(textareas.length).toBeGreaterThan(0);
      });

      const textareas = screen.getAllByTestId("markdown-textarea");
      fireEvent.change(textareas[0], { target: { value: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal" } });

      await waitFor(() => {
        expect((textareas[0] as HTMLTextAreaElement).value).toContain("flow:");
      });
    });

    it("editor renders models section content", async () => {
      renderEditGoal();

      await waitFor(() => {
        const textareas = screen.queryAllByTestId("markdown-textarea");
        expect(textareas.length).toBeGreaterThan(0);
      });

      const textareas = screen.getAllByTestId("markdown-textarea");
      fireEvent.change(textareas[0], { target: { value: "---\nmodels:\n  \"coordinator\": \"opencode/glm-5\"\n---\n# Goal" } });

      await waitFor(() => {
        expect((textareas[0] as HTMLTextAreaElement).value).toContain("models:");
      });
    });
  });
});
