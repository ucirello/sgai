import { useState, useMemo, useCallback, type MouseEvent } from "react";
import { useNavigate } from "react-router";
import { Mail, SquarePen, ExternalLink, Square } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectOption } from "@/components/ui/select";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { PromptHistory } from "@/components/PromptHistory";
import { ActionBar } from "@/components/ActionBar";
import { WorkspaceRepositoryAction } from "@/components/WorkspaceRepositoryAction";
import { api } from "@/lib/api";
import { useFactoryState } from "@/lib/factory-state";
import {
  buildWorkspaceNameDisambiguators,
  buildWorkspacePath,
  getWorkspaceDisplayLabel,
  resolveWorkspaceByIdentity,
} from "@/lib/workspace-identity";
import { useAdhocRun } from "@/hooks/useAdhocRun";
import type { ApiForkEntry, ApiActionEntry, ApiWorkspaceEntry } from "@/types";

interface ForksTabProps {
  workspaceName: string;
  workspaceDir?: string;
  actions?: ApiActionEntry[];
  actionConfigError?: string;
  onActionClick?: (action: ApiActionEntry, variables: Record<string, string>, forkName: string) => void;
  isActionRunning?: boolean;
}

function ForksTabSkeleton() {
  return (
    <div className="space-y-2">
      {Array.from({ length: 3 }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full rounded" />
      ))}
    </div>
  );
}

function StatusDot({ running, needsInput }: { running: boolean; needsInput: boolean }) {
  let colorClass = "bg-gray-400";
  let label = "idle";
  if (running) {
    colorClass = "bg-green-500";
    label = "running";
  } else if (needsInput) {
    colorClass = "bg-amber-400";
    label = "needs input";
  }
  return (
    <span
      className={`inline-block w-2 h-2 rounded-full shrink-0 ${colorClass}`}
      aria-label={label}
      title={label}
    />
  );
}

interface CompactForkRowProps {
  fork: ApiForkEntry;
  forkWorkspace?: ApiWorkspaceEntry;
  needsInput: boolean;
  workspaceNameDisambiguators: Map<string, string>;
  actions?: ApiActionEntry[];
  isActionRunning: boolean;
  onActionClick?: (action: ApiActionEntry, variables: Record<string, string>, forkName: string) => void;
}

