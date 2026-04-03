import { afterEach, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import type { ReactNode } from "react";
import * as factoryStateModule from "@/lib/factory-state";
import type { ApiForkEntry, ApiWorkspaceEntry } from "@/types";
import { App } from "../App";

function createMockFork(overrides: Partial<ApiForkEntry> = {}): ApiForkEntry {
  return {
    name: "pending-fork",
    dir: "/workspaces/pending-fork",
    running: false,
    needsInput: false,
    inProgress: false,
    pinned: false,
    title: "",
    ...overrides,
  };
}

function createMockWorkspace(overrides: Partial<ApiWorkspaceEntry> = {}): ApiWorkspaceEntry {
  return {
    name: "workspace",
    dir: "/workspaces/workspace",
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
    cost: {
      totalCost: 0,
      dollars: {
        input: 0,
        output: 0,
        reasoning: 0,
        cacheRead: 0,
        cacheWrite: 0,
        total: 0,
      },
      totalTokens: {
        input: 0,
        output: 0,
        reasoning: 0,
        cacheRead: 0,
        cacheWrite: 0,
      },
      byAgent: [],
    },
    events: [],
    messages: [],
    projectTodos: [],
    agentTodoSections: [],
    log: [],
    ...overrides,
  };
}

function RouteProbe({ children }: { children: ReactNode }) {
  const location = useLocation();

  return (
    <div>
      <div data-testid="route-path">{location.pathname}</div>
      {children}
    </div>
  );
}

function renderApp(initialRoute = "/") {
  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
      <Routes>
        <Route element={<App />}>
          <Route
            index
            element={(
              <RouteProbe>
                <button type="button" data-testid="outside-target">Outside</button>
              </RouteProbe>
            )}
          />
          <Route
            path="editable"
            element={(
              <RouteProbe>
                <input data-testid="editable-input" />
                <textarea data-testid="editable-textarea" />
                <select data-testid="editable-select" defaultValue="one">
                  <option value="one">One</option>
                </select>
                <div data-testid="editable-content" contentEditable suppressContentEditableWarning>
                  Editable content
                </div>
                <div className="monaco-editor">
                  <div data-testid="editor-surface" tabIndex={0}>Editor surface</div>
                </div>
              </RouteProbe>
            )}
          />
          <Route
            path="workspaces/:name/respond"
            element={(
              <RouteProbe>
                <div data-testid="respond-screen">Respond screen</div>
              </RouteProbe>
            )}
          />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

let mockWorkspaces: ApiWorkspaceEntry[] = [];
const originalNotification = window.Notification;
const mockRequestPermission = mock(async () => "denied");

describe("App pending response shortcut", () => {
  beforeEach(() => {
    mockWorkspaces = [];
    mockRequestPermission.mockClear();

    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: mockWorkspaces,
      fetchStatus: "idle",
      lastFetchedAt: null,
    }));

    Object.defineProperty(window, "Notification", {
      configurable: true,
      writable: true,
      value: {
        permission: "denied",
        requestPermission: mockRequestPermission,
      },
    });
  });

  afterEach(() => {
    mock.restore();
    cleanup();
    Object.defineProperty(window, "Notification", {
      configurable: true,
      writable: true,
      value: originalNotification,
    });
  });

  it("navigates to the first pending root workspace from non-editable surfaces", async () => {
    mockWorkspaces = [
      createMockWorkspace({ name: "workspace-a", dir: "/workspaces/workspace-a" }),
      createMockWorkspace({ name: "workspace-b", dir: "/workspaces/workspace-b", needsInput: true }),
      createMockWorkspace({ name: "workspace-c", dir: "/workspaces/workspace-c", needsInput: true }),
    ];

    renderApp();

    fireEvent.keyDown(screen.getByTestId("outside-target"), {
      key: "i",
      metaKey: true,
    });

    await waitFor(() => {
      expect(screen.getByTestId("route-path").textContent).toBe("/workspaces/workspace-b/respond");
    });
  });

  it("navigates to the first pending fork workspace", async () => {
    mockWorkspaces = [
      createMockWorkspace({
        name: "root-workspace",
        dir: "/workspaces/root-workspace",
        isRoot: true,
        forks: [
          createMockFork({
            name: "root-workspace-fork",
            dir: "/workspaces/root-workspace-fork",
            needsInput: true,
          }),
        ],
      }),
      createMockWorkspace({
        name: "root-workspace-fork",
        dir: "/workspaces/root-workspace-fork",
        isFork: true,
        needsInput: true,
      }),
      createMockWorkspace({ name: "later-root", dir: "/workspaces/later-root", needsInput: true }),
    ];

    renderApp();

    fireEvent.keyDown(screen.getByTestId("outside-target"), {
      key: "i",
      ctrlKey: true,
    });

    await waitFor(() => {
      expect(screen.getByTestId("route-path").textContent).toBe("/workspaces/root-workspace-fork/respond");
    });
  });

  it("does nothing when no workspace is waiting for response", async () => {
    mockWorkspaces = [
      createMockWorkspace({ name: "workspace-a", dir: "/workspaces/workspace-a" }),
      createMockWorkspace({ name: "workspace-b", dir: "/workspaces/workspace-b" }),
    ];

    renderApp();

    fireEvent.keyDown(screen.getByTestId("outside-target"), {
      key: "i",
      metaKey: true,
    });

    await waitFor(() => {
      expect(screen.getByTestId("route-path").textContent).toBe("/");
    });
  });

  it("ignores the shortcut inside editable surfaces and editors", async () => {
    mockWorkspaces = [
      createMockWorkspace({ name: "workspace-a", dir: "/workspaces/workspace-a" }),
      createMockWorkspace({ name: "workspace-b", dir: "/workspaces/workspace-b", needsInput: true }),
    ];

    renderApp("/editable");

    const editableTargets = [
      screen.getByTestId("editable-input"),
      screen.getByTestId("editable-textarea"),
      screen.getByTestId("editable-select"),
      screen.getByTestId("editable-content"),
      screen.getByTestId("editor-surface"),
    ];

    for (const target of editableTargets) {
      if (target instanceof HTMLElement) {
        target.focus();
      }

      fireEvent.keyDown(target, {
        key: "i",
        ctrlKey: true,
      });

      await waitFor(() => {
        expect(screen.getByTestId("route-path").textContent).toBe("/editable");
      });
    }
  });
});
