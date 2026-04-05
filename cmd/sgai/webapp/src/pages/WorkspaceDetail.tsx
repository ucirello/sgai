import { useState, useEffect, Suspense, lazy, useRef, useCallback, useMemo, type ReactNode } from "react";
import { useParams, Link, useNavigate, useSearchParams } from "react-router";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { FocusableTooltipText } from "@/components/FocusableTooltipText";
import {
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogFooter,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogCancel,
  AlertDialogAction,
} from "@/components/ui/alert-dialog";
import { NotYetAvailable } from "@/components/NotYetAvailable";
import { WorkspaceRepositoryAction } from "@/components/WorkspaceRepositoryAction";
import { InlineForkEditor } from "@/pages/InlineForkEditor";
import { api } from "@/lib/api";
import { normalizeActiveAgents } from "@/lib/active-agents";
import { duplicateRouteWorkspaceControlsDisabledReason } from "@/lib/duplicate-route-mutations";
import { canCreateForkFromWorkspace } from "@/lib/workspace-forks";
import { useWorkspacePageState } from "@/lib/workspace-page-state";
import { useAdhocRun } from "@/hooks/useAdhocRun";
import { ChevronRight, Square } from "lucide-react";
import type { ApiWorkspaceEntry, ApiActionEntry } from "@/types";
import { cn } from "@/lib/utils";
import {
  buildWorkspaceGoalEditPath,
  buildWorkspacePath,
  getWorkspaceBaseLabel,
  readWorkspaceDirFromSearchParams,
} from "@/lib/workspace-identity";

const SessionTab = lazy(() => import("./tabs/SessionTab").then((m) => ({ default: m.SessionTab })));
const MessagesTab = lazy(() => import("./tabs/MessagesTab").then((m) => ({ default: m.MessagesTab })));
const LogTab = lazy(() => import("./tabs/LogTab").then((m) => ({ default: m.LogTab })));
const RunTab = lazy(() => import("./tabs/RunTab").then((m) => ({ default: m.RunTab })));
const EventsTab = lazy(() => import("./tabs/EventsTab").then((m) => ({ default: m.EventsTab })));
const ForksTab = lazy(() => import("./tabs/ForksTab").then((m) => ({ default: m.ForksTab })));

function parseExecTime(value: string | undefined | null): number | null {
  if (!value) return null;
  const trimmed = value.trim();
  if (!trimmed || trimmed === "-") return null;
  const compact = trimmed.replace(/\s+/g, "");
  const match = compact.match(/^(?:(\d+)m)?(\d+)s$/);
  if (!match) return null;
  const minutes = match[1] ? Number(match[1]) : 0;
  const seconds = Number(match[2]);
  if (Number.isNaN(minutes) || Number.isNaN(seconds)) return null;
  return minutes * 60 + seconds;
}

function formatExecTime(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}

function WorkspaceDetailSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 pb-3 border-b">
        <Skeleton className="h-7 w-48" />
        <Skeleton className="h-6 w-16 rounded" />
        <Skeleton className="h-6 w-20 rounded" />
      </div>
      <Skeleton className="h-10 w-full" />
      <div className="space-y-3">
        <Skeleton className="h-32 w-full rounded-xl" />
        <Skeleton className="h-48 w-full rounded-xl" />
      </div>
    </div>
  );
}

const TABS = [
  { key: "progress", label: "Progress" },
  { key: "fork", label: "Fork" },
  { key: "log", label: "Log" },
  { key: "messages", label: "Messages" },
  { key: "internals", label: "Internals" },
  { key: "run", label: "Run" },
] as const;

const ROOT_TABS = [
  { key: "forks", label: "Forks" },
  { key: "fork", label: "Fork" },
] as const;

const DEFAULT_TAB = TABS[0].key;
const DEFAULT_ROOT_TAB = ROOT_TABS[0].key;
const TAB_KEYS = new Set(TABS.map((tab) => tab.key));
const ROOT_TAB_KEYS = new Set(ROOT_TABS.map((tab) => tab.key));