function CompactForkRow({
  fork,
  forkWorkspace,
  needsInput,
  workspaceNameDisambiguators,
  actions,
  isActionRunning,
  onActionClick,
}: CompactForkRowProps) {
  const navigate = useNavigate();
  const [actionError, setActionError] = useState<string | null>(null);
  const [isActionPending, setIsActionPending] = useState(false);
  const forkIdentity = forkWorkspace
    ? { ...forkWorkspace, ...fork }
    : { ...fork, title: fork.title ?? "", computedTitle: fork.computedTitle ?? "" };
  const forkLabel = getWorkspaceDisplayLabel(forkIdentity, workspaceNameDisambiguators);
  const showTechnicalName = forkLabel !== fork.name;
  const respondLabel = `Respond to fork ${forkLabel}`;
  const openEditorLabel = `Open fork ${forkLabel} in Editor`;
  const openInSgaiLabel = `Open fork ${forkLabel} in sgai`;

  const handleOpenEditor = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.stopPropagation();
    if (isActionPending) return;
    setActionError(null);
    setIsActionPending(true);
    void (async () => {
      try {
        await api.workspaces.openEditor(fork.name);
      } catch (err) {
        setActionError(err instanceof Error ? err.message : "Failed to open editor");
      } finally {
        setIsActionPending(false);
      }
    })();
  }, [fork.name, isActionPending]);

  const handleRespond = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.stopPropagation();
    navigate(buildWorkspacePath(forkIdentity, "respond"));
  }, [forkIdentity, navigate]);

  const handleOpenInSgai = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.stopPropagation();
    navigate(buildWorkspacePath(forkIdentity, "progress"));
  }, [forkIdentity, navigate]);

  return (
    <div className="border rounded-md overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2 bg-muted/10 hover:bg-muted/20 transition-colors">
        <StatusDot running={fork.running} needsInput={needsInput} />

        <Tooltip>
          <TooltipTrigger asChild>
            <span className="font-medium text-sm truncate flex-1 min-w-0 cursor-default">
              {forkLabel}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            <div className="max-w-xs">
              <div className="font-medium">{forkLabel}</div>
              {showTechnicalName && (
                <div className="text-xs text-muted-foreground mt-1">Name: {fork.name}</div>
              )}
            </div>
          </TooltipContent>
        </Tooltip>

        <div className="flex items-center gap-1 shrink-0">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                size="icon"
                variant={needsInput ? "default" : "ghost"}
                className="h-7 w-7"
                onClick={handleRespond}
                disabled={isActionPending || !needsInput}
                aria-label={respondLabel}
              >
                <Mail className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{respondLabel}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                size="icon"
                variant="ghost"
                className="h-7 w-7"
                onClick={handleOpenEditor}
                disabled={isActionPending}
                aria-label={openEditorLabel}
              >
                <SquarePen className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{openEditorLabel}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                size="icon"
                variant="ghost"
                className="h-7 w-7"
                onClick={handleOpenInSgai}
                disabled={isActionPending}
                aria-label={openInSgaiLabel}
              >
                <ExternalLink className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
              <TooltipContent>{openInSgaiLabel}</TooltipContent>
            </Tooltip>
          {forkWorkspace ? (
            <WorkspaceRepositoryAction
              workspace={forkWorkspace}
              context="fork-row"
              triggerLabelSuffix={workspaceNameDisambiguators.get(forkWorkspace.dir)}
            />
          ) : null}
        </div>

        {actions && actions.length > 0 && (
          <div className="shrink-0 border-l pl-2 ml-1">
            <ActionBar
              actions={actions}
              isRunning={isActionPending || isActionRunning}
              onActionClick={(action, variables) => onActionClick?.(action, variables, fork.name)}
              accessibilityContext={`fork ${forkLabel}`}
              className="flex items-center gap-1"
              buttonClassName="h-7 px-2 text-xs"
              showValidationErrors={false}
            />
          </div>
        )}
      </div>

      {actionError && (
        <div className="px-3 py-1.5 border-t bg-destructive/5">
          <p className="text-xs text-destructive" role="alert">{actionError}</p>
        </div>
      )}
    </div>
  );
}

function InlineRunBox({ workspaceName }: { workspaceName: string }) {
  const {
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
    handleSubmit,
    handleKeyDown,
    stopRun,
    outputRef,
    promptHistory,
    selectFromHistory,
    clearHistory,
  } = useAdhocRun({ workspaceName });

  if (modelsLoading && !models) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-10 w-full rounded" />
        <Skeleton className="h-32 w-full rounded" />
        <Skeleton className="h-10 w-32 rounded" />
      </div>
    );
  }

  if (modelsError) {
    return (
      <p className="text-sm text-destructive">
        Failed to load models: {modelsError.message}
      </p>
    );
  }

  if (!models) return null;

  return (
    <div className="space-y-4">
      {runError ? (
        <Alert className="border-destructive/50 text-destructive">
          <AlertDescription>{runError}</AlertDescription>
        </Alert>
      ) : null}

      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="flex flex-col gap-4">
          <div className="space-y-2">
            <Label htmlFor="forks-adhoc-model">Model</Label>
            <Select
              id="forks-adhoc-model"
              value={selectedModel}
              onChange={(event) => setSelectedModel(event.target.value)}
              disabled={isRunning}
              className="w-full"
            >
              <SelectOption value="" disabled>
                Select a model
              </SelectOption>
              {models?.models?.map((model) => (
                <SelectOption key={model.id} value={model.id}>
                  {model.name}
                </SelectOption>
              ))}
            </Select>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="forks-adhoc-prompt">Prompt</Label>
              <PromptHistory
                history={promptHistory}
                onSelect={selectFromHistory}
                onClear={clearHistory}
                disabled={isRunning}
              />
            </div>
            <Textarea
              id="forks-adhoc-prompt"
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Enter prompt..."
              rows={6}
              className="resize-y"
              disabled={isRunning}
            />
          </div>
        </div>

        <div className="flex gap-2">
          {isRunning ? (
            <Button
              type="button"
              variant="destructive"
              onClick={stopRun}
            >
              <Square className="mr-2 h-4 w-4" />
              Stop
            </Button>
          ) : (
            <Button
              type="submit"
              disabled={!selectedModel || !prompt.trim()}
            >
              Submit
            </Button>
          )}
        </div>
      </form>

      {(isRunning || output) ? (
        <div className="space-y-2">
          <Label>Output</Label>
          <pre
            ref={outputRef}
            className="bg-muted rounded-md p-4 text-sm font-mono overflow-auto max-h-[400px] whitespace-pre-wrap"
          >
            {output}
          </pre>
        </div>
      ) : null}
    </div>
  );
}

