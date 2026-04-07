import { useState, useEffect, useCallback, useRef } from "react";
import { api, ApiError } from "@/lib/api";
import type { ApiAdhocResponse, ApiModelsResponse } from "@/types";

const MAX_HISTORY_ENTRIES = 20;
const POLL_INTERVAL_MS = 2000;

function storageKey(workspaceName: string, suffix: string): string {
  return `adhoc-${suffix}-${workspaceName}`;
}

function readLocalStorage<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw === null) return fallback;
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

function writeLocalStorage<T>(key: string, value: T): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // quota exceeded or unavailable — silently ignore
  }
}

export interface UseAdhocRunOptions {
  workspaceName: string;
  currentModel?: string;
  /** When true, skip fetching model list (AdhocOutput uses a text input) */
  skipModelsFetch?: boolean;
  /** When true, skip background adhoc status fetches for dir-qualified duplicate routes */
  skipStatusFetch?: boolean;
  /** When set, block basename-only adhoc mutations and surface the reason to the UI */
  disableMutationsReason?: string;
}

export interface UseAdhocRunResult {
  models: ApiModelsResponse | null;
  modelsLoading: boolean;
  modelsError: Error | null;
  selectedModel: string;
  setSelectedModel: (model: string) => void;
  prompt: string;
  setPrompt: (prompt: string) => void;
  output: string;
  isRunning: boolean;
  runError: string | null;
  startRun: (promptOverride?: string, modelOverride?: string, workspaceOverride?: string) => void;
  startActionRun: (actionName: string, variables?: Record<string, string>, workspaceOverride?: string) => void;
  stopRun: () => void;
  handleSubmit: (event: React.FormEvent) => void;
  handleKeyDown: (event: React.KeyboardEvent) => void;
  outputRef: React.RefObject<HTMLPreElement | null>;
  promptHistory: string[];
  selectFromHistory: (entry: string) => void;
  clearHistory: () => void;
}

