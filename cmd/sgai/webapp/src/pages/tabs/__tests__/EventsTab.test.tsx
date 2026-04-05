import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { api } from "@/lib/api";
import * as markdownContentModule from "@/components/MarkdownContent";
import * as useAdhocRunModule from "@/hooks/useAdhocRun";
import { EventsTab } from "../EventsTab";

const createAction = (overrides = {}) => ({
  name: "Run Tests",
  kind: "prompt",
  model: "model-1",
  prompt: "run tests",
  variables: [] as string[],
  description: "Run test suite",
  ...overrides,
});

beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

const createMockWorkspace = (overrides = {}) => ({
  name: "test-workspace",
  dir: "/path/to/test-workspace",
  running: false,
  needsInput: false,
  inProgress: false,
  pinned: false,
  isRoot: false,
  isFork: false,
  description: "Test Workspace",
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
  svgHash: "abc123",
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
  ...overrides,
});

let mockWorkspaces = [createMockWorkspace()];
const mockActionRun = mock(() => Promise.resolve({ output: "", running: false }));
const mockStartActionRun = mock(() => Promise.resolve());
const mockAdhoc = mock(() => Promise.resolve({ output: "", running: false }));
const mockAdhocStatus = mock(() => Promise.resolve({ output: "", running: false }));
const mockAdhocStop = mock(() => Promise.resolve({ output: "", running: false }));
const mockModelsList = mock(() => Promise.resolve({ models: [], defaultModel: "" }));

const mockMarkdownContent = ({ content }: { content: string }) => (
  <div data-testid="markdown-content">{content}</div>
);

const mockUseAdhocRun = () => ({
    output: "",
    isRunning: false,
    runError: null,
    startActionRun: mockStartActionRun,
    stopRun: mock(() => {}),
    outputRef: { current: null },
    models: null,
    modelsLoading: false,
    modelsError: null,
    selectedModel: "",
    setSelectedModel: mock(() => {}),
    prompt: "",
    setPrompt: mock(() => {}),
    startRun: mock(() => {}),
    handleSubmit: mock(() => {}),
    handleKeyDown: mock(() => {}),
    promptHistory: [],
    selectFromHistory: mock(() => {}),
    clearHistory: mock(() => {}),
  });

function renderEventsTab(props = {}) {
  const workspace = mockWorkspaces[0] ?? createMockWorkspace();
  const defaultProps = {
    workspaceName: workspace.name,
    svgHash: workspace.svgHash,
    agentModels: workspace.agentModels,
    modelStatuses: workspace.modelStatuses,
    needsInput: workspace.needsInput,
    humanMessage: workspace.humanMessage,
    currentAgent: workspace.currentAgent,
    events: workspace.events,
    goalContent: workspace.goalContent || undefined,
    actions: undefined as any[] | undefined,
    ...props,
  };

  return render(
    <MemoryRouter>
      <TooltipProvider>
        <EventsTab {...defaultProps} />
      </TooltipProvider>
    </MemoryRouter>
  );
}

afterEach(() => {
  mock.restore();
  cleanup();
});