function resolveRedirectTab({
  requestedTab,
  isForkedRoot,
  showForkTab,
}: {
  requestedTab: string;
  isForkedRoot: boolean;
  showForkTab: boolean;
}): string | null {
  if (isForkedRoot) {
    return ROOT_TAB_KEYS.has(requestedTab) ? null : DEFAULT_ROOT_TAB;
  }

  if (requestedTab === "fork" && !showForkTab) {
    return DEFAULT_TAB;
  }

  return TAB_KEYS.has(requestedTab) ? null : DEFAULT_TAB;
}

interface TabNavProps {
  workspace: Pick<ApiWorkspaceEntry, "name" | "dir">;
  activeTab: string;
  isRoot: boolean;
  hasForks: boolean;
  showForkTab: boolean;
  workspaceDir?: string;
}

function TabNav({ workspace, activeTab, isRoot, hasForks, showForkTab, workspaceDir }: TabNavProps) {
  const tabs = isRoot && hasForks
    ? ROOT_TABS
    : TABS.filter((tab) => {
        if (tab.key === "fork" && !showForkTab) return false;
        return true;
      });

  return (
    <nav className="border-b overflow-x-auto overflow-y-hidden pl-2.5 mb-0">
      <ul className="flex mb-0 whitespace-nowrap min-w-min list-none p-0 m-0">
        {tabs.map((tab) => (
          <li key={tab.key}>
            <Link
              to={buildWorkspacePath(workspace, tab.key, { workspaceDir })}
              aria-current={activeTab === tab.key ? "page" : undefined}
              className={cn(
                "inline-block px-4 py-2 text-sm no-underline transition-colors border-b-2",
                activeTab === tab.key
                  ? "text-primary border-primary"
                  : "text-muted-foreground border-transparent hover:text-foreground"
              )}
            >
              {tab.label}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}


export function WorkspaceDetail(): JSX.Element | null {
  const { name, "*": tabPath } = useParams<{ name: string; "*": string }>();
  const [searchParams] = useSearchParams();
  const workspaceName = name ?? "";
  const workspaceDir = readWorkspaceDirFromSearchParams(searchParams);
  const workspaceRouteKey = workspaceDir ? `${workspaceName}|${workspaceDir}` : workspaceName;
  const requestedTab = tabPath?.split("/")[0] || "progress";
  const navigate = useNavigate();

  const [actionError, setActionError] = useState<string | null>(null);
  const [runningOverride, setRunningOverride] = useState<boolean | null>(null);
  const previousWorkspaceRef = useRef<string | null>(null);
  const [isStartStopPending, setIsStartStopPending] = useState(false);
  const [isSelfDrivePending, setIsSelfDrivePending] = useState(false);
  const [isPinPending, setIsPinPending] = useState(false);
  const [isEditorPending, setIsEditorPending] = useState(false);
  const [isResetPending, setIsResetPending] = useState(false);
  const [isResetDialogOpen, setIsResetDialogOpen] = useState(false);
  const [execTimeSeconds, setExecTimeSeconds] = useState<number | null>(null);

  const { workspace: detail, fetchStatus } = useWorkspacePageState(
    workspaceDir
      ? { name: workspaceName, dir: workspaceDir }
      : workspaceName,
  );
  const loading = fetchStatus === "fetching" && detail === null;
  const routeError = fetchStatus === "error"
    ? "Failed to load workspace state"
    : "Workspace not found.";

  useEffect(() => {
    if (previousWorkspaceRef.current !== workspaceRouteKey) {
      previousWorkspaceRef.current = workspaceRouteKey;
      setRunningOverride(null);
    }
  }, [workspaceRouteKey]);

  useEffect(() => {
    if (runningOverride !== null && detail?.running === runningOverride) {
      setRunningOverride(null);
    }
  }, [detail?.running, runningOverride]);

  useEffect(() => {
    if (!workspaceRouteKey) return;
    setActionError(null);
  }, [workspaceRouteKey]);

  const totalExecTimeRaw = detail?.totalExecTime;
  const detailRunning = detail?.running;
  useEffect(() => {
    if (!detail) return;
    const parsed = parseExecTime(totalExecTimeRaw ?? "");
    if (parsed !== null) {
      setExecTimeSeconds(parsed);
    } else if (detailRunning) {
      setExecTimeSeconds(0);
    } else {
      setExecTimeSeconds(null);
    }
    if (!detailRunning) return;
    const timer = setInterval(() => {
      setExecTimeSeconds((prev) => (prev === null ? prev : prev + 1));
    }, 1000);
    return () => clearInterval(timer);
  }, [totalExecTimeRaw, detailRunning]);

  const hasForks = (detail?.forks?.length ?? 0) > 0;
  const isForkedRoot = Boolean(detail?.isRoot && hasForks);
  const showForkTab = canCreateForkFromWorkspace(detail);
  const ideAvailable = detail?.ide?.available ?? false;
  const redirectTab = resolveRedirectTab({ requestedTab, isForkedRoot, showForkTab });
  const idePageUrl = buildWorkspacePath({
    name: detail?.name ?? workspaceName,
    dir: detail?.dir ?? workspaceDir,
  }, "ide", { workspaceDir });
  const activeTab = redirectTab ?? requestedTab;

  useEffect(() => {
    if (!detail || !redirectTab) return;
    navigate(buildWorkspacePath(detail, redirectTab, { workspaceDir }), { replace: true });
  }, [detail, navigate, redirectTab, workspaceDir]);

  if (loading && !detail) return <WorkspaceDetailSkeleton />;

  if (!detail) {
    return <WorkspaceRouteErrorState message={routeError} />;
  }

  const detailLabel = getWorkspaceBaseLabel(detail);
  const dirQualifiedRoute = Boolean(detail.dir && workspaceDir);
  const workspaceControlsDisabledReason = dirQualifiedRoute
    ? duplicateRouteWorkspaceControlsDisabledReason
    : null;

  if (!detail.hasSgai && !detail.isRoot) {
    return (
      <NoWorkspaceState
        label={detailLabel}
        goalEditPath={buildWorkspaceGoalEditPath(detail)}
        dir={detail.dir}
        goalEditDisabledReason={workspaceControlsDisabledReason}
      />
    );
  }

  const areBasenameOnlyMutationsDisabled = workspaceControlsDisabledReason !== null;
  const effectiveRunning = runningOverride !== null ? runningOverride : (detail.running ?? false);

  const totalExecTime = detail.totalExecTime?.trim() ?? "";
  const fallbackExecTime = totalExecTime && totalExecTime !== "-" ? totalExecTime : "0s";
  const displayExecTime = execTimeSeconds !== null
    ? formatExecTime(execTimeSeconds)
    : fallbackExecTime;
  const activeAgents = normalizeActiveAgents(detail);
  const hasMultipleActiveAgents = activeAgents.length > 1;
  const singleActiveAgent = activeAgents[0] ?? "";
  const modelLabel = detail.currentModel
    ? detail.currentModel.split("/").pop() ?? detail.currentModel
    : "";
  const agentModelLabel = hasMultipleActiveAgents
    ? ""
    : [singleActiveAgent, modelLabel].filter(Boolean).join(" | ");
  const fullAgentModelLabel = hasMultipleActiveAgents
    ? detail.currentModel
    : [singleActiveAgent, detail.currentModel].filter(Boolean).join(" | ");
  const statusLine = detail.task?.trim() || detail.status?.trim();
  const showStatusLine = !isForkedRoot && Boolean(activeAgents.length > 0 || modelLabel || statusLine);
  const selfDriveLabel = effectiveRunning ? "Self-Drive" : "Self-drive";
  const showEditGoalAction = !effectiveRunning || detail.hasSgai || Boolean(detail.goalContent?.trim());
  const goalEditPath = buildWorkspaceGoalEditPath(detail);
  const isGoalEditActionDisabled = areBasenameOnlyMutationsDisabled;
  const showOpenEditorAction = true;
  const isStartActionDisabled = effectiveRunning || isStartStopPending || isSelfDrivePending || isResetPending || areBasenameOnlyMutationsDisabled;
  const isStopActionDisabled = isStartStopPending || areBasenameOnlyMutationsDisabled;
  const isResetActionDisabled = isResetPending || isStartStopPending || isSelfDrivePending || areBasenameOnlyMutationsDisabled;
  const isPinActionDisabled = isPinPending || areBasenameOnlyMutationsDisabled;
  const isEditorActionDisabled = isEditorPending || areBasenameOnlyMutationsDisabled;
  const handleStart = () => {
	if (!workspaceName || isStartStopPending || areBasenameOnlyMutationsDisabled) return;
	setActionError(null);
	setIsStartStopPending(true);
	void (async () => {
		try {
			const result = await api.workspaces.start(workspaceName, false);
			if (result.running) {
				setRunningOverride(true);
			}
		} catch (err) {
			setActionError(err instanceof Error ? err.message : "Failed to start session");
		} finally {
			setIsStartStopPending(false);
		}
	})();
  };

  const handleStop = () => {
	if (!workspaceName || isStartStopPending || areBasenameOnlyMutationsDisabled) return;
	setActionError(null);
	setIsStartStopPending(true);
	void (async () => {
		try {
			const result = await api.workspaces.stop(workspaceName);
			if (!result.running) {
				setRunningOverride(false);
			}
		} catch (err) {
			setActionError(err instanceof Error ? err.message : "Failed to stop session");
		} finally {
			setIsStartStopPending(false);
		}
	})();
  };

  const handleSelfDrive = () => {
	if (!workspaceName || isSelfDrivePending || areBasenameOnlyMutationsDisabled) return;
	setActionError(null);
	setIsSelfDrivePending(true);
	void (async () => {
		try {
			await api.workspaces.start(workspaceName, true);
			setRunningOverride(true);
		} catch (err) {
			setActionError(err instanceof Error ? err.message : "Failed to start self-drive session");
		} finally {
			setIsSelfDrivePending(false);
		}
	})();
  };

  const handlePinToggle = () => {
	if (!workspaceName || isPinPending || areBasenameOnlyMutationsDisabled) return;
	setActionError(null);
	setIsPinPending(true);
	void (async () => {
		try {
			await api.workspaces.togglePin(workspaceName);
		} catch (err) {
			setActionError(err instanceof Error ? err.message : "Failed to toggle pin");
		} finally {
			setIsPinPending(false);
		}
	})();
  };

  const handleOpenEditor = () => {
	if (!workspaceName || isEditorPending || areBasenameOnlyMutationsDisabled) return;
	setActionError(null);
	setIsEditorPending(true);
	void (async () => {
		try {
			await api.workspaces.openEditor(workspaceName);
		} catch (err) {
			setActionError(err instanceof Error ? err.message : "Failed to open editor");
		} finally {
			setIsEditorPending(false);
		}
	})();
  };

  const handleRepositoryActionCompleted = () => {
    navigate("/");
  };

  const handleResetDialogOpenChange = (nextOpen: boolean) => {
	if (isResetPending && !nextOpen) {
		return;
	}
	setIsResetDialogOpen(nextOpen);
  };

  const handleReset = () => {
	if (!workspaceName || isResetActionDisabled) return;
	setActionError(null);
	setIsResetPending(true);
	void (async () => {
		try {
			await api.workspaces.reset(workspaceName);
			setIsResetDialogOpen(false);
		} catch (err) {
			setActionError(err instanceof Error ? err.message : "Failed to reset workspace");
		} finally {
			setIsResetPending(false);
		}
	})();
  };

  return (
    <div className="sticky-header-wrapper">
      <div className="sticky top-0 z-10 bg-background">
        <header className="flex flex-wrap items-start gap-3 mb-3 pb-3 border-b">
          <div className="flex-shrink min-w-0 max-w-fit">
            <h3 className="m-0 text-xl font-semibold">
              <FocusableTooltipText
                as="span"
                className="whitespace-nowrap overflow-hidden text-ellipsis"
                content={(
                  <div className="max-w-xs">
                    <div className="font-medium">{detailLabel}</div>
                    {detailLabel !== detail.name && (
                      <div className="text-xs text-muted-foreground mt-1">Name: {detail.name}</div>
                    )}
                    {!detail.isFork && (
                      <div className="text-xs text-muted-foreground mt-1">{detail.dir}</div>
                    )}
                  </div>
                )}
              >
                {detailLabel}
              </FocusableTooltipText>
            </h3>
          </div>

          {!isForkedRoot && (
            <div className="flex items-center gap-2 shrink-0">
              <Tooltip>
                <TooltipTrigger asChild>
                <Badge
                  variant="secondary"
                  className="font-mono"
                  aria-label="Total execution time"
                  tabIndex={0}
                >
                  {displayExecTime}
                </Badge>
                </TooltipTrigger>
                <TooltipContent>Total execution time</TooltipContent>
              </Tooltip>
              <Badge variant={effectiveRunning ? "default" : "secondary"}>
                {effectiveRunning ? "running" : "stopped"}
              </Badge>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2 w-full md:w-auto md:ml-auto mt-2 md:mt-0 justify-start md:justify-end">
              {isForkedRoot ? (
                <>
                  {showOpenEditorAction && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={handleOpenEditor}
                      disabled={isEditorActionDisabled}
                    >
                      Open in Editor
                    </Button>
                  )}
                  {ideAvailable && (
                    <Button asChild size="sm" variant="outline">
                      <a href={idePageUrl} target="_blank" rel="noopener noreferrer" aria-label="Open IDE (opens in new tab)">
                        Open IDE
                      </a>
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="sm"
                      variant={detail.pinned ? "secondary" : "outline"}
                      onClick={handlePinToggle}
                      disabled={isPinActionDisabled}
                      aria-pressed={detail.pinned}
                    >
                    {detail.pinned ? "Unpin" : "Pin"}
                  </Button>
                </>
              ) : (
                <>
                  {detail?.needsInput && (
                      <Button
                        type="button"
                        size="sm"
                        variant="default"
                        onClick={() => navigate(buildWorkspacePath(detail, "respond", { workspaceDir }))}
                      >
                        Respond
                      </Button>
                  )}
                  {detail.continuousMode ? (
                    <>
                      <Button
                        type="button"
                        size="sm"
                        variant={(effectiveRunning && detail.interactiveAuto) ? "default" : "outline"}
                        onClick={handleSelfDrive}
                        disabled={isStartActionDisabled}
                        aria-pressed={effectiveRunning && detail.interactiveAuto}
                      >
                        Continuous Self-Drive
                      </Button>
                      {effectiveRunning && (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={handleStop}
                          disabled={isStopActionDisabled}
                        >
                          Stop
                        </Button>
                      )}
                    </>
                  ) : (
                    <>
                      <Button
                        type="button"
                        size="sm"
                        variant={(effectiveRunning && detail.interactiveAuto) ? "default" : "outline"}
                        onClick={handleSelfDrive}
                        disabled={isStartActionDisabled}
                        aria-pressed={effectiveRunning && detail.interactiveAuto}
                      >
                        {selfDriveLabel}
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant={(effectiveRunning && !detail.interactiveAuto) ? "default" : "outline"}
                        onClick={handleStart}
                        disabled={isStartActionDisabled}
                        aria-pressed={effectiveRunning && !detail.interactiveAuto}
                      >
                        Start
                      </Button>
                      {effectiveRunning && (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={handleStop}
                          disabled={isStopActionDisabled}
                        >
                          Stop
                        </Button>
                      )}
                      {!effectiveRunning && (
                        <AlertDialog open={isResetDialogOpen} onOpenChange={handleResetDialogOpenChange}>
                          <AlertDialogTrigger asChild>
                            <Button
                              type="button"
                              size="sm"
                              variant="destructive"
                              disabled={isResetActionDisabled}
                            >
                              Reset
                            </Button>
                          </AlertDialogTrigger>
                          <AlertDialogContent>
                            <AlertDialogHeader>
                              <AlertDialogTitle>Reset workspace state?</AlertDialogTitle>
                              <AlertDialogDescription>
                                This will reset the workflow state. The next time you start this workspace, it will begin with a fresh state.
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                              <AlertDialogCancel disabled={isResetActionDisabled}>Cancel</AlertDialogCancel>
                              <AlertDialogAction asChild>
                                <Button
                                  type="button"
                                  variant="destructive"
                                  onClick={(event) => {
                                    event.preventDefault();
                                    handleReset();
                                  }}
                                  disabled={isResetActionDisabled}
                                >
                                  Reset
                                </Button>
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      )}
                    </>
                  )}
                  {showEditGoalAction && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        if (isGoalEditActionDisabled) {
                          return;
                        }

                        navigate(goalEditPath);
                      }}
                      disabled={isGoalEditActionDisabled}
                    >
                      Edit GOAL
                    </Button>
                  )}
                  {showOpenEditorAction && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={handleOpenEditor}
                      disabled={isEditorActionDisabled}
                    >
                      Open in Editor
                    </Button>
                  )}
                  {ideAvailable && (
                    <Button asChild size="sm" variant="outline">
                      <a href={idePageUrl} target="_blank" rel="noopener noreferrer" aria-label="Open IDE (opens in new tab)">
                        Open IDE
                      </a>
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => navigate(buildWorkspacePath(detail, "skills", { workspaceDir }))}
                  >
                    Skills
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => navigate(buildWorkspacePath(detail, "snippets", { workspaceDir }))}
                  >
                    Snippets
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => navigate(buildWorkspacePath(detail, "agents", { workspaceDir }))}
                  >
                    Agents
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={detail.pinned ? "secondary" : "outline"}
                    onClick={handlePinToggle}
                    disabled={isPinActionDisabled}
                    aria-pressed={detail.pinned}
                  >
                    {detail.pinned ? "Unpin" : "Pin"}
                  </Button>
                  {!dirQualifiedRoute ? (
                    <WorkspaceRepositoryAction
                      workspace={detail}
                      context="detail"
                      onCompleted={handleRepositoryActionCompleted}
                    />
                  ) : null}
                </>
              )}
          </div>
        </header>

        {showStatusLine && (
          <div className="flex flex-wrap items-center gap-2 mb-2">
            {hasMultipleActiveAgents && (
              <div className="flex flex-wrap items-center gap-1">
                {activeAgents.map((agent) => (
                  <Badge key={agent} variant="secondary" className="font-mono">
                    {agent}
                  </Badge>
                ))}
              </div>
            )}
            {agentModelLabel && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="secondary" className="font-mono" tabIndex={0}>
                    {agentModelLabel}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>{fullAgentModelLabel || agentModelLabel}</TooltipContent>
              </Tooltip>
            )}
            {hasMultipleActiveAgents && modelLabel && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="secondary" className="font-mono" tabIndex={0}>
                    {modelLabel}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>{fullAgentModelLabel || modelLabel}</TooltipContent>
              </Tooltip>
            )}
            {statusLine && (
              <FocusableTooltipText as="span" className="text-sm text-muted-foreground max-w-[320px] md:max-w-[520px]">
                {statusLine}
              </FocusableTooltipText>
            )}
          </div>
          )}

          {workspaceControlsDisabledReason && (
            <Alert className="mb-2">
              <AlertDescription>{workspaceControlsDisabledReason}</AlertDescription>
            </Alert>
          )}

          {actionError && (
            <p className="text-sm text-destructive mb-2" role="alert">
              {actionError}
            </p>
          )}
          <TabNav
            workspace={detail}
            activeTab={activeTab}
            isRoot={detail.isRoot}
            hasForks={hasForks}
            showForkTab={showForkTab}
            workspaceDir={workspaceDir}
          />

        </div>

        <div className="pt-4">
          {isForkedRoot ? (
            <ForkRootActionOutputSlot workspaceName={workspaceName}>
              {({ onActionClick, isActionRunning }) => (
                <Suspense fallback={<TabSkeleton />}>
                  <TabContent
                    key={detail.dir}
                    activeTab={activeTab}
                    detail={detail}
                    onActionClick={onActionClick}
                    isActionRunning={isActionRunning}
                    showForkTab={showForkTab}
                    dirQualifiedRoute={dirQualifiedRoute}
                  />
                </Suspense>
              )}
            </ForkRootActionOutputSlot>
          ) : (
            <Suspense fallback={<TabSkeleton />}>
              <TabContent
                key={detail.dir}
                activeTab={activeTab}
                detail={detail}
                showForkTab={showForkTab}
                dirQualifiedRoute={dirQualifiedRoute}
              />
            </Suspense>
          )}
        </div>
    </div>
  );
}

