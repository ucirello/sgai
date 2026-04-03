import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { act, render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode, Suspense, startTransition, useEffect, type ReactNode, type TextareaHTMLAttributes } from "react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import * as textareaModule from "@/components/ui/textarea";
import * as workspacePageStateModule from "@/lib/workspace-page-state";
import { api } from "@/lib/api";
import * as markdownContentModule from "@/components/MarkdownContent";
import { SessionTab } from "../SessionTab";

beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

const mockSteer = mock(() => Promise.resolve({ success: true, message: "ok" }));
const mockTriggerFactoryRefresh = mock(() => {});
let markdownRenderCount = 0;
let steeringTextareaRenderCount = 0;
let steeringTextareaMountCount = 0;

function InstrumentedTextarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  steeringTextareaRenderCount += 1;

  useEffect(() => {
    steeringTextareaMountCount += 1;
  }, []);

  return <textarea data-slot="textarea" {...props} />;
}

const createDollarBreakdown = (overrides = {}) => ({
  input: 0,
  output: 0,
  reasoning: 0,
  cacheRead: 0,
  cacheWrite: 0,
  total: 0,
  ...overrides,
});

type TestTodoEntry = {
  id: string;
  content: string;
  status: string;
  priority: string;
};

const createAgentTodoSection = (agent: string, todos: TestTodoEntry[] = []) => ({ agent, todos });

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
  agentTodoSections: [],
  log: [],
  external: false,
  ...overrides,
});

let mockWorkspaces = [createMockWorkspace()];

const mockMarkdownContent = ({ content }: { content: string }) => (
  (() => {
    markdownRenderCount += 1;
    return <div data-testid="markdown-content">{content}</div>;
  })()
);

function maybeWrapStrictMode(children: ReactNode, strictMode: boolean) {
  if (!strictMode) {
    return children;
  }

  return <StrictMode>{children}</StrictMode>;
}

const neverSettlingPromise = new Promise<never>(() => {});

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function SuspendOnDemand({ active }: { active: boolean }) {
  if (active) {
    throw neverSettlingPromise;
  }

  return null;
}