describe("EventsTab", () => {
  beforeEach(() => {
    mockWorkspaces = [createMockWorkspace()];
    mockActionRun.mockClear();
    mockStartActionRun.mockClear();
    mockAdhoc.mockClear();
    mockAdhocStatus.mockClear();
    mockAdhocStop.mockClear();
    mockModelsList.mockClear();

    spyOn(api.workspaces, "adhoc").mockImplementation((...args) => mockAdhoc(...args));
    spyOn(api.workspaces, "actionRun").mockImplementation((...args) => mockActionRun(...args));
    spyOn(api.workspaces, "adhocStatus").mockImplementation((...args) => mockAdhocStatus(...args));
    spyOn(api.workspaces, "adhocStop").mockImplementation((...args) => mockAdhocStop(...args));
    spyOn(api.models, "list").mockImplementation((...args) => mockModelsList(...args));
    spyOn(markdownContentModule, "MarkdownContent").mockImplementation((...args) => mockMarkdownContent(...args));
    spyOn(useAdhocRunModule, "useAdhocRun").mockImplementation((...args) => mockUseAdhocRun(...args));
  });

  describe("event rendering", () => {
    it("shows empty events message when no events", async () => {
      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("No events recorded yet")).toBeTruthy();
      });
    });

    it("displays events timeline", async () => {
      mockWorkspaces = [createMockWorkspace({
        events: [
          {
            timestamp: "2026-03-05T10:00:00Z",
            agent: "coordinator",
            description: "Started workspace",
            formattedTime: "10:00 AM",
            showDateDivider: true,
            dateDivider: "Mar 5, 2026",
          },
          {
            timestamp: "2026-03-05T10:05:00Z",
            agent: "developer",
            description: "Writing tests",
            formattedTime: "10:05 AM",
            showDateDivider: false,
            dateDivider: "",
          },
        ],
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("Started workspace")).toBeTruthy();
        expect(screen.getByText("Writing tests")).toBeTruthy();
      });
    });

    it("shows date dividers", async () => {
      mockWorkspaces = [createMockWorkspace({
        events: [
          {
            timestamp: "2026-03-05T10:00:00Z",
            agent: "coordinator",
            description: "Event 1",
            formattedTime: "10:00 AM",
            showDateDivider: true,
            dateDivider: "Mar 5, 2026",
          },
        ],
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("Mar 5, 2026")).toBeTruthy();
      });
    });

    it("shows agent badge for events", async () => {
      mockWorkspaces = [createMockWorkspace({
        events: [
          {
            timestamp: "2026-03-05T10:00:00Z",
            agent: "coordinator",
            description: "Event 1",
            formattedTime: "10:00 AM",
            showDateDivider: false,
            dateDivider: "",
          },
        ],
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("coordinator")).toBeTruthy();
      });
    });

    it("shows formatted time for events", async () => {
      mockWorkspaces = [createMockWorkspace({
        events: [
          {
            timestamp: "2026-03-05T10:00:00Z",
            agent: "test-agent",
            description: "Event 1",
            formattedTime: "10:00 AM",
            showDateDivider: false,
            dateDivider: "",
          },
        ],
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("10:00 AM")).toBeTruthy();
      });
    });
  });

  describe("workflow section", () => {
    it("renders workflow graph image", async () => {
      renderEventsTab();

      await waitFor(() => {
        const img = screen.getByAltText("Workflow graph");
        expect(img).toBeTruthy();
        expect(img.getAttribute("src")).toContain("workflow.svg");
      });
    });

    it("shows agent models table when available", async () => {
      mockWorkspaces = [createMockWorkspace({
        agentModels: [
          { agent: "coordinator", models: ["opencode/glm-5"] },
          { agent: "developer", models: ["anthropic/claude-opus-4-6"] },
        ],
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("Agent")).toBeTruthy();
        expect(screen.getByText("Model(s)")).toBeTruthy();
      });
    });

    it("shows model status list when available", async () => {
      mockWorkspaces = [createMockWorkspace({
        modelStatuses: [
          { modelId: "opencode/glm-5", status: "model-working" },
        ],
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("Model Consensus:")).toBeTruthy();
      });
    });

    it("shows human message when workspace needs input", async () => {
      mockWorkspaces = [createMockWorkspace({
        needsInput: true,
        humanMessage: "Please choose an option",
        currentAgent: "coordinator",
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("Please choose an option")).toBeTruthy();
      });
    });

    it("shows separate active-agent badges when currentAgent encodes multiple agents", async () => {
      mockWorkspaces = [createMockWorkspace({
        needsInput: true,
        humanMessage: "Please choose an option",
        currentAgent: "go-developer, react-developer",
      })];

      renderEventsTab();

      await waitFor(() => {
        expect(screen.getByText("go-developer")).toBeTruthy();
        expect(screen.getByText("react-developer")).toBeTruthy();
      });
    });

    it("prefers normalized activeAgents when provided", async () => {
      mockWorkspaces = [createMockWorkspace({
        needsInput: true,
        humanMessage: "Please choose an option",
        currentAgent: "coordinator",
      })];

      renderEventsTab({ activeAgents: ["go-developer", "react-developer"] });

      await waitFor(() => {
        expect(screen.getByText("go-developer")).toBeTruthy();
        expect(screen.getByText("react-developer")).toBeTruthy();
        expect(screen.queryByText(/^coordinator$/)).toBeNull();
      });
    });

    it("hides the workflow graph when a dir-qualified duplicate route is active", async () => {
      renderEventsTab({ dirQualifiedRoute: true, svgHash: "abc123" });

      await waitFor(() => {
        expect(screen.queryByAltText("Workflow graph")).toBeNull();
        expect(screen.getByText("Workflow graph unavailable for duplicate-name workspace routes.")).toBeTruthy();
      });
    });
  });

  describe("goal content section", () => {
    it("shows GOAL.md section when goal content is provided", () => {
      renderEventsTab({ goalContent: "# My Goal" });
      expect(screen.getByText("GOAL.md")).toBeTruthy();
    });
  });

  describe("empty state", () => {
    it("shows the empty events state when no event data is provided", () => {
      mockWorkspaces = [];

      renderEventsTab({ workspaceName: "nonexistent" });
      expect(screen.getByText("No events recorded yet")).toBeTruthy();
    });
  });

  describe("action buttons", () => {
    it("runs no-variable actions through the named action endpoint", async () => {
      const user = userEvent.setup();

      renderEventsTab({ actions: [createAction()] });

      await user.click(screen.getByRole("button", { name: "Run Tests" }));

      await waitFor(() => {
        expect(mockStartActionRun).toHaveBeenCalledWith("Run Tests", {});
      });
    });

    it("disables named actions on dir-qualified duplicate routes", async () => {
      renderEventsTab({ actions: [createAction()], dirQualifiedRoute: true });

      await waitFor(() => {
        expect(screen.getByText("Action buttons are unavailable on duplicate-name workspace routes because the current API still targets workspaces by basename only.")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Run Tests" }).hasAttribute("disabled")).toBe(true);
      });
    });
  });
});