function ForkRootActionOutputSlot({
  workspaceName,
  children,
}: {
  workspaceName: string;
  children: (controls: {
    onActionClick: (action: ApiActionEntry, variables: Record<string, string>, targetWorkspaceName: string) => void;
    isActionRunning: boolean;
  }) => ReactNode;
}) {
  const [actionOutputOpen, setActionOutputOpen] = useState(false);
  const {
    output: actionOutput,
    isRunning: isActionRunning,
    runError: actionRunError,
    startActionRun,
    stopRun: stopActionRun,
    outputRef: actionOutputRef,
  } = useAdhocRun({ workspaceName, skipModelsFetch: true });

  const handleActionClick = useCallback((
    action: ApiActionEntry,
    variables: Record<string, string>,
    targetWorkspaceName = workspaceName,
  ) => {
    setActionOutputOpen(true);
    void startActionRun(action.name, variables, targetWorkspaceName);
  }, [startActionRun, workspaceName]);

  return (
    <>
      {(actionRunError || isActionRunning || actionOutput) ? (
        <div className="space-y-3 mb-4">
          {actionRunError ? (
            <p className="text-sm text-destructive" role="alert">{actionRunError}</p>
          ) : null}
          {(isActionRunning || actionOutput) ? (
            <details open={actionOutputOpen} onToggle={(e) => setActionOutputOpen((e.target as HTMLDetailsElement).open)}>
              <summary className="cursor-pointer text-sm font-medium flex items-center gap-2">
                <ChevronRight
                  className="h-4 w-4 text-muted-foreground transition-transform duration-200 [[open]>&]:rotate-90"
                  aria-hidden="true"
                />
                Output
                {isActionRunning && (
                  <Button
                    type="button"
                    variant="destructive"
                    size="sm"
                    onClick={(e) => { e.preventDefault(); stopActionRun(); }}
                    className="ml-auto"
                  >
                    <Square className="mr-1 h-3 w-3" />
                    Stop
                  </Button>
                )}
              </summary>
              <pre
                ref={actionOutputRef}
                className="mt-2 bg-muted rounded-md p-4 text-sm font-mono overflow-auto max-h-[400px] whitespace-pre-wrap"
              >
                {actionOutput || (isActionRunning ? "Running..." : "")}
              </pre>
            </details>
          ) : null}
        </div>
      ) : null}
      {children({ onActionClick: handleActionClick, isActionRunning })}
    </>
  );
}

