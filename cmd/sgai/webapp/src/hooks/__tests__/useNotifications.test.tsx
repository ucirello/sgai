import { afterEach, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import * as factoryStateModule from "@/lib/factory-state";
import type { ApiWorkspaceEntry } from "@/types";
import { useNotifications } from "../useNotifications";

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
    agentTodos: [],
    log: [],
    ...overrides,
  };
}

interface RecordedNotification {
  title: string;
  options: NotificationOptions | undefined;
}

class MockNotification {
  static permission: NotificationPermission = "granted";

  onclick: (() => void) | null = null;

  constructor(title: string, options?: NotificationOptions) {
    recordedNotifications.push({ title, options });
  }
}

let mockState: factoryStateModule.FactoryStateSnapshot;
let recordedNotifications: RecordedNotification[];

const originalNotification = window.Notification;

describe("useNotifications", () => {
  beforeEach(() => {
    recordedNotifications = [];
    mockState = {
      workspaces: [],
      fetchStatus: "idle",
      lastFetchedAt: null,
    };

    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => mockState);

    Object.defineProperty(window, "Notification", {
      configurable: true,
      writable: true,
      value: MockNotification,
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

  it("uses the computed workspace display label in the notification body", async () => {
    const { rerender } = renderHook(() => useNotifications());

    mockState = {
      ...mockState,
      lastFetchedAt: Date.now(),
      workspaces: [
        createMockWorkspace({
          name: "workspace-name",
          dir: "/workspaces/workspace-name",
          needsInput: true,
          title: "Workspace Title",
          computedTitle: "Computed Workspace Label",
        }),
      ],
    };

    rerender();

    await waitFor(() => {
      expect(recordedNotifications).toHaveLength(1);
    });

    expect(recordedNotifications[0]).toEqual({
      title: "Approval Needed",
      options: {
        body: "Workspace Computed Workspace Label needs your input",
        tag: "/workspaces/workspace-name",
      },
    });
  });

  it("tracks duplicate-name workspaces independently by directory using the UI label", async () => {
    const { rerender } = renderHook(() => useNotifications());

    mockState = {
      ...mockState,
      lastFetchedAt: 1,
      workspaces: [
        createMockWorkspace({
          name: "shared-workspace",
          dir: "/two/shared-workspace",
          needsInput: false,
          title: "Shared Workspace",
        }),
        createMockWorkspace({
          name: "shared-workspace",
          dir: "/one/shared-workspace",
          needsInput: true,
          title: "Shared Workspace",
        }),
      ],
    };

    rerender();

    await waitFor(() => {
      expect(recordedNotifications).toHaveLength(1);
    });

    expect(recordedNotifications[0]).toEqual({
      title: "Approval Needed",
      options: {
        body: "Workspace Shared Workspace · one needs your input",
        tag: "/one/shared-workspace",
      },
    });

    mockState = {
      ...mockState,
      lastFetchedAt: 2,
      workspaces: [
        createMockWorkspace({
          name: "shared-workspace",
          dir: "/one/shared-workspace",
          needsInput: false,
          title: "Shared Workspace",
        }),
        createMockWorkspace({
          name: "shared-workspace",
          dir: "/two/shared-workspace",
          needsInput: true,
          title: "Shared Workspace",
        }),
      ],
    };

    rerender();

    await waitFor(() => {
      expect(recordedNotifications).toHaveLength(2);
    });

    expect(recordedNotifications).toEqual([
      {
        title: "Approval Needed",
        options: {
          body: "Workspace Shared Workspace · one needs your input",
          tag: "/one/shared-workspace",
        },
      },
      {
        title: "Approval Needed",
        options: {
          body: "Workspace Shared Workspace · two needs your input",
          tag: "/two/shared-workspace",
        },
      },
    ]);
  });
});
