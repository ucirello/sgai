import { afterEach, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import * as MonacoEditorModule from "@monaco-editor/react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { api } from "@/lib/api";
import { resetFactoryStateStore } from "@/lib/factory-state";
import { EditGoal } from "@/pages/EditGoal";
import { InlineForkEditor } from "@/pages/InlineForkEditor";

const DEFAULT_GOAL_CONTENT = "# Test Goal\n\nThis is a test goal.";
const DEFAULT_FORK_TEMPLATE = "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal\n\nDescribe your task";

const mockFork = mock((goalContent: string) => ({
  name: "new-fork",
  dir: "/path/to/new-fork",
  goalContent,
}));
const mockGetGoal = mock(() => ({ content: DEFAULT_GOAL_CONTENT }));
const mockUpdateGoal = mock((content: string) => ({
  updated: true,
  workspace: "test-workspace",
  content,
}));
const mockGetForkTemplate = mock(() => ({ content: DEFAULT_FORK_TEMPLATE }));
const mockGetState = mock(() => ({
  workspaces: [
    {
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
      goalContent: DEFAULT_GOAL_CONTENT,
      rawGoalContent: DEFAULT_GOAL_CONTENT,
      pmContent: "",
      hasProjectMgmt: false,
      svgHash: "",
      totalExecTime: "",
      latestProgress: "",
      humanMessage: "",
      agentSequence: [],
      cost: {
        totalCost: 0,
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
    },
  ],
}));
const mockAgentsList = mock(() => ({ agents: [] }));
const mockModelsList = mock(() => ({ models: [] }));

type RestorableGlobalKey = "fetch" | "EventSource";

let editorValue = "";
let originalFetchDescriptor: PropertyDescriptor | undefined;
let originalEventSourceDescriptor: PropertyDescriptor | undefined;

function captureGlobalDescriptor(key: RestorableGlobalKey) {
  return Object.getOwnPropertyDescriptor(globalThis, key);
}

function restoreGlobalDescriptor(key: RestorableGlobalKey, descriptor: PropertyDescriptor | undefined) {
  if (descriptor) {
    Object.defineProperty(globalThis, key, descriptor);
    return;
  }

  Reflect.deleteProperty(globalThis, key);
}

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    disconnect() {}
    unobserve() {}
  };
}

function MockEditor({ value, onChange, onMount }: {
  value?: string;
  onChange?: (nextValue: string | undefined) => void;
  onMount?: (editor: unknown, monaco: unknown) => void;
}) {
  useEffect(() => {
    editorValue = value ?? "";
  }, [value]);

  useEffect(() => {
    const model = {
      getValue: () => editorValue,
      getValueInRange: () => "",
      getFullModelRange: () => ({
        startLineNumber: 1,
        startColumn: 1,
        endLineNumber: 1,
        endColumn: Math.max(editorValue.length, 1),
      }),
      getLineContent: () => editorValue,
    };

    const editor = {
      addAction: () => ({ dispose() {} }),
      getModel: () => model,
      getDomNode: () => document.querySelector("[data-testid='monaco-editor-input']"),
      deltaDecorations: () => [],
      onDidChangeModelContent: () => ({ dispose() {} }),
      executeEdits: () => {},
      getSelection: () => ({
        startLineNumber: 1,
        startColumn: 1,
        endLineNumber: 1,
        endColumn: 1,
      }),
      setSelection: () => {},
      focus: () => {},
    };

    const monaco = {
      KeyMod: { CtrlCmd: 1 },
      KeyCode: {
        KeyA: 65,
        KeyB: 66,
        KeyI: 73,
        KeyK: 75,
        KeyS: 83,
      },
      languages: {
        registerCompletionItemProvider: () => ({ dispose() {} }),
      },
    };

    onMount?.(editor, monaco);
  }, [onMount]);

  return (
    <textarea
      data-testid="monaco-editor-input"
      value={value ?? ""}
      onKeyDown={(event) => {
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
          event.preventDefault();
          event.stopPropagation();
        }
      }}
      onChange={(event) => {
        editorValue = event.target.value;
        onChange?.(event.target.value);
      }}
    />
  );
}

class MockEventSource {
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(public url: string) {
    queueMicrotask(() => {
      this.onopen?.(new Event("open"));
    });
  }

  addEventListener() {}

  close() {}
}

function jsonResponse(body: unknown, init?: ResponseInit) {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });
}

const mockFetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
  const requestUrl = new URL(typeof input === "string" ? input : input.toString(), "http://localhost");
  const method = init?.method ?? "GET";
  const pathname = requestUrl.pathname;

  if (method === "GET" && pathname === "/api/v1/state") {
    return jsonResponse(mockGetState());
  }

  throw new Error(`Unhandled fetch: ${method} ${pathname}${requestUrl.search}`);
});

function renderWithProviders(ui: React.ReactNode) {
  return render(
    <MemoryRouter>
      <TooltipProvider>{ui}</TooltipProvider>
    </MemoryRouter>,
  );
}

function RoutePathProbe() {
  const location = useLocation();
  return <div data-testid="route-path">{location.pathname}</div>;
}

