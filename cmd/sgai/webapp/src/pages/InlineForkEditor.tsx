import { useState, useCallback, useEffect, useId, useRef, useTransition } from "react";
import { useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { api, ApiError } from "@/lib/api";
import { triggerFactoryRefresh } from "@/lib/factory-state";
import { stripFrontmatter } from "@/lib/markdown-utils";
import { Loader2, GitFork } from "lucide-react";

interface InlineForkEditorProps {
  workspaceName: string;
}

const STORAGE_PREFIX = "sgai-inline-fork-";

type DraftStorageResult = {
  draft: string | null;
  error: string | null;
};

function getStorageKey(workspaceName: string): string {
  return `${STORAGE_PREFIX}${workspaceName}`;
}

function storageFailureMessage(action: "read" | "save" | "clear", error: unknown): string {
  const detail = error instanceof Error && error.message.trim().length > 0
    ? error.message
    : "sessionStorage is unavailable";

  switch (action) {
    case "read":
      return `Draft persistence is unavailable while restoring your draft: ${detail}`;
    case "save":
      return `Draft persistence is unavailable while saving your draft: ${detail}`;
    case "clear":
      return `Draft persistence is unavailable while clearing your draft: ${detail}`;
  }
}

function loadStoredDraft(workspaceName: string): DraftStorageResult {
  try {
    return {
      draft: sessionStorage.getItem(getStorageKey(workspaceName)),
      error: null,
    };
  } catch (error) {
    return {
      draft: null,
      error: storageFailureMessage("read", error),
    };
  }
}

function saveStoredDraft(workspaceName: string, content: string): string | null {
  try {
    if (content.length === 0) {
      sessionStorage.removeItem(getStorageKey(workspaceName));
      return null;
    }
    sessionStorage.setItem(getStorageKey(workspaceName), content);
    return null;
  } catch (error) {
    return storageFailureMessage("save", error);
  }
}

function clearStoredDraft(workspaceName: string): string | null {
  try {
    sessionStorage.removeItem(getStorageKey(workspaceName));
    return null;
  } catch (error) {
    return storageFailureMessage("clear", error);
  }
}

function extractForkTemplateBody(content: string | null | undefined): string {
  return stripFrontmatter(content ?? "");
}

export function InlineForkEditor({ workspaceName }: InlineForkEditorProps) {
  const navigate = useNavigate();
  const bodyEditorLabelId = useId();
  const bodyEditorHelpId = useId();
  const [content, setContent] = useState("");
  const [hasUserDraft, setHasUserDraft] = useState(false);
  const [isTemplateLoading, setIsTemplateLoading] = useState(false);
  const [templateError, setTemplateError] = useState<string | null>(null);
  const [templateRequestId, setTemplateRequestId] = useState(0);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [draftStorageError, setDraftStorageError] = useState<string | null>(null);
  const [isSubmitting, startSubmitTransition] = useTransition();
  const contentRef = useRef(content);
  const hasUserDraftRef = useRef(hasUserDraft);
  const hasUnsavedChangesRef = useRef(false);
  const forceTemplateReloadRef = useRef(false);

  const loadTemplate = useCallback(async (cancelledRef: { current: boolean }) => {
    setTemplateError(null);
    setValidationError(null);
    setSubmitError(null);

    const forceTemplateReload = forceTemplateReloadRef.current;
    forceTemplateReloadRef.current = false;

    if (!forceTemplateReload) {
      const storedDraftResult = loadStoredDraft(workspaceName);
      setDraftStorageError(storedDraftResult.error);
      if (storedDraftResult.draft !== null) {
        setIsTemplateLoading(false);
        setHasUserDraft(true);
        setContent(storedDraftResult.draft);
        return;
      }
      setHasUserDraft(false);
      setContent("");
    }

    const preserveExistingContent = forceTemplateReload && hasUserDraftRef.current && contentRef.current.trim().length > 0;
    setIsTemplateLoading(true);

    try {
      const result = await api.workspaces.forkTemplate(workspaceName);
      if (cancelledRef.current) {
        return;
      }
      setTemplateError(null);
      if (!preserveExistingContent) {
        setHasUserDraft(false);
        setContent(extractForkTemplateBody(result.content));
      }
    } catch (err) {
      if (cancelledRef.current) {
        return;
      }
      setTemplateError(err instanceof Error ? err.message : "Failed to load fork template");
    } finally {
      if (!cancelledRef.current) {
        setIsTemplateLoading(false);
      }
    }
  }, [workspaceName]);

  useEffect(() => {
    if (!workspaceName) return;
    const cancelledRef = { current: false };
    void loadTemplate(cancelledRef);
    return () => { cancelledRef.current = true; };
  }, [loadTemplate, templateRequestId, workspaceName]);

  const bodyText = content.trim();
  const isBodyEmpty = bodyText.length === 0;

  useEffect(() => {
    contentRef.current = content;
    hasUserDraftRef.current = hasUserDraft;
    hasUnsavedChangesRef.current = hasUserDraft && bodyText.length > 0;
    if (!workspaceName || !hasUserDraft) {
      return;
    }
    if (content.length > 0) {
      setDraftStorageError(saveStoredDraft(workspaceName, content));
      return;
    }
    setDraftStorageError(clearStoredDraft(workspaceName));
  }, [workspaceName, content, bodyText, hasUserDraft]);

  useEffect(() => {
    function handleBeforeUnload(event: BeforeUnloadEvent) {
      if (!hasUnsavedChangesRef.current) {
        return;
      }
      event.preventDefault();
      event.returnValue = "";
    }

    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload);
    };
  }, []);

  const handleContentChange = useCallback((value: string | undefined) => {
    const newValue = value ?? "";
    setHasUserDraft(true);
    setContent(newValue);
    const newBody = newValue.trim();
    if (newBody.length > 0) {
      setValidationError(null);
    }
  }, []);

  const handleRetryTemplate = useCallback(() => {
    forceTemplateReloadRef.current = true;
    setTemplateError(null);
    setTemplateRequestId((current) => current + 1);
  }, []);

  const handleSubmit = useCallback(() => {
    if (isBodyEmpty) {
      setValidationError("Please write a goal description");
      return;
    }
    setValidationError(null);
    setSubmitError(null);
    startSubmitTransition(async () => {
      try {
        const result = await api.workspaces.fork(workspaceName, content);
        setDraftStorageError(clearStoredDraft(workspaceName));
        setHasUserDraft(false);
        hasUnsavedChangesRef.current = false;
        triggerFactoryRefresh();
        navigate(`/workspaces/${encodeURIComponent(result.name)}/progress`);
      } catch (err) {
        if (err instanceof ApiError) {
          setSubmitError(err.message);
        } else {
          setSubmitError("Failed to create fork");
        }
      }
    });
  }, [content, isBodyEmpty, navigate, workspaceName]);

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-lg font-semibold mb-1">New Task</h3>
        <p className="text-sm text-muted-foreground">
          Write the task body for your new fork. A fork will be created automatically.
        </p>
        <p className="mt-1 text-sm text-muted-foreground">
          Root frontmatter and the inherited title are copied automatically; only the body below is submitted.
        </p>
      </div>

      {submitError && (
        <Alert className="border-destructive/50 text-destructive">
          <AlertDescription>{submitError}</AlertDescription>
        </Alert>
      )}

      {templateError && (
        <Alert className="border-destructive/50 text-destructive" role="alert">
          <AlertDescription className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span>{templateError}</span>
            <Button type="button" variant="outline" onClick={handleRetryTemplate} disabled={isTemplateLoading || isSubmitting}>
              Retry template load
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {draftStorageError && (
        <Alert className="border-destructive/50 text-destructive" role="alert">
          <AlertDescription>{draftStorageError}</AlertDescription>
        </Alert>
      )}

      {isTemplateLoading && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground" role="status" aria-live="polite">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading fork template...
        </div>
      )}

      <div className="space-y-2" role="group" aria-labelledby={bodyEditorLabelId} aria-describedby={bodyEditorHelpId}>
        <Label id={bodyEditorLabelId} className="text-sm font-medium">Task body</Label>
        <p id={bodyEditorHelpId} className="text-sm text-muted-foreground">
          Only the body you write here is submitted for the fork.
        </p>
        <MarkdownEditor
          value={content}
          onChange={handleContentChange}
          onSubmitShortcut={handleSubmit}
          minHeight={300}
          defaultHeight={400}
          disabled={isTemplateLoading || isSubmitting}
          placeholder={isTemplateLoading ? "Loading fork template..." : "Describe the task body for this fork..."}
          ariaLabel="Task body"
        />
      </div>

      {validationError && (
        <p className="text-sm text-destructive" role="alert">
          {validationError}
        </p>
      )}

      <div className="flex justify-end">
        <Button
          type="button"
          onClick={handleSubmit}
          disabled={isTemplateLoading || isSubmitting || isBodyEmpty}
        >
          {isSubmitting ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Creating Fork...
            </>
          ) : (
            <>
              <GitFork className="mr-2 h-4 w-4" />
              Create Fork
            </>
          )}
        </Button>
      </div>
    </div>
  );
}
