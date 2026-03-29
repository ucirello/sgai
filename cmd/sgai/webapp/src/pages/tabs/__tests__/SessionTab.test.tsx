import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import * as factoryStateModule from "@/lib/factory-state";
import { api } from "@/lib/api";
import * as markdownContentModule from "@/components/MarkdownContent";
import { SessionTab } from "../SessionTab";

beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

const mockSteer = mock(() => Promise.resolve({ success: true, message: "ok" }));
const mockTriggerFactoryRefresh = mock(() => {});

const createDollarBreakdown = (overrides = {}) => ({
  input: 0,
  output: 0,
  reasoning: 0,
  cacheRead: 0,
  cacheWrite: 0,
  total: 0,
  ...overrides,
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
  svgHash: "",
  totalExecTime: "",
  latestProgress: "",
  humanMessage: "",
  agentSequence: [],
  cost: {
    totalCost: 0,
    dollars: createDollarBreakdown(),
    totalTokens: { input: 0, output: 0, reasoning: 0, cacheRead: 0, cacheWrite: 0 },
    byAgent: [],
  },
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

let mockWorkspaces = [createMockWorkspace()];

const mockMarkdownContent = ({ content }: { content: string }) => (
  <div data-testid="markdown-content">{content}</div>
);

function renderSessionTab(props = {}) {
  const defaultProps = {
    workspaceName: "test-workspace",
    pmContent: undefined as string | undefined,
    hasProjectMgmt: false,
    ...props,
  };

  return render(
    <MemoryRouter>
      <TooltipProvider>
        <SessionTab {...defaultProps} />
      </TooltipProvider>
    </MemoryRouter>
  );
}

afterEach(() => {
  mock.restore();
  cleanup();
});

describe("SessionTab", () => {
  beforeEach(() => {
    mockWorkspaces = [createMockWorkspace()];
    mockSteer.mockClear();
    mockTriggerFactoryRefresh.mockClear();

    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: mockWorkspaces,
      fetchStatus: "idle",
      lastFetchedAt: Date.now(),
    }));
    spyOn(factoryStateModule, "triggerFactoryRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(api.workspaces, "steer").mockImplementation((...args) => mockSteer(...args));
    spyOn(markdownContentModule, "MarkdownContent").mockImplementation((...args) => mockMarkdownContent(...args));
  });

  describe("steer next turn", () => {
    it("renders steer section with textarea", async () => {
      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("Steer Next Turn")).toBeTruthy();
        expect(screen.getByPlaceholderText("Enter re-steering instruction...")).toBeTruthy();
      });
    });

    it("shows submit button", async () => {
      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("Submit")).toBeTruthy();
      });
    });

    it("disables submit when textarea is empty", async () => {
      renderSessionTab();

      await waitFor(() => {
        const submitButton = screen.getByText("Submit");
        expect(submitButton.hasAttribute("disabled")).toBe(true);
      });
    });

    it("enables submit when textarea has content", async () => {
      renderSessionTab();

      const textarea = screen.getByPlaceholderText("Enter re-steering instruction...");
      fireEvent.change(textarea, { target: { value: "go faster" } });

      await waitFor(() => {
        const submitButton = screen.getByText("Submit");
        expect(submitButton.hasAttribute("disabled")).toBe(false);
      });
    });

    it("calls steer API on submit", async () => {
      const user = userEvent.setup();
      renderSessionTab();

      const textarea = screen.getByPlaceholderText("Enter re-steering instruction...");
      fireEvent.change(textarea, { target: { value: "go faster" } });

      const submitButton = screen.getByText("Submit");
      await user.click(submitButton);

      await waitFor(() => {
        expect(mockSteer).toHaveBeenCalledWith("test-workspace", "go faster");
      });
    });

    it("shows success message after steer", async () => {
      const user = userEvent.setup();
      renderSessionTab();

      const textarea = screen.getByPlaceholderText("Enter re-steering instruction...");
      fireEvent.change(textarea, { target: { value: "go faster" } });

      const submitButton = screen.getByText("Submit");
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByText("Steering instruction sent.")).toBeTruthy();
      });
    });

    it("shows error when steer fails", async () => {
      const user = userEvent.setup();
      mockSteer.mockImplementationOnce(() => Promise.reject(new Error("Steer failed")));

      renderSessionTab();

      const textarea = screen.getByPlaceholderText("Enter re-steering instruction...");
      fireEvent.change(textarea, { target: { value: "go faster" } });

      const submitButton = screen.getByText("Submit");
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByText("Steer failed")).toBeTruthy();
      });
    });
  });

  describe("tasks section", () => {
    it("shows tasks section", async () => {
      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("Tasks")).toBeTruthy();
      });
    });

    it("shows empty message when no project todos", async () => {
      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("No project todos")).toBeTruthy();
      });
    });

    it("shows empty message when no agent todos", async () => {
      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("No active agent todos")).toBeTruthy();
      });
    });

    it("displays project todos", async () => {
      mockWorkspaces = [createMockWorkspace({
        projectTodos: [
          { id: "1", content: "Fix bug", status: "in_progress", priority: "high" },
          { id: "2", content: "Add feature", status: "pending", priority: "medium" },
        ],
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("Fix bug")).toBeTruthy();
        expect(screen.getByText("Add feature")).toBeTruthy();
      });
    });

    it("displays agent todos", async () => {
      mockWorkspaces = [createMockWorkspace({
        agentTodos: [
          { id: "1", content: "Write tests", status: "completed", priority: "high" },
        ],
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("Write tests")).toBeTruthy();
      });
    });
  });

  describe("cost section", () => {
    it("displays a full session token and dollar breakdown", async () => {
      mockWorkspaces = [createMockWorkspace({
        cost: {
          totalCost: 2,
          dollars: createDollarBreakdown({
            input: 1,
            output: 0.5,
            reasoning: 0.1,
            cacheRead: 0.2,
            cacheWrite: 0.2,
            total: 2,
          }),
          totalTokens: { input: 1000, output: 500, reasoning: 100, cacheRead: 200, cacheWrite: 200 },
          byAgent: [],
        },
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("Cost Tracking")).toBeTruthy();
        expect(screen.getByText("Session Breakdown")).toBeTruthy();
      });

      expect(screen.getByText("Input")).toBeTruthy();
      expect(screen.getByText("Output")).toBeTruthy();
      expect(screen.getByText("Reasoning")).toBeTruthy();
      expect(screen.getByText("Cache Read")).toBeTruthy();
      expect(screen.getByText("Cache Write")).toBeTruthy();
      expect(screen.getByText("Total")).toBeTruthy();
      expect(screen.getByText("1,000 tok")).toBeTruthy();
      expect(screen.getByText("500 tok")).toBeTruthy();
      expect(screen.getByText("100 tok")).toBeTruthy();
      expect(screen.getAllByText("200 tok").length).toBeGreaterThanOrEqual(2);
      expect(screen.getByText("2,000 tok")).toBeTruthy();
      expect(screen.getByText("$1.0000")).toBeTruthy();
      expect(screen.getByText("$0.5000")).toBeTruthy();
      expect(screen.getByText("$0.1000")).toBeTruthy();
      expect(screen.getAllByText("$0.2000").length).toBeGreaterThanOrEqual(2);
      expect(screen.getByText("$2.0000")).toBeTruthy();
    });

    it("shows per-agent token and dollar breakdowns when available", async () => {
      const user = userEvent.setup();
      mockWorkspaces = [createMockWorkspace({
        cost: {
          totalCost: 1.5,
          dollars: createDollarBreakdown({
            input: 0.9,
            output: 0.45,
            reasoning: 0.09,
            cacheRead: 0.045,
            cacheWrite: 0.015,
            total: 1.5,
          }),
          totalTokens: { input: 1000, output: 500, reasoning: 0, cacheRead: 0, cacheWrite: 0 },
          byAgent: [
            {
              agent: "coordinator",
              cost: 0.75,
              dollars: createDollarBreakdown({
                input: 0.3,
                output: 0.15,
                reasoning: 0.03,
                cacheRead: 0.015,
                cacheWrite: 0.005,
                total: 0.75,
              }),
              tokens: { input: 300, output: 150, reasoning: 30, cacheRead: 15, cacheWrite: 5 },
              steps: [
                {
                  stepId: "step-1",
                  agent: "coordinator",
                  cost: 0.125,
                  dollars: createDollarBreakdown({
                    input: 0.05,
                    output: 0.025,
                    reasoning: 0.01,
                    cacheRead: 0.005,
                    cacheWrite: 0.005,
                    total: 0.125,
                  }),
                  tokens: { input: 100, output: 50, reasoning: 10, cacheRead: 5, cacheWrite: 5 },
                  timestamp: "2026-03-07T00:00:00Z",
                },
              ],
            },
            {
              agent: "developer",
              cost: 0.75,
              dollars: createDollarBreakdown({
                input: 0.6,
                output: 0.3,
                total: 0.75,
              }),
              tokens: { input: 700, output: 350, reasoning: 0, cacheRead: 0, cacheWrite: 0 },
              steps: [],
            },
          ],
        },
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("By Agent (2 agents)")).toBeTruthy();
      });

      await user.click(screen.getByText("By Agent (2 agents)"));
      await user.click(screen.getByText("coordinator"));

      expect(screen.getByText("500 tok | 1 steps")).toBeTruthy();
      expect(screen.getByText("step-1")).toBeTruthy();
      expect(screen.getByText("100 in | 50 out | 10 reason | 5 cache-read | 5 cache-write | 170 total")).toBeTruthy();
      expect(screen.getAllByText("$0.3000").length).toBeGreaterThanOrEqual(1);
      expect(screen.getAllByText("$0.1500").length).toBeGreaterThanOrEqual(1);
      expect(screen.getAllByText("$0.0300").length).toBeGreaterThanOrEqual(1);
      expect(screen.getAllByText("$0.0150").length).toBeGreaterThanOrEqual(1);
      expect(screen.getAllByText("$0.0050").length).toBeGreaterThanOrEqual(1);
      expect(screen.getAllByText("$0.7500").length).toBeGreaterThanOrEqual(1);
      expect(screen.getByText("$0.050000")).toBeTruthy();
      expect(screen.getByText("$0.025000")).toBeTruthy();
      expect(screen.getByText("$0.010000")).toBeTruthy();
      expect(screen.getAllByText("$0.005000").length).toBeGreaterThanOrEqual(2);
      expect(screen.getAllByText("$0.125000").length).toBeGreaterThanOrEqual(2);
    });

    it("tolerates missing per-agent arrays from persisted state", async () => {
      mockWorkspaces = [createMockWorkspace({
        cost: {
          totalCost: 1.5,
          dollars: createDollarBreakdown({ total: 1.5 }),
          totalTokens: { input: 1000, output: 500, reasoning: 0, cacheRead: 0, cacheWrite: 0 },
          byAgent: null,
        } as unknown,
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("Cost Tracking")).toBeTruthy();
      });

      expect(screen.queryByText(/By Agent/)).toBeNull();
    });
  });

  describe("agent sequence", () => {
    it("shows empty state when no agent sequence", async () => {
      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("No agent sequence yet")).toBeTruthy();
      });
    });

    it("displays agent sequence when available", async () => {
      mockWorkspaces = [createMockWorkspace({
        agentSequence: [
          { agent: "coordinator", model: "opencode/glm-5", elapsedTime: "1m", isCurrent: true },
          { agent: "developer", model: "opencode/glm-5", elapsedTime: "2m", isCurrent: false },
        ],
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("coordinator")).toBeTruthy();
        expect(screen.getByText("developer")).toBeTruthy();
      });
    });
  });

  describe("project management section", () => {
    it("shows PM section when hasProjectMgmt is true", () => {
      renderSessionTab({ hasProjectMgmt: true, pmContent: "# PM" });
      expect(screen.getByText("PROJECT_MANAGEMENT.md")).toBeTruthy();
    });

    it("shows no content message when PM content is empty", () => {
      renderSessionTab({ hasProjectMgmt: true });
      expect(screen.getByText("No content available")).toBeTruthy();
    });
  });
});