function renderEditGoal() {
  return render(
    <MemoryRouter initialEntries={["/workspaces/test-workspace/goal/edit"]}>
      <TooltipProvider>
        <Routes>
          <Route path="/workspaces/:name/goal/edit" element={<EditGoal />} />
        </Routes>
      </TooltipProvider>
    </MemoryRouter>,
  );
}

function renderInlineForkEditor() {
  return render(
    <MemoryRouter initialEntries={["/workspaces/test-workspace/fork"]}>
      <TooltipProvider>
        <Routes>
          <Route
            path="*"
            element={(
              <>
                <InlineForkEditor workspaceName="test-workspace" />
                <RoutePathProbe />
              </>
            )}
          />
        </Routes>
      </TooltipProvider>
    </MemoryRouter>
  );
}

describe("MarkdownEditor submit shortcut integration", () => {
  beforeEach(() => {
    editorValue = "";
    originalFetchDescriptor = captureGlobalDescriptor("fetch");
    originalEventSourceDescriptor = captureGlobalDescriptor("EventSource");
    globalThis.fetch = mockFetch as unknown as typeof fetch;
    globalThis.EventSource = MockEventSource as unknown as typeof EventSource;
    sessionStorage.clear();
    resetFactoryStateStore();

    mockFetch.mockClear();
    mockFork.mockClear();
    mockGetGoal.mockClear();
    mockUpdateGoal.mockClear();
    mockGetForkTemplate.mockClear();
    mockGetState.mockClear();
    mockAgentsList.mockClear();
    mockModelsList.mockClear();

    spyOn(MonacoEditorModule, "default").mockImplementation((...args) => MockEditor(...args));
    spyOn(api.agents, "list").mockImplementation(async () => mockAgentsList());
    spyOn(api.models, "list").mockImplementation(async () => mockModelsList());
    spyOn(api.workspaces, "getGoal").mockImplementation(async () => mockGetGoal());
    spyOn(api.workspaces, "updateGoal").mockImplementation(async (_name: string, content: string) => mockUpdateGoal(content));
    spyOn(api.workspaces, "forkTemplate").mockImplementation(async () => mockGetForkTemplate());
    spyOn(api.workspaces, "fork").mockImplementation(async (_name: string, goalContent: string) => mockFork(goalContent));
  });

  afterEach(() => {
    mock.restore();
    resetFactoryStateStore();
    restoreGlobalDescriptor("fetch", originalFetchDescriptor);
    restoreGlobalDescriptor("EventSource", originalEventSourceDescriptor);
    cleanup();
  });

  it("removes mocked fetch and EventSource globals when the original state was missing", () => {
    const mockedFetchDescriptor = captureGlobalDescriptor("fetch");
    const mockedEventSourceDescriptor = captureGlobalDescriptor("EventSource");

    restoreGlobalDescriptor("fetch", undefined);
    restoreGlobalDescriptor("EventSource", undefined);

    expect(Object.getOwnPropertyDescriptor(globalThis, "fetch")).toBeUndefined();
    expect(Object.getOwnPropertyDescriptor(globalThis, "EventSource")).toBeUndefined();

    restoreGlobalDescriptor("fetch", mockedFetchDescriptor);
    restoreGlobalDescriptor("EventSource", mockedEventSourceDescriptor);
  });

  it("submits from MarkdownEditor when Ctrl+S is pressed inside the editor", async () => {
    const onSubmitShortcut = mock(() => {});

    renderWithProviders(
      <MarkdownEditor value="# Goal" onChange={() => {}} onSubmitShortcut={onSubmitShortcut} />,
    );

    const editor = await screen.findByTestId("monaco-editor-input") as HTMLTextAreaElement;
    editor.focus();

    fireEvent.keyDown(editor, { key: "s", ctrlKey: true, bubbles: true });

    await waitFor(() => {
      expect(onSubmitShortcut).toHaveBeenCalledTimes(1);
    });
  });

  it("saves GOAL.md from EditGoal when Meta+S is pressed inside the editor", async () => {
    renderEditGoal();

    await waitFor(() => {
      expect((screen.getByTestId("monaco-editor-input") as HTMLTextAreaElement).value).toContain(DEFAULT_GOAL_CONTENT);
    });

    const editor = screen.getByTestId("monaco-editor-input") as HTMLTextAreaElement;
    editor.focus();
    fireEvent.keyDown(editor, { key: "s", metaKey: true, bubbles: true });

    await waitFor(() => {
      expect(mockUpdateGoal).toHaveBeenCalledWith(DEFAULT_GOAL_CONTENT);
    });

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Saved!" })).toBeTruthy();
    });
  });

  it("submits the inline fork flow when Ctrl+S is pressed inside the editor", async () => {
    renderInlineForkEditor();

    await waitFor(() => {
      expect((screen.getByTestId("monaco-editor-input") as HTMLTextAreaElement).value).toContain("# Goal");
    });

    const editor = screen.getByTestId("monaco-editor-input") as HTMLTextAreaElement;
    editor.focus();
    fireEvent.keyDown(editor, { key: "s", ctrlKey: true, bubbles: true });

    await waitFor(() => {
      expect(mockFork).toHaveBeenCalledWith(expect.stringContaining("# Goal"));
    });

    await waitFor(() => {
      expect(screen.getByTestId("route-path").textContent).toBe("/workspaces/new-fork/progress");
    });
  });
});
