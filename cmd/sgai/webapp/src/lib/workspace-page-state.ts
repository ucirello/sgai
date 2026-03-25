import { useSyncExternalStore } from "react";
import type { ApiWorkspaceEntry } from "@/types";
import type { FetchStatus } from "./factory-state";

interface WorkspacePageSnapshot {
  workspace: ApiWorkspaceEntry | null;
  fetchStatus: FetchStatus;
  lastFetchedAt: number | null;
}

interface SignalPayload {
  workspace?: string;
}

type Listener = () => void;

const POLL_INTERVAL_MS = 3000;
const SLOW_POLL_INTERVAL_MS = 10000;
const SSE_BASE_BACKOFF_MS = 1000;
const SSE_MAX_BACKOFF_MS = 30000;
const DEBOUNCE_MS = 300;

function createWorkspacePageStore(workspaceName: string, removeStore: () => void) {
  let snapshot: WorkspacePageSnapshot = {
    workspace: null,
    fetchStatus: "idle",
    lastFetchedAt: null,
  };

  const listeners: Set<Listener> = new Set();
  let pollTimerId: ReturnType<typeof setTimeout> | null = null;
  let sseSource: EventSource | null = null;
  let sseReconnectTimerId: ReturnType<typeof setTimeout> | null = null;
  let sseReconnectAttempts = 0;
  let isFetching = false;
  let isDestroyed = false;
  let isStarted = false;
  let refreshDebounceTimerId: ReturnType<typeof setTimeout> | null = null;
  let refreshRequestedWhileFetching = false;

  function isDocumentHidden(): boolean {
    return typeof document !== "undefined" && document.visibilityState === "hidden";
  }

  function shouldPoll(): boolean {
    return isDocumentHidden() || sseSource === null;
  }

  function pollIntervalMs(): number {
    return isDocumentHidden() ? SLOW_POLL_INTERVAL_MS : POLL_INTERVAL_MS;
  }

  function emitChange() {
    for (const listener of listeners) {
      listener();
    }
  }

  function updateSnapshot(partial: Partial<WorkspacePageSnapshot>) {
    const nextSnapshot = { ...snapshot, ...partial };
    if (
      nextSnapshot.workspace === snapshot.workspace
      && nextSnapshot.fetchStatus === snapshot.fetchStatus
      && nextSnapshot.lastFetchedAt === snapshot.lastFetchedAt
    ) {
      return;
    }
    snapshot = nextSnapshot;
    emitChange();
  }

  async function fetchState() {
    if (isDestroyed || !workspaceName) return;
    if (isFetching) {
      refreshRequestedWhileFetching = true;
      return;
    }

    isFetching = true;
    refreshRequestedWhileFetching = false;

    if (snapshot.workspace === null) {
      updateSnapshot({ fetchStatus: "fetching" });
    }

    try {
      const response = await fetch(
        `/api/v1/workspaces/${encodeURIComponent(workspaceName)}/state`,
      );
      if (isDestroyed) return;
      if (!response.ok) {
        updateSnapshot({ fetchStatus: snapshot.workspace === null ? "error" : "idle" });
        return;
      }

      const data = (await response.json()) as ApiWorkspaceEntry;
      if (isDestroyed) return;
      updateSnapshot({
        workspace: data,
        fetchStatus: "idle",
        lastFetchedAt: Date.now(),
      });
    } catch {
      if (!isDestroyed) {
        updateSnapshot({ fetchStatus: snapshot.workspace === null ? "error" : "idle" });
      }
    } finally {
      isFetching = false;

      if (refreshRequestedWhileFetching && !isDestroyed) {
        refreshRequestedWhileFetching = false;
        void fetchState();
      }
    }
  }

  function syncPolling() {
    if (pollTimerId !== null) {
      clearTimeout(pollTimerId);
      pollTimerId = null;
    }
    if (isDestroyed || !workspaceName || !shouldPoll()) return;
    pollTimerId = setTimeout(() => {
      pollTimerId = null;
      void fetchState();
      syncPolling();
    }, pollIntervalMs());
  }

  function handleVisibilityChange() {
    if (isDestroyed) return;
    if (!isDocumentHidden()) {
      void fetchState();
      if (sseSource === null && sseReconnectTimerId === null) {
        connectSSESignal();
      }
    }
    syncPolling();
  }

  function sseBackoffDelay(): number {
    return Math.min(SSE_BASE_BACKOFF_MS * Math.pow(2, sseReconnectAttempts), SSE_MAX_BACKOFF_MS);
  }

  function scheduleSSEReconnect() {
    if (isDestroyed) return;
    if (sseReconnectTimerId !== null) {
      clearTimeout(sseReconnectTimerId);
      sseReconnectTimerId = null;
    }
    const delay = sseBackoffDelay();
    sseReconnectAttempts++;
    sseReconnectTimerId = setTimeout(() => {
      sseReconnectTimerId = null;
      connectSSESignal();
    }, delay);
  }

  function shouldRefreshForSignal(payload: SignalPayload): boolean {
    if (!payload.workspace) {
      return false;
    }
    return payload.workspace === workspaceName || payload.workspace === snapshot.workspace?.dir;
  }

  function connectSSESignal() {
    if (sseSource !== null || isDestroyed || !workspaceName || typeof EventSource === "undefined") return;

    sseSource = new EventSource("/api/v1/signal");

    sseSource.onopen = () => {
      sseReconnectAttempts = 0;
      syncPolling();
    };

    sseSource.onerror = () => {
      if (sseSource !== null) {
        sseSource.close();
        sseSource = null;
      }
      syncPolling();
      scheduleSSEReconnect();
    };

    sseSource.addEventListener("reload", () => {
      if (!isDestroyed && snapshot.workspace === null) {
        void fetchState();
      }
    });

    sseSource.addEventListener("workspace", (event) => {
      if (isDestroyed) {
        return;
      }

      try {
        const payload = JSON.parse((event as MessageEvent<string>).data) as SignalPayload;
        if (shouldRefreshForSignal(payload)) {
          void fetchState();
        }
      } catch {
        void fetchState();
      }
    });
  }

  function start() {
    if (isStarted || isDestroyed || !workspaceName) return;
    isStarted = true;

    void fetchState();
    connectSSESignal();
    syncPolling();

    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", handleVisibilityChange);
    }
  }

  function triggerRefresh() {
    if (isDestroyed) return;

    if (refreshDebounceTimerId !== null) {
      clearTimeout(refreshDebounceTimerId);
      refreshDebounceTimerId = null;
    }

    refreshDebounceTimerId = setTimeout(() => {
      refreshDebounceTimerId = null;
      void fetchState();
      syncPolling();
    }, DEBOUNCE_MS);
  }

  function stop() {
    if (isDestroyed) {
      return;
    }

    isDestroyed = true;
    isStarted = false;

    if (pollTimerId !== null) {
      clearTimeout(pollTimerId);
      pollTimerId = null;
    }

    if (refreshDebounceTimerId !== null) {
      clearTimeout(refreshDebounceTimerId);
      refreshDebounceTimerId = null;
    }

    if (sseReconnectTimerId !== null) {
      clearTimeout(sseReconnectTimerId);
      sseReconnectTimerId = null;
    }

    if (sseSource !== null) {
      sseSource.close();
      sseSource = null;
    }

    if (typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    }

    listeners.clear();
    removeStore();
  }

  function subscribe(listener: Listener): () => void {
    listeners.add(listener);
    if (!isStarted && !isDestroyed) {
      start();
    }
    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) {
        stop();
      }
    };
  }

  function getSnapshot(): WorkspacePageSnapshot {
    return snapshot;
  }

  function getServerSnapshot(): WorkspacePageSnapshot {
    return {
      workspace: null,
      fetchStatus: "idle",
      lastFetchedAt: null,
    };
  }

  return {
    subscribe,
    getSnapshot,
    getServerSnapshot,
    stop,
    triggerRefresh,
  };
}