function TabSkeleton() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-24 w-full rounded-xl" />
      <Skeleton className="h-32 w-full rounded-xl" />
    </div>
  );
}

function TabContent({
  activeTab,
  detail,
  onActionClick,
  isActionRunning,
  showForkTab,
  dirQualifiedRoute,
}: {
  activeTab: string;
  detail: ApiWorkspaceEntry;
  onActionClick?: (action: ApiActionEntry, variables: Record<string, string>, targetWorkspaceName: string) => void;
  isActionRunning?: boolean;
  showForkTab: boolean;
  dirQualifiedRoute: boolean;
}) {
  switch (activeTab) {
    case "progress":
      return (
        <EventsTab
          workspaceName={detail.name}
          svgHash={detail.svgHash}
          agentModels={detail.agentModels}
          modelStatuses={detail.modelStatuses}
          needsInput={detail.needsInput}
          humanMessage={detail.humanMessage}
          currentAgent={detail.currentAgent}
          activeAgents={detail.activeAgents}
          dirQualifiedRoute={dirQualifiedRoute}
          events={detail.events ?? []}
          goalContent={detail.goalContent}
          actions={detail.actions}
          actionConfigError={detail.actionConfigError}
        />
      );
    case "fork":
      return showForkTab ? <InlineForkEditor key={detail.name} workspaceName={detail.name} /> : <NotYetAvailable pageName="Fork Tab" />;
    case "log":
      return <LogTab lines={detail.log ?? []} />;
    case "messages":
      return <MessagesTab workspaceName={detail.name} messages={detail.messages ?? []} />;
    case "internals":
      return (
        <SessionTab
          workspaceName={detail.name}
          agentSequence={detail.agentSequence ?? []}
          cost={detail.cost}
          modelStatuses={detail.modelStatuses}
          projectTodos={detail.projectTodos ?? []}
          agentTodoSections={detail.agentTodoSections ?? []}
          pmContent={detail.pmContent}
          hasProjectMgmt={detail.hasProjectMgmt}
        />
      );
    case "run":
      return <RunTab workspaceName={detail.name} currentModel={detail.currentModel} />;
    case "forks":
      return (
        <ForksTab
          workspaceName={detail.name}
          workspaceDir={detail.dir}
          actions={detail.actions}
          actionConfigError={detail.actionConfigError}
          onActionClick={onActionClick}
          isActionRunning={isActionRunning}
        />
      );
    default:
      return <NotYetAvailable pageName={`${activeTab.charAt(0).toUpperCase() + activeTab.slice(1)} Tab`} />;
  }
}

