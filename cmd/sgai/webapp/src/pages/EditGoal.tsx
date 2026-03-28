import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { FocusableTooltipText } from "@/components/FocusableTooltipText";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { api, ApiError } from "@/lib/api";
import { triggerFactoryRefresh, useFactoryState } from "@/lib/factory-state";
import { getRepositoryTitle } from "@/lib/repository-title";
import { resolveWorkspaceByName } from "@/lib/workspace-identity";
import { ArrowLeft, Save, Loader2, Check } from "lucide-react";
import { Link } from "react-router";

export function EditGoal(): JSX.Element {
  const { name: workspaceRouteName = "" } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const [content, setContent] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [loadAttempt, setLoadAttempt] = useState(0);
  const redirectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const loadedGoalTargetRef = useRef<string | null>(null);
  const routeErrorRef = useRef<HTMLDivElement | null>(null);
  const goalErrorRef = useRef<HTMLDivElement | null>(null);
  const editorSurfaceRef = useRef<HTMLDivElement | null>(null);
  const backLinkRef = useRef<HTMLAnchorElement | null>(null);
  const titleRef = useRef<HTMLHeadingElement | null>(null);
  const goalRequestRef = useRef<{
    target: string;
    promise: ReturnType<typeof api.workspaces.getGoal>;
  } | null>(null);
  const { workspaces, fetchStatus, lastFetchedAt } = useFactoryState();
  const workspace = useMemo(
    () => resolveWorkspaceByName(workspaces, workspaceRouteName),
    [workspaces, workspaceRouteName],
  );
  const workspaceName = workspace?.name ?? workspaceRouteName;
  const workspaceLabel = getRepositoryTitle(workspace ?? { name: workspaceName });
  const isWorkspaceStatePending = lastFetchedAt === null && fetchStatus !== "error";
  const isAmbiguousWorkspaceRoute =
    !workspace
    && workspaces.filter((candidate) => candidate.name === workspaceRouteName).length > 1;
  const routeError = fetchStatus === "error"
    ? "Failed to load workspace state"
    : isAmbiguousWorkspaceRoute
      ? "Workspace route is ambiguous."
      : "Workspace not found.";
  const dir = workspace?.dir ?? "";
  const goalTarget = workspace?.name ?? "";
  const hasLoadedGoal = goalTarget !== "" && loadedGoalTargetRef.current === goalTarget;
  const workspaceReturnTarget = workspace?.name ?? workspaceRouteName ?? workspaceName;
  const workspaceDetailPath = `/workspaces/${encodeURIComponent(workspaceReturnTarget)}`;

  useEffect(() => {
    return () => {
      if (redirectTimeoutRef.current) {
        clearTimeout(redirectTimeoutRef.current);
      }
    };
  }, []);

  useEffect(() => {
    if (!goalTarget) {
      loadedGoalTargetRef.current = null;
      goalRequestRef.current = null;
      setError(null);
      setIsLoading(isWorkspaceStatePending);
    }
  }, [goalTarget, isWorkspaceStatePending]);

  useEffect(() => {
    if (!goalTarget || loadedGoalTargetRef.current === goalTarget) {
      return;
    }

    let cancelled = false;

    let goalRequest = goalRequestRef.current;
    if (!goalRequest || goalRequest.target !== goalTarget) {
      goalRequest = {
        target: goalTarget,
        promise: api.workspaces.getGoal(goalTarget),
      };
      goalRequestRef.current = goalRequest;
    }

    setIsLoading(true);
    setError(null);

    goalRequest.promise.then(
      (goal) => {
        if (cancelled) {
          return;
        }

        if (goalRequestRef.current?.target === goalTarget) {
          goalRequestRef.current = null;
        }

        loadedGoalTargetRef.current = goalTarget;
        setContent(goal.content);
        setSaveSuccess(false);
        setIsLoading(false);
      },
      () => {
        if (cancelled) {
          return;
        }

        if (goalRequestRef.current?.target === goalTarget) {
          goalRequestRef.current = null;
        }

        setError("Failed to load GOAL.md");
        setIsLoading(false);
      },
    );

    return () => { cancelled = true; };
  }, [goalTarget, loadAttempt]);

  useEffect(() => {
    if (isLoading) {
      return;
    }

    if (!workspace) {
      routeErrorRef.current?.focus();
      return;
    }

    if (!hasLoadedGoal && error) {
      goalErrorRef.current?.focus();
      return;
    }

    if (!hasLoadedGoal || error) {
      return;
    }

    titleRef.current?.focus();
  }, [isLoading, workspace, hasLoadedGoal, error, goalTarget]);

  const handleRetryLoad = useCallback(() => {
    if (!goalTarget || isLoading) {
      return;
    }

    loadedGoalTargetRef.current = null;
    goalRequestRef.current = null;
    setError(null);
    setIsLoading(true);
    setSaveSuccess(false);
    setLoadAttempt((current) => current + 1);
  }, [goalTarget, isLoading]);

  const blurFocusedEditor = useCallback(() => {
    const activeElement = document.activeElement;
    if (!(activeElement instanceof HTMLElement)) {
      return;
    }

    if (!editorSurfaceRef.current?.contains(activeElement)) {
      return;
    }

    backLinkRef.current?.focus();
    if (document.activeElement !== activeElement) {
      return;
    }

    activeElement.blur();
  }, []);

  const handleSave = useCallback(async () => {
    if (!goalTarget || isSaving || !content.trim()) return;

    setIsSaving(true);
    setError(null);

    try {
      await api.workspaces.updateGoal(goalTarget, content);
      triggerFactoryRefresh();
      blurFocusedEditor();
      setSaveSuccess(true);
      if (redirectTimeoutRef.current) {
        clearTimeout(redirectTimeoutRef.current);
      }
      redirectTimeoutRef.current = setTimeout(() => {
        redirectTimeoutRef.current = null;
        navigate(workspaceDetailPath);
      }, 1000);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError("Failed to save GOAL.md");
      }
    } finally {
      setIsSaving(false);
    }
  }, [goalTarget, isSaving, content, blurFocusedEditor, navigate, workspaceDetailPath]);

  if (isLoading || (workspace && !hasLoadedGoal && !error)) {
    return (
      <div className="fixed inset-0 z-[60] flex flex-col bg-background">
        <div className="flex items-center gap-3 px-4 py-2 border-b bg-background">
          <Skeleton className="h-8 w-8" />
          <Skeleton className="h-5 w-32" />
          <div className="ml-auto">
            <Skeleton className="h-8 w-24" />
          </div>
        </div>
        <div className="flex-1 p-4">
          <Skeleton className="h-full w-full" />
        </div>
      </div>
    );
  }

  if (!workspace) {
    return (
      <div className="fixed inset-0 z-[60] flex flex-col bg-background">
        <div className="flex items-center gap-3 px-4 py-2 border-b bg-background shrink-0">
          <Button asChild size="sm" variant="outline">
            <Link to="/">Back to Workspaces</Link>
          </Button>
          <h1 className="text-sm font-medium">Edit GOAL.md</h1>
        </div>
        <div className="p-4">
          <div data-testid="edit-goal-route-error" ref={routeErrorRef} tabIndex={-1} className="rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
            <Alert variant="destructive">
              <AlertDescription>{routeError}</AlertDescription>
            </Alert>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="fixed inset-0 z-[60] flex flex-col bg-background">
      <div className="flex items-center gap-3 px-4 py-2 border-b bg-background shrink-0">
        <Link
          ref={backLinkRef}
          to={workspaceDetailPath}
          className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1"
          aria-label={`Back to ${workspaceLabel}`}
        >
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <h1 ref={titleRef} tabIndex={-1} className="text-sm font-medium rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
          Edit GOAL.md
        </h1>
        
        <FocusableTooltipText
          as="span"
          className="text-sm text-muted-foreground max-w-xs cursor-help"
          content={(
            <div className="max-w-xs">
              <div className="font-medium">{workspaceLabel}</div>
              {workspaceLabel !== workspaceName && (
                <div className="text-xs text-muted-foreground mt-1">Name: {workspaceName}</div>
              )}
              {dir && <div className="text-xs text-muted-foreground mt-1">{dir}</div>}
            </div>
          )}
        >
          {workspaceLabel}
        </FocusableTooltipText>

        {hasLoadedGoal && error ? (
          <Alert className="py-1 px-3 border-destructive/50 text-destructive flex items-center gap-2 h-8">
            <AlertDescription className="text-xs">{error}</AlertDescription>
          </Alert>
        ) : null}

        <div className="ml-auto">
          {hasLoadedGoal ? (
            <Button
              size="sm"
              onClick={handleSave}
              disabled={isSaving || saveSuccess || !content.trim()}
            >
              {saveSuccess ? (
                <>
                  <Check className="mr-2 h-4 w-4" />
                  Saved!
                </>
              ) : isSaving ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="mr-2 h-4 w-4" />
                  Save GOAL.md
                </>
              )}
            </Button>
          ) : error ? (
            <Button size="sm" variant="outline" onClick={handleRetryLoad} disabled={isLoading}>
              Retry
            </Button>
          ) : null}
        </div>
      </div>

      <div ref={editorSurfaceRef} className="flex-1 min-h-0">
        {hasLoadedGoal ? (
          <MarkdownEditor
            value={content}
            onChange={(v) => setContent(v ?? "")}
            onSubmitShortcut={handleSave}
            disabled={isSaving || saveSuccess}
            workspaceName={workspaceName}
            fillHeight
          />
        ) : (
          <div className="p-4">
            <div data-testid="edit-goal-load-error" ref={goalErrorRef} tabIndex={-1} className="rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
              <Alert variant="destructive">
                <AlertDescription>{error ?? "Failed to load GOAL.md"}</AlertDescription>
              </Alert>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