type WorkspacePageStore = ReturnType<typeof createWorkspacePageStore>;

const storeInstances = new Map<string, WorkspacePageStore>();

function normalizeWorkspaceName(workspaceName: string): string {
  return workspaceName.trim();
}

function getWorkspacePageStore(workspaceName: string): WorkspacePageStore {
  const normalizedName = normalizeWorkspaceName(workspaceName);
  const existingStore = storeInstances.get(normalizedName);
  if (existingStore) {
    return existingStore;
  }

  let createdStore!: WorkspacePageStore;
  createdStore = createWorkspacePageStore(normalizedName, () => {
    if (storeInstances.get(normalizedName) === createdStore) {
      storeInstances.delete(normalizedName);
    }
  });
  storeInstances.set(normalizedName, createdStore);
  return createdStore;
}

export function resetWorkspacePageStateStores(): void {
  for (const store of Array.from(storeInstances.values())) {
    store.stop();
  }
  storeInstances.clear();
}

export function triggerWorkspacePageRefresh(workspaceName: string): void {
  const normalizedName = normalizeWorkspaceName(workspaceName);
  const store = storeInstances.get(normalizedName);
  if (!store) {
    return;
  }
  store.triggerRefresh();
}

export function useWorkspacePageState(workspaceName: string): WorkspacePageSnapshot {
  const store = getWorkspacePageStore(workspaceName);
  return useSyncExternalStore(
    store.subscribe,
    store.getSnapshot,
    store.getServerSnapshot,
  );
}
