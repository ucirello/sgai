import { useState, useEffect, useCallback, useRef } from "react";
import { api, ApiError } from "@/lib/api";
import { duplicateRouteResponseSubmissionDisabledReason } from "@/lib/duplicate-route-mutations";
import { useWorkspacePageState } from "@/lib/workspace-page-state";
import type { ApiPendingQuestionResponse, ApiWorkspaceEntry } from "@/types";

interface StoredResponseState {
  selections: Record<string, string[]>;
  otherText: string;
  promptToken: string;
}

function getStorageKey(prefix: string, workspaceStorageKey: string): string {
  return `${prefix}${workspaceStorageKey}`;
}

function loadStoredState(prefix: string, workspaceStorageKey: string): StoredResponseState | null {
  try {
    const stored = sessionStorage.getItem(getStorageKey(prefix, workspaceStorageKey));
    if (stored) {
      return JSON.parse(stored) as StoredResponseState;
    }
  } catch {
    // Ignore parse errors
  }
  return null;
}

function saveStoredState(prefix: string, workspaceStorageKey: string, state: StoredResponseState): void {
  try {
    sessionStorage.setItem(getStorageKey(prefix, workspaceStorageKey), JSON.stringify(state));
  } catch {
    // Ignore storage errors
  }
}

function clearStoredState(prefix: string, workspaceStorageKey: string): void {
  try {
    sessionStorage.removeItem(getStorageKey(prefix, workspaceStorageKey));
  } catch {
    // Ignore
  }
}

interface UseResponseFormOptions {
  workspaceName: string;
  workspaceDir?: string;
  storagePrefix: string;
  active: boolean;
  onQuestionMissing?: () => void;
  onSubmitSuccess?: () => void;
}

interface UseResponseFormReturn {
  question: ApiPendingQuestionResponse | null;
  workspaceDetail: ApiWorkspaceEntry | null;
  loading: boolean;
  error: Error | null;
  submitting: boolean;
  submitError: string | null;
  submitDisabledReason: string | null;
  selections: Record<string, string[]>;
  otherText: string;
  setOtherText: (text: string) => void;
  handleChoiceToggle: (questionIndex: number, choice: string, multiSelect: boolean) => void;
  handleSubmit: (e: React.FormEvent) => void;
}

export function useResponseForm({
  workspaceName,
  workspaceDir,
  storagePrefix,
  active,
  onQuestionMissing,
  onSubmitSuccess,
}: UseResponseFormOptions): UseResponseFormReturn {
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [selections, setSelections] = useState<Record<string, string[]>>({});
  const [otherText, setOtherText] = useState("");
  const hasUnsavedChangesRef = useRef(false);
  const previousPromptTokenRef = useRef<string | null>(null);

  const normalizedWorkspaceDir = workspaceDir?.trim() ?? "";
  const submitDisabledReason = normalizedWorkspaceDir
    ? duplicateRouteResponseSubmissionDisabledReason
    : null;
  const workspaceStorageKey = normalizedWorkspaceDir ? `${workspaceName}|${normalizedWorkspaceDir}` : workspaceName;
  const { workspace, fetchStatus } = useWorkspacePageState(
    normalizedWorkspaceDir
      ? { name: workspaceName, dir: normalizedWorkspaceDir }
      : workspaceName,
  );
  const question = workspace?.pendingQuestion ?? null;
  const promptToken = question?.promptToken ?? null;
  const loading = fetchStatus === "fetching" && workspace === null;
  const error: Error | null = fetchStatus === "error" && workspace === null
    ? new Error("Failed to load workspace state")
    : null;

  useEffect(() => {
    if (!active || !workspaceName) return;

    if (question === null && workspace !== null) {
      onQuestionMissing?.();
      return;
    }

    if (promptToken === null) return;

    if (previousPromptTokenRef.current !== promptToken) {
      previousPromptTokenRef.current = promptToken;
      const stored = loadStoredState(storagePrefix, workspaceStorageKey);
      if (stored && stored.promptToken === promptToken) {
        setSelections(stored.selections);
        setOtherText(stored.otherText);
      } else {
        setSelections({});
        setOtherText("");
      }
    }
  }, [active, workspaceStorageKey, storagePrefix, promptToken, workspace, onQuestionMissing]);

  useEffect(() => {
    if (promptToken === null) return;

    const hasSelections = Object.values(selections).some((s) => s.length > 0);
    const hasText = otherText.trim().length > 0;
    hasUnsavedChangesRef.current = hasSelections || hasText;

    saveStoredState(storagePrefix, workspaceStorageKey, {
      selections,
      otherText,
      promptToken,
    });
  }, [selections, otherText, promptToken, workspaceStorageKey, storagePrefix]);

  useEffect(() => {
    function handleBeforeUnload(e: BeforeUnloadEvent) {
      if (hasUnsavedChangesRef.current && active) {
        e.preventDefault();
      }
    }

    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload);
    };
  }, [active]);

  const handleChoiceToggle = useCallback(
    (questionIndex: number, choice: string, multiSelect: boolean) => {
      setSelections((prev) => {
        const key = String(questionIndex);
        const current = prev[key] ?? [];

        if (multiSelect) {
          const updated = current.includes(choice)
            ? current.filter((c) => c !== choice)
            : [...current, choice];
          return { ...prev, [key]: updated };
        }

        return { ...prev, [key]: [choice] };
      });
    },
    [],
  );

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();

      if (!question || submitting) return;

      if (submitDisabledReason) {
        setSubmitError(submitDisabledReason);
        return;
      }

      setSubmitting(true);
      setSubmitError(null);

      const allSelectedChoices: string[] = [];
      for (const key of Object.keys(selections)) {
        allSelectedChoices.push(...selections[key]);
      }

      try {
        await api.workspaces.respond(workspaceName, {
          promptToken: question.promptToken,
          answer: otherText.trim(),
          selectedChoices: allSelectedChoices,
        });

        clearStoredState(storagePrefix, workspaceStorageKey);
        hasUnsavedChangesRef.current = false;
        onSubmitSuccess?.();
      } catch (err: unknown) {
        if (err instanceof ApiError && err.status === 409) {
          setSubmitError("This question has expired. The agent may have moved on.");
        } else {
          setSubmitError(
            err instanceof Error ? err.message : "Failed to submit response",
          );
        }
      } finally {
        setSubmitting(false);
      }
    },
    [question, submitting, submitDisabledReason, selections, otherText, workspaceName, workspaceStorageKey, storagePrefix, onSubmitSuccess],
  );

  return {
    question,
    workspaceDetail: workspace,
    loading,
    error,
    submitting,
    submitError,
    submitDisabledReason,
    selections,
    otherText,
    setOtherText,
    handleChoiceToggle,
    handleSubmit,
  };
}
