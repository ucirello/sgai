import { describe, it, expect, beforeEach, afterEach, mock, vi } from "bun:test";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { createElement } from "react";
import {
  resetWorkspacePageStateStores,
  triggerWorkspacePageRefresh,
  useWorkspacePageState,
} from "../workspace-page-state";

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function createResponse(body: unknown): Response {
  return {
    ok: true,
    status: 200,
    json: () => Promise.resolve(body),
  } as Response;
}

type EventListenerMap = Map<string, Set<(event: MessageEvent<string>) => void>>;

class MockEventSource {
  static instances: MockEventSource[] = [];

  onopen: ((this: EventSource, event: Event) => void) | null = null;
  onerror: ((this: EventSource, event: Event) => void) | null = null;

  private listeners: EventListenerMap = new Map();

  constructor(public readonly url: string) {
    MockEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  close(): void {}

  emit(type: string, payload: unknown): void {
    const event = { data: JSON.stringify(payload) } as MessageEvent<string>;
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }

  static reset(): void {
    MockEventSource.instances = [];
  }
}

async function advanceTimersAndFlush(timeMs: number): Promise<void> {
  await act(async () => {
    vi.advanceTimersByTime(timeMs);
    await Promise.resolve();
    await Promise.resolve();
  });
}

const mockFetch = mock(() => Promise.resolve(createResponse({ name: "test-workspace", dir: "/tmp/test-workspace" })));

function WorkspacePageProbe({ workspaceName }: { workspaceName: string }) {
  const { workspace, fetchStatus } = useWorkspacePageState(workspaceName);

  return createElement(
    "div",
    null,
    createElement("span", { "data-testid": "workspace-name" }, workspace?.name ?? ""),
    createElement("span", { "data-testid": "fetch-status" }, fetchStatus),
  );
}

beforeEach(() => {
  globalThis.fetch = mockFetch as unknown as typeof fetch;
  globalThis.EventSource = MockEventSource as unknown as typeof EventSource;
  mockFetch.mockClear();
  mockFetch.mockImplementation(() => Promise.resolve(createResponse({ name: "test-workspace", dir: "/tmp/test-workspace" })));
  MockEventSource.reset();
});

afterEach(() => {
  cleanup();
  resetWorkspacePageStateStores();
  MockEventSource.reset();
  vi.useRealTimers();
});

describe("workspace-page-state store", () => {
  it("queues a second refresh request while the current fetch is still running", async () => {
    const firstResponse = deferredValue<Response>();

    render(createElement(WorkspacePageProbe, { workspaceName: "test-workspace" }));

    await waitFor(() => {
      expect(screen.getByTestId("workspace-name").textContent).toBe("test-workspace");
    });

    mockFetch.mockClear();
    mockFetch
      .mockImplementationOnce(() => firstResponse.promise)
      .mockImplementationOnce(() => Promise.resolve(createResponse({ name: "test-workspace", dir: "/tmp/test-workspace", status: "fresh" })));

    vi.useFakeTimers();

    triggerWorkspacePageRefresh("test-workspace");
    await advanceTimersAndFlush(300);

    expect(mockFetch.mock.calls.length).toBe(1);

    triggerWorkspacePageRefresh("test-workspace");
    await advanceTimersAndFlush(300);
    expect(mockFetch.mock.calls.length).toBe(1);

    firstResponse.resolve(createResponse({ name: "test-workspace", dir: "/tmp/test-workspace" }));

    await waitFor(() => {
      expect(mockFetch.mock.calls.length).toBe(2);
    });
  });

  it("keeps the existing snapshot idle during a background refresh", async () => {
    const secondResponse = deferredValue<Response>();

    mockFetch
      .mockImplementationOnce(() => Promise.resolve(createResponse({ name: "test-workspace", dir: "/tmp/test-workspace" })))
      .mockImplementationOnce(() => secondResponse.promise);

    render(createElement(WorkspacePageProbe, { workspaceName: "test-workspace" }));

    await waitFor(() => {
      expect(screen.getByTestId("workspace-name").textContent).toBe("test-workspace");
      expect(screen.getByTestId("fetch-status").textContent).toBe("idle");
    });

    triggerWorkspacePageRefresh("test-workspace");

    await waitFor(() => {
      expect(mockFetch.mock.calls.length).toBe(2);
    });

    expect(screen.getByTestId("fetch-status").textContent).toBe("idle");

    secondResponse.resolve(createResponse({ name: "test-workspace", dir: "/tmp/test-workspace", status: "updated" }));

    await waitFor(() => {
      expect(screen.getByTestId("fetch-status").textContent).toBe("idle");
    });
  });

  it("stops polling and ignores refresh requests after the last subscriber unsubscribes", async () => {
    vi.useFakeTimers();

    const { unmount } = render(createElement(WorkspacePageProbe, { workspaceName: "test-workspace" }));

    await waitFor(() => {
      expect(screen.getByTestId("workspace-name").textContent).toBe("test-workspace");
    });

    expect(mockFetch.mock.calls.length).toBe(1);

    unmount();

    triggerWorkspacePageRefresh("test-workspace");

    await advanceTimersAndFlush(300);
    await advanceTimersAndFlush(3000);

    expect(mockFetch.mock.calls.length).toBe(1);
  });

  it("ignores generic reload SSE events after the workspace page has loaded", async () => {
    render(createElement(WorkspacePageProbe, { workspaceName: "test-workspace" }));

    await waitFor(() => {
      expect(screen.getByTestId("workspace-name").textContent).toBe("test-workspace");
    });

    const source = MockEventSource.instances[0];
    expect(source).toBeDefined();

    source.emit("reload", {});
    expect(mockFetch.mock.calls.length).toBe(1);

    source.emit("workspace", { workspace: "/tmp/test-workspace" });

    await waitFor(() => {
      expect(mockFetch.mock.calls.length).toBe(2);
    });
  });

  it("refreshes only for matching workspace-dir SSE events and keeps state fetches headerless", async () => {
    render(createElement(WorkspacePageProbe, { workspaceName: "test-workspace" }));

    await waitFor(() => {
      expect(screen.getByTestId("workspace-name").textContent).toBe("test-workspace");
    });

    const source = MockEventSource.instances[0];
    expect(source).toBeDefined();

    source.emit("workspace", { workspace: "/tmp/other-workspace" });
    expect(mockFetch.mock.calls.length).toBe(1);

    source.emit("workspace", { workspace: "/tmp/test-workspace" });

    await waitFor(() => {
      expect(mockFetch.mock.calls.length).toBe(2);
    });

    expect(mockFetch.mock.calls[1]).toHaveLength(1);
  });

  it("stops visible polling once SSE is connected and resumes fallback polling after disconnect", async () => {
    vi.useFakeTimers();

    render(createElement(WorkspacePageProbe, { workspaceName: "test-workspace" }));
    await advanceTimersAndFlush(0);

    expect(mockFetch.mock.calls.length).toBe(1);

    const source = MockEventSource.instances[0];
    expect(source).toBeDefined();

    source.onopen?.call(source as unknown as EventSource, new Event("open"));

    await advanceTimersAndFlush(3000);
    expect(mockFetch.mock.calls.length).toBe(1);

    source.onerror?.call(source as unknown as EventSource, new Event("error"));

    await advanceTimersAndFlush(3000);
    expect(mockFetch.mock.calls.length).toBe(2);
  });

  it("does not fall back to visible polling while an idle EventSource still exists", async () => {
    vi.useFakeTimers();

    render(createElement(WorkspacePageProbe, { workspaceName: "test-workspace" }));
    await advanceTimersAndFlush(0);

    expect(mockFetch.mock.calls.length).toBe(1);
    expect(MockEventSource.instances.length).toBe(1);

    await advanceTimersAndFlush(9000);

    expect(mockFetch.mock.calls.length).toBe(1);
    expect(MockEventSource.instances.length).toBe(1);
  });
});