function renderSessionTab(props = {}, { strictMode = false }: { strictMode?: boolean } = {}) {
  const workspace = mockWorkspaces[0] ?? createMockWorkspace();
  const defaultProps = {
    workspaceName: "test-workspace",
    agentSequence: workspace.agentSequence,
    cost: workspace.cost,
    modelStatuses: workspace.modelStatuses,
    projectTodos: workspace.projectTodos,
    agentTodoSections: workspace.agentTodoSections,
    pmContent: workspace.pmContent as string | undefined,
    hasProjectMgmt: workspace.hasProjectMgmt,
    ...props,
  };

  return render(maybeWrapStrictMode(
    <MemoryRouter>
      <TooltipProvider>
        <SessionTab {...defaultProps} />
      </TooltipProvider>
    </MemoryRouter>,
    strictMode,
  ));
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
    markdownRenderCount = 0;
    steeringTextareaRenderCount = 0;
    steeringTextareaMountCount = 0;

    spyOn(workspacePageStateModule, "triggerWorkspacePageRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(api.workspaces, "steer").mockImplementation((...args) => mockSteer(...args));
    spyOn(markdownContentModule, "MarkdownContent").mockImplementation((...args) => mockMarkdownContent(...args));
    spyOn(textareaModule, "Textarea").mockImplementation((props) => <InstrumentedTextarea {...props} />);
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

      expect(mockTriggerFactoryRefresh).not.toHaveBeenCalled();
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

    it("keeps the steering controls disabled until the request settles", async () => {
      const user = userEvent.setup();
      const pendingSteer = deferredValue<{ success: boolean; message: string }>();
      mockSteer.mockImplementationOnce(() => pendingSteer.promise);

      renderSessionTab();

      const textarea = screen.getByPlaceholderText("Enter re-steering instruction...") as HTMLTextAreaElement;
      fireEvent.change(textarea, { target: { value: "go faster" } });

      const submitButton = screen.getByRole("button", { name: "Submit" });
      await user.click(submitButton);

      await waitFor(() => {
        expect(textarea.disabled).toBe(true);
        expect(submitButton.hasAttribute("disabled")).toBe(true);
      });

      await act(async () => {
        pendingSteer.resolve({ success: true, message: "ok" });
        await pendingSteer.promise;
      });

      await waitFor(() => {
        expect(screen.getByText("Steering instruction sent.")).toBeTruthy();
      });
    });

    it("does not rerender project management markdown while typing a steering draft", async () => {
      const user = userEvent.setup();

      renderSessionTab({ hasProjectMgmt: true, pmContent: "# PM" });

      await waitFor(() => {
        expect(screen.getByText("PROJECT_MANAGEMENT.md")).toBeTruthy();
      });

      expect(markdownRenderCount).toBe(1);

      const textarea = screen.getByPlaceholderText("Enter re-steering instruction...");
      await user.type(textarea, "go faster");

      expect((screen.getByPlaceholderText("Enter re-steering instruction...") as HTMLTextAreaElement).value).toBe("go faster");
      expect(markdownRenderCount).toBe(1);
    });

    it("keeps the steering composer stable while internals props refresh during typing", async () => {
      const user = userEvent.setup();

      const view = renderSessionTab({
        hasProjectMgmt: true,
        pmContent: "# PM 0",
        agentSequence: [{ agent: "coordinator", model: "opencode/glm-5", elapsedTime: "0m", isCurrent: true }],
        modelStatuses: [{ modelId: "opencode/glm-5", status: "model-working" }],
        projectTodos: [{ id: "todo-0", content: "Todo 0", status: "pending", priority: "medium" }],
      });

      const textarea = await screen.findByRole("textbox", { name: /re-steering instruction/i });
      textarea.focus();

      expect(steeringTextareaMountCount).toBe(1);

      await user.type(textarea, "abc");
      const renderCountAfterFirstChunk = steeringTextareaRenderCount;

      view.rerender(
        <MemoryRouter>
          <TooltipProvider>
            <SessionTab
              workspaceName="test-workspace"
              hasProjectMgmt
              pmContent="# PM 1"
              agentSequence={[{ agent: "coordinator", model: "opencode/glm-5", elapsedTime: "1m", isCurrent: true }]}
              modelStatuses={[{ modelId: "opencode/glm-5", status: "model-done" }]}
              projectTodos={[{ id: "todo-1", content: "Todo 1", status: "pending", priority: "medium" }]}
              agentTodoSections={[]}
            />
          </TooltipProvider>
        </MemoryRouter>
      );

      const refreshedTextarea = screen.getByRole("textbox", { name: /re-steering instruction/i }) as HTMLTextAreaElement;
      expect(steeringTextareaRenderCount).toBe(renderCountAfterFirstChunk);
      expect(steeringTextareaMountCount).toBe(1);
      expect(refreshedTextarea.value).toBe("abc");
      expect(refreshedTextarea).toBe(document.activeElement);

      await user.type(refreshedTextarea, "defghij");
      expect((screen.getByRole("textbox", { name: /re-steering instruction/i }) as HTMLTextAreaElement).value).toBe("abcdefghij");
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

    it("shows empty message when there are no active agents", async () => {
      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("No active agents")).toBeTruthy();
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

    it("renders grouped agent todo sections in active-agent order and shows per-agent empty states", async () => {
      mockWorkspaces = [createMockWorkspace({
        agentTodoSections: [
          createAgentTodoSection("coordinator", [
            { id: "1", content: "Write tests", status: "completed", priority: "high" },
          ]),
          createAgentTodoSection("react-developer"),
          createAgentTodoSection("go-developer", [
            { id: "2", content: "Update API contract", status: "pending", priority: "medium" },
          ]),
        ],
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByRole("heading", { level: 3, name: "coordinator" })).toBeTruthy();
        expect(screen.getByRole("heading", { level: 3, name: "react-developer" })).toBeTruthy();
        expect(screen.getByRole("heading", { level: 3, name: "go-developer" })).toBeTruthy();
        expect(screen.getByText("Write tests")).toBeTruthy();
        expect(screen.getByText("No active TODOs for react-developer")).toBeTruthy();
        expect(screen.getByText("Update API contract")).toBeTruthy();
      });

      const agentSectionHeadings = screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent);
      expect(agentSectionHeadings).toEqual(["coordinator", "react-developer", "go-developer"]);
    });

    it("does not emit duplicate-key warnings when persisted agent todos are missing ids", async () => {
      const consoleErrorSpy = spyOn(console, "error").mockImplementation(() => {});

      mockWorkspaces = [createMockWorkspace({
        agentTodoSections: [
          createAgentTodoSection("react-developer", [
            { id: "", content: "First todo", status: "pending", priority: "high" },
            { id: "", content: "Second todo", status: "pending", priority: "medium" },
          ]),
        ],
      })];

      renderSessionTab();

      await waitFor(() => {
        expect(screen.getByText("First todo")).toBeTruthy();
        expect(screen.getByText("Second todo")).toBeTruthy();
      });

      const duplicateKeyWarnings = consoleErrorSpy.mock.calls.filter(([message]) => (
        typeof message === "string" && message.includes("Encountered two children with the same key")
      ));

      expect(duplicateKeyWarnings).toHaveLength(0);
    });

    it("keeps the original blank-id agent todo row mounted when a same-signature blank-id todo is inserted ahead of it", async () => {
      const view = renderSessionTab({
        agentTodoSections: [
          createAgentTodoSection("react-developer", [
            { id: "", content: "Duplicate todo", status: "pending", priority: "medium" },
          ]),
        ],
      });

      const originalRow = await waitFor(() => {
        const row = screen.getByText("Duplicate todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      view.rerender(
        <MemoryRouter>
          <TooltipProvider>
            <SessionTab
              workspaceName="test-workspace"
              agentSequence={[]}
              cost={createMockWorkspace().cost}
              modelStatuses={[]}
              projectTodos={[]}
              agentTodoSections={[
                createAgentTodoSection("react-developer", [
                  { id: "", content: "Duplicate todo", status: "pending", priority: "medium" },
                  { id: "", content: "Duplicate todo", status: "pending", priority: "medium" },
                ]),
              ]}
            />
          </TooltipProvider>
        </MemoryRouter>
      );

      await waitFor(() => {
        expect(screen.getAllByText("Duplicate todo")).toHaveLength(2);
      });

      const rows = screen.getAllByRole("listitem") as HTMLLIElement[];
      const updatedRow = rows[1] as HTMLLIElement;

      expect(updatedRow).toBe(originalRow);
      expect(rows).toHaveLength(2);
      expect(rows[0]?.textContent).toContain("Duplicate todo");
      expect(rows[1]).toBe(updatedRow);
    });

    it("keeps the original blank-id agent todo row mounted when its mutable display fields change", async () => {
      const view = renderSessionTab({
        agentTodoSections: [
          createAgentTodoSection("react-developer", [
            { id: "", content: "Original todo", status: "pending", priority: "medium" },
          ]),
        ],
      });

      const originalRow = await waitFor(() => {
        const row = screen.getByText("Original todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      view.rerender(
        <MemoryRouter>
          <TooltipProvider>
            <SessionTab
              workspaceName="test-workspace"
              agentSequence={[]}
              cost={createMockWorkspace().cost}
              modelStatuses={[]}
              projectTodos={[]}
              agentTodoSections={[
                createAgentTodoSection("react-developer", [
                  { id: "", content: "Updated todo", status: "in_progress", priority: "high" },
                ]),
              ]}
            />
          </TooltipProvider>
        </MemoryRouter>
      );

      const updatedRow = await waitFor(() => {
        const row = screen.getByText("Updated todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      expect(updatedRow).toBe(originalRow);
      expect(updatedRow.textContent).toContain("Updated todo");
      expect(updatedRow.textContent).toContain("high");
    });

    it("keeps the original blank-id agent todo row mounted under StrictMode when a same-signature blank-id todo is inserted ahead of it", async () => {
      const view = renderSessionTab({
        agentTodoSections: [
          createAgentTodoSection("react-developer", [
            { id: "", content: "Duplicate todo", status: "pending", priority: "medium" },
          ]),
        ],
      }, { strictMode: true });

      const originalRow = await waitFor(() => {
        const row = screen.getByText("Duplicate todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      view.rerender(maybeWrapStrictMode(
        <MemoryRouter>
          <TooltipProvider>
            <SessionTab
              workspaceName="test-workspace"
              agentSequence={[]}
              cost={createMockWorkspace().cost}
              modelStatuses={[]}
              projectTodos={[]}
              agentTodoSections={[
                createAgentTodoSection("react-developer", [
                  { id: "", content: "Duplicate todo", status: "pending", priority: "medium" },
                  { id: "", content: "Duplicate todo", status: "pending", priority: "medium" },
                ]),
              ]}
            />
          </TooltipProvider>
        </MemoryRouter>,
        true,
      ));

      await waitFor(() => {
        expect(screen.getAllByText("Duplicate todo")).toHaveLength(2);
      });

      const rows = screen.getAllByRole("listitem") as HTMLLIElement[];
      expect(rows[1]).toBe(originalRow);
    });

    it("keeps the original blank-id agent todo row mounted under StrictMode when its mutable display fields change", async () => {
      const view = renderSessionTab({
        agentTodoSections: [
          createAgentTodoSection("react-developer", [
            { id: "", content: "Original todo", status: "pending", priority: "medium" },
          ]),
        ],
      }, { strictMode: true });

      const originalRow = await waitFor(() => {
        const row = screen.getByText("Original todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      view.rerender(maybeWrapStrictMode(
        <MemoryRouter>
          <TooltipProvider>
            <SessionTab
              workspaceName="test-workspace"
              agentSequence={[]}
              cost={createMockWorkspace().cost}
              modelStatuses={[]}
              projectTodos={[]}
              agentTodoSections={[
                createAgentTodoSection("react-developer", [
                  { id: "", content: "Updated todo", status: "in_progress", priority: "high" },
                ]),
              ]}
            />
          </TooltipProvider>
        </MemoryRouter>,
        true,
      ));

      const updatedRow = await waitFor(() => {
        const row = screen.getByText("Updated todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      expect(updatedRow).toBe(originalRow);
      expect(updatedRow.textContent).toContain("Updated todo");
      expect(updatedRow.textContent).toContain("high");
    });

    it("ignores abandoned StrictMode duplicate-insertion renders when later committing a mutable blank-id todo update", async () => {
      const renderTree = (agentTodoSections: Array<{ agent: string; todos: TestTodoEntry[] }>, shouldSuspend: boolean) => (
        maybeWrapStrictMode(
          <MemoryRouter>
            <TooltipProvider>
              <Suspense fallback={null}>
                <SessionTab
                  workspaceName="test-workspace"
                  agentSequence={[]}
                  cost={createMockWorkspace().cost}
                  modelStatuses={[]}
                  projectTodos={[]}
                  agentTodoSections={agentTodoSections}
                />
                <SuspendOnDemand active={shouldSuspend} />
              </Suspense>
            </TooltipProvider>
          </MemoryRouter>,
          true,
        )
      );

      const view = render(renderTree([
        createAgentTodoSection("react-developer", [
          { id: "", content: "Original todo", status: "pending", priority: "medium" },
        ]),
      ], false));

      const originalRow = await waitFor(() => {
        const row = screen.getByText("Original todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      startTransition(() => {
        view.rerender(renderTree([
          createAgentTodoSection("react-developer", [
            { id: "", content: "Original todo", status: "pending", priority: "medium" },
            { id: "", content: "Original todo", status: "pending", priority: "medium" },
          ]),
        ], true));
      });

      await waitFor(() => {
        expect(screen.getByText("Original todo").closest("li")).toBe(originalRow);
      });

      view.rerender(renderTree([
        createAgentTodoSection("react-developer", [
          { id: "", content: "Updated todo", status: "in_progress", priority: "high" },
        ]),
      ], false));

      const updatedRow = await waitFor(() => {
        const row = screen.getByText("Updated todo").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      expect(updatedRow).toBe(originalRow);
      expect(updatedRow.textContent).toContain("Updated todo");
      expect(updatedRow.textContent).toContain("high");
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

    it("keeps the same agent sequence row mounted when only elapsed time changes", async () => {
      const view = renderSessionTab({
        agentSequence: [{ agent: "coordinator", model: "opencode/glm-5", elapsedTime: "1m", isCurrent: true }],
      });

      const originalRow = await waitFor(() => {
        const row = screen.getByText("coordinator").closest("li");
        expect(row).toBeTruthy();
        expect(screen.getByText("(1m)")).toBeTruthy();
        return row as HTMLLIElement;
      });

      view.rerender(
        <MemoryRouter>
          <TooltipProvider>
            <SessionTab
              workspaceName="test-workspace"
              agentSequence={[{ agent: "coordinator", model: "opencode/glm-5", elapsedTime: "2m", isCurrent: true }]}
              cost={createMockWorkspace().cost}
              modelStatuses={[]}
              projectTodos={[]}
              agentTodoSections={[]}
            />
          </TooltipProvider>
        </MemoryRouter>
      );

      await waitFor(() => {
        expect(screen.getByText("(2m)")).toBeTruthy();
      });

      const updatedRow = screen.getByText("coordinator").closest("li") as HTMLLIElement;
      expect(updatedRow).toBe(originalRow);
      expect(screen.queryByText("(1m)")).toBeNull();
    });

    it("keeps the original agent sequence row mounted when a new newest-first handoff is inserted at the top", async () => {
      const view = renderSessionTab({
        agentSequence: [{ agent: "coordinator", model: "opencode/glm-5", elapsedTime: "1m", isCurrent: true }],
      });

      const originalRow = await waitFor(() => {
        const row = screen.getByText("coordinator").closest("li");
        expect(row).toBeTruthy();
        return row as HTMLLIElement;
      });

      view.rerender(
        <MemoryRouter>
          <TooltipProvider>
            <SessionTab
              workspaceName="test-workspace"
              agentSequence={[
                { agent: "developer", model: "opencode/glm-5", elapsedTime: "10s", isCurrent: true },
                { agent: "coordinator", model: "opencode/glm-5", elapsedTime: "1m", isCurrent: false },
              ]}
              cost={createMockWorkspace().cost}
              modelStatuses={[]}
              projectTodos={[]}
              agentTodoSections={[]}
            />
          </TooltipProvider>
        </MemoryRouter>
      );

      await waitFor(() => {
        expect(screen.getByText("developer")).toBeTruthy();
        expect(screen.getByText("coordinator")).toBeTruthy();
      });

      const updatedRow = screen.getByText("coordinator").closest("li") as HTMLLIElement;
      const rows = screen.getAllByRole("listitem") as HTMLLIElement[];

      expect(updatedRow).toBe(originalRow);
      expect(rows).toHaveLength(2);
      expect(rows[0]?.textContent).toContain("developer");
      expect(rows[1]).toBe(updatedRow);
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