function NoWorkspaceState({
  label,
  goalEditPath,
  dir,
  goalEditDisabledReason,
}: {
  label: string;
  goalEditPath: string;
  dir: string;
  goalEditDisabledReason: string | null;
}) {
  return (
    <div>
      <div className="sticky top-0 z-10 bg-background">
        <header className="flex items-center gap-3 mb-3 pb-3 border-b">
          <h3 className="m-0 text-xl font-semibold" title={dir}>{label}</h3>
          <Badge variant="secondary">no workspace</Badge>
        </header>
      </div>
      <div className="text-center py-8 text-muted-foreground italic">
        <p>No workspace configured for this directory.</p>
        {goalEditDisabledReason ? (
          <Button type="button" variant="outline" className="mt-4 not-italic" disabled>
            Edit GOAL
          </Button>
        ) : (
          <Button asChild variant="outline" className="mt-4 not-italic">
            <Link to={goalEditPath}>Edit GOAL</Link>
          </Button>
        )}
        {goalEditDisabledReason && (
          <Alert className="mt-4 text-left not-italic">
            <AlertDescription>{goalEditDisabledReason}</AlertDescription>
          </Alert>
        )}
      </div>
    </div>
  );
}

function WorkspaceRouteErrorState({ message }: { message: string }) {
  return (
    <Alert className="max-w-2xl">
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}
