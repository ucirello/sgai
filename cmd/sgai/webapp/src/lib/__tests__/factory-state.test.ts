import { describe, it, expect, beforeEach, afterEach, mock, vi } from "bun:test";
import { act, cleanup, render } from "@testing-library/react";
import { createElement } from "react";
import { resetFactoryStateStore, triggerFactoryRefresh, useFactoryState } from "../factory-state";

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

async function emitSignalAndFlush(source: MockEventSource, type: string, payload: unknown): Promise<void> {
  await act(async () => {
    source.emit(type, payload);
    await Promise.resolve();
    await Promise.resolve();
  });
}

const mockFetch = mock(() =>
  Promise.resolve(createResponse({ workspaces: [{ name: "test-workspace", dir: "/tmp/test-workspace" }] }))
);

function FactoryStateProbe() {
  const { workspaces, fetchStatus } = useFactoryState();

  return createElement(
    "div",
    null,
    createElement("span", { "data-testid": "workspace-count" }, String(workspaces.length)),
    createElement("span", { "data-testid": "fetch-status" }, fetchStatus),
  );
}

beforeEach(() => {
  globalThis.fetch = mockFetch as unknown as typeof fetch;
  globalThis.EventSource = MockEventSource as unknown as typeof EventSource;
  mockFetch.mockClear();
  mockFetch.mockImplementation(() =>
    Promise.resolve(createResponse({ workspaces: [{ name: "test-workspace", dir: "/tmp/test-workspace" }] }))
  );
  MockEventSource.reset();
});

afterEach(() => {
  cleanup();
  resetFactoryStateStore();
  MockEventSource.reset();
  vi.useRealTimers();
});

describe("factory-state store", () => {
  it("ignores page-only workspace SSE events and refreshes on reload events", async () => {
    vi.useFakeTimers();

    render(createElement(FactoryStateProbe));
    await advanceTimersAndFlush(0);

    expect(mockFetch.mock.calls.length).toBe(1);

    const source = MockEventSource.instances[0];
    expect(source).toBeDefined();

    source.onopen?.call(source as unknown as EventSource, new Event("open"));

    await emitSignalAndFlush(source, "workspace", { workspace: "test-workspace" });

    expect(mockFetch.mock.calls.length).toBe(1);

    await emitSignalAndFlush(source, "reload", { workspace: "test-workspace" });

    expect(mockFetch.mock.calls.length).toBe(2);
  });

  it("stops visible polling once SSE is connected and resumes fallback polling after disconnect", async () => {
    vi.useFakeTimers();

    render(createElement(FactoryStateProbe));
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

    render(createElement(FactoryStateProbe));
    await advanceTimersAndFlush(0);

    expect(mockFetch.mock.calls.length).toBe(1);
    expect(MockEventSource.instances.length).toBe(1);

    await advanceTimersAndFlush(9000);

    expect(mockFetch.mock.calls.length).toBe(1);
    expect(MockEventSource.instances.length).toBe(1);
  });

  it("stops polling and ignores refresh requests after the last subscriber unsubscribes", async () => {
    vi.useFakeTimers();

    const { unmount } = render(createElement(FactoryStateProbe));
    await advanceTimersAndFlush(0);

    expect(mockFetch.mock.calls.length).toBe(1);

    unmount();

    triggerFactoryRefresh();

    await advanceTimersAndFlush(300);
    await advanceTimersAndFlush(3000);

    expect(mockFetch.mock.calls.length).toBe(1);
  });
});