export function useAdhocRun({
  workspaceName,
  currentModel,
  skipModelsFetch = false,
  skipStatusFetch = false,
  disableMutationsReason,
}: UseAdhocRunOptions): UseAdhocRunResult {
  const [models, setModels] = useState<ApiModelsResponse | null>(null);
  const [modelsLoading, setModelsLoading] = useState(!skipModelsFetch);
  const [modelsError, setModelsError] = useState<Error | null>(null);

  const [selectedModel, setSelectedModelState] = useState(() =>
    readLocalStorage(storageKey(workspaceName, "model"), ""),
  );
  const [prompt, setPromptState] = useState(() =>
    readLocalStorage(storageKey(workspaceName, "prompt"), ""),
  );
  const [output, setOutput] = useState("");
  const [isRunning, setIsRunning] = useState(false);
  const [runError, setRunError] = useState<string | null>(null);
  const [runWorkspaceName, setRunWorkspaceName] = useState(workspaceName);
  const [promptHistory, setPromptHistory] = useState<string[]>(() =>
    readLocalStorage<string[]>(storageKey(workspaceName, "history"), []),
  );
  const normalizedDisableMutationsReason = disableMutationsReason?.trim() ?? "";

  const outputRef = useRef<HTMLPreElement | null>(null);
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const mountedRef = useRef(true);
  const previousWorkspaceNameRef = useRef(workspaceName);

  // -- persist helpers --

  const setSelectedModel = useCallback(
    (model: string) => {
      setSelectedModelState(model);
      writeLocalStorage(storageKey(workspaceName, "model"), model);
    },
    [workspaceName],
  );

  const setPrompt = useCallback(
    (value: string) => {
      setPromptState(value);
      writeLocalStorage(storageKey(workspaceName, "prompt"), value);
    },
    [workspaceName],
  );

  const addToHistory = useCallback(
    (entry: string) => {
      setPromptHistory((prev) => {
        const deduped = prev.filter((p) => p !== entry);
        const next = [entry, ...deduped].slice(0, MAX_HISTORY_ENTRIES);
        writeLocalStorage(storageKey(workspaceName, "history"), next);
        return next;
      });
    },
    [workspaceName],
  );

  const clearHistory = useCallback(() => {
    setPromptHistory([]);
    writeLocalStorage(storageKey(workspaceName, "history"), []);
  }, [workspaceName]);

  const selectFromHistory = useCallback(
    (entry: string) => {
      setPrompt(entry);
    },
    [setPrompt],
  );

  // -- polling --

  const stopPolling = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      stopPolling();
    };
  }, [stopPolling]);

  // reset running state when workspace changes
  useEffect(() => {
    if (previousWorkspaceNameRef.current === workspaceName) {
      return;
    }

    previousWorkspaceNameRef.current = workspaceName;
    stopPolling();
    setRunWorkspaceName(workspaceName);
    setIsRunning(false);
    setOutput("");
    setRunError(null);
  }, [workspaceName, stopPolling]);

  // auto-scroll output
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [output]);

  // -- fetch models --

  useEffect(() => {
    if (skipModelsFetch || !workspaceName) return;

    let cancelled = false;
    setModels(null);
    setModelsLoading(true);
    setModelsError(null);

    api.models
      .list(workspaceName)
      .then((response) => {
        if (!cancelled) {
          setModels(response);
          setModelsLoading(false);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setModelsError(err instanceof Error ? err : new Error(String(err)));
          setModelsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workspaceName, skipModelsFetch]);

  // -- auto-select default model --

  useEffect(() => {
    if (!models || selectedModel) return;
    const fallbackModel = models.defaultModel ?? currentModel;
    if (fallbackModel && models.models.some((m) => m.id === fallbackModel)) {
      setSelectedModel(fallbackModel);
    }
  }, [models, selectedModel, currentModel, setSelectedModel]);

  const startStatusPolling = useCallback((statusWorkspaceName: string) => {
    pollTimerRef.current = setInterval(async () => {
      try {
        const poll = await api.workspaces.adhocStatus(statusWorkspaceName);
        if (!mountedRef.current) return;
        if (poll.output) setOutput(poll.output);
        if (!poll.running) { stopPolling(); setIsRunning(false); }
      } catch {
        stopPolling();
        setIsRunning(false);
      }
    }, POLL_INTERVAL_MS);
  }, [stopPolling]);

  useEffect(() => {
    if (skipStatusFetch) return;
    if (!workspaceName || runWorkspaceName !== workspaceName) return;
    let cancelled = false;
    api.workspaces.adhocStatus(workspaceName).then((status) => {
      if (cancelled) return;
      if (status.output) setOutput(status.output);
      if (status.running) {
        setRunWorkspaceName(workspaceName);
        setIsRunning(true);
        startStatusPolling(workspaceName);
      }
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [workspaceName, runWorkspaceName, skipStatusFetch, startStatusPolling]);

  const executeRun = useCallback(
    async ({
      targetWorkspaceName,
      historyEntry,
      errorMessage,
      execute,
    }: {
      targetWorkspaceName: string;
      historyEntry?: string;
      errorMessage: string;
      execute: (workspace: string) => Promise<ApiAdhocResponse>;
    }) => {
      const trimmedWorkspaceName = targetWorkspaceName.trim();
      if (!trimmedWorkspaceName || isRunning) return;

      if (normalizedDisableMutationsReason) {
        stopPolling();
        setIsRunning(false);
        setRunError(normalizedDisableMutationsReason);
        return;
      }

      stopPolling();
      setRunWorkspaceName(trimmedWorkspaceName);
      setIsRunning(true);
      setRunError(null);
      setOutput("");
      if (historyEntry) {
        addToHistory(historyEntry);
      }

      try {
        const result = await execute(trimmedWorkspaceName);
        if (result.output) setOutput(result.output);
        if (!result.running) {
          setIsRunning(false);
          return;
        }
        startStatusPolling(trimmedWorkspaceName);
      } catch (err) {
        if (err instanceof ApiError) {
          setRunError(err.message);
        } else {
          setRunError(errorMessage);
        }
        setIsRunning(false);
      }
    },
    [addToHistory, isRunning, normalizedDisableMutationsReason, startStatusPolling, stopPolling],
  );

  const startRun = useCallback(
    async (promptOverride?: string, modelOverride?: string, workspaceOverride?: string) => {
      const trimmedPrompt = (promptOverride ?? prompt).trim();
      const trimmedModel = (modelOverride ?? selectedModel).trim();
      const targetWorkspaceName = workspaceOverride ?? workspaceName;
      if (!targetWorkspaceName || !trimmedPrompt || !trimmedModel) return;

      await executeRun({
        targetWorkspaceName,
        historyEntry: trimmedPrompt,
        errorMessage: "Failed to execute ad-hoc prompt",
        execute: (targetWorkspace) => api.workspaces.adhoc(targetWorkspace, trimmedPrompt, trimmedModel),
      });
    },
    [executeRun, prompt, selectedModel, workspaceName],
  );

  const startActionRun = useCallback(
    async (actionName: string, variables: Record<string, string> = {}, workspaceOverride?: string) => {
      const trimmedActionName = actionName.trim();
      const targetWorkspaceName = workspaceOverride ?? workspaceName;
      if (!targetWorkspaceName || !trimmedActionName) return;

      await executeRun({
        targetWorkspaceName,
        errorMessage: "Failed to execute action",
        execute: (targetWorkspace) => api.workspaces.actionRun(targetWorkspace, {
          name: trimmedActionName,
          variables,
        }),
      });
    },
    [executeRun, workspaceName],
  );

  const stopRun = useCallback(async () => {
    if (!runWorkspaceName || !isRunning) return;

    if (normalizedDisableMutationsReason) {
      stopPolling();
      setIsRunning(false);
      setRunError(normalizedDisableMutationsReason);
      return;
    }

    try {
      await api.workspaces.adhocStop(runWorkspaceName);
      stopPolling();
      setIsRunning(false);
      setOutput((prev) => (prev ? prev + "\n\nStopped." : "Stopped."));
    } catch (err) {
      if (err instanceof ApiError) {
        setRunError(err.message);
      } else {
        setRunError("Failed to stop ad-hoc prompt");
      }
    }
  }, [isRunning, normalizedDisableMutationsReason, runWorkspaceName, stopPolling]);

  const handleSubmit = useCallback(
    (event: React.FormEvent) => {
      event.preventDefault();
      startRun();
    },
    [startRun],
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        startRun();
      }
    },
    [startRun],
  );

  return {
    models,
    modelsLoading,
    modelsError,
    selectedModel,
    setSelectedModel,
    prompt,
    setPrompt,
    output,
    isRunning,
    runError,
    startRun,
    startActionRun,
    stopRun,
    handleSubmit,
    handleKeyDown,
    outputRef,
    promptHistory,
    selectFromHistory,
    clearHistory,
  };
}