export function ForksTab({ workspaceName, workspaceDir, actions, actionConfigError, onActionClick, isActionRunning = false }: ForksTabProps) {
  const navigate = useNavigate();
  const { workspaces: allWorkspaces, fetchStatus } = useFactoryState();
  const workspace = resolveWorkspaceByIdentity(allWorkspaces, workspaceName, workspaceDir);
  const workspaceNameDisambiguators = useMemo(() => {
    return buildWorkspaceNameDisambiguators(allWorkspaces);
  }, [allWorkspaces]);
  const handleCreateFork = useCallback(() => {
    if (workspace) {
      navigate(buildWorkspacePath(workspace, "progress"));
    }
  }, [navigate, workspace]);

  const forkWorkspaceLookup = useMemo(() => {
    const lookup = new Map<string, ApiWorkspaceEntry>();
    for (const currentWorkspace of allWorkspaces) {
      lookup.set(currentWorkspace.dir, currentWorkspace);
    }
    return lookup;
  }, [allWorkspaces]);

  const needsInputMap = useMemo(() => {
    const map: Record<string, boolean> = {};
    for (const ws of allWorkspaces) {
      map[ws.dir] = ws.needsInput;
    }
    return map;
  }, [allWorkspaces]);

  if (fetchStatus === "fetching" && !workspace) return <ForksTabSkeleton />;

  if (!workspace) {
    if (fetchStatus === "error") {
      return (
        <p className="text-sm text-destructive">
          Failed to load forks
        </p>
      );
    }
    return null;
  }

  const forks = workspace.forks ?? [];
  const hasActionBar = Boolean((actions && actions.length > 0) || actionConfigError?.trim());
  const workspaceLabel = getWorkspaceDisplayLabel(workspace, workspaceNameDisambiguators);

  return (
    <div className="space-y-4">
      {hasActionBar && (
        <ActionBar
          actions={actions ?? []}
          actionConfigError={actionConfigError}
          isRunning={isActionRunning}
          onActionClick={(action, variables) => onActionClick?.(action, variables, workspaceName)}
          accessibilityContext={`workspace ${workspaceLabel}`}
        />
      )}
      {forks.length === 0 ? (
        <div className="py-8 text-center">
          <p>No forks yet. Create a fork to start work.</p>
          <div className="mt-4 flex justify-center">
            <Button type="button" onClick={handleCreateFork}>
              Create Fork
            </Button>
          </div>
        </div>
      ) : (
        <div className="space-y-1.5">
          {forks.map((fork) => (
            <CompactForkRow
              key={fork.dir}
              fork={fork}
              forkWorkspace={forkWorkspaceLookup.get(fork.dir)}
              needsInput={needsInputMap[fork.dir] ?? fork.needsInput ?? false}
              workspaceNameDisambiguators={workspaceNameDisambiguators}
              actions={actions}
              isActionRunning={isActionRunning}
              onActionClick={onActionClick}
            />
          ))}
        </div>
      )}

      <Separator className="my-6" />

      <div>
        <h3 className="text-lg font-semibold mb-4">Ad-hoc Prompt</h3>
        <InlineRunBox workspaceName={workspaceName} />
      </div>
    </div>
  );
}
