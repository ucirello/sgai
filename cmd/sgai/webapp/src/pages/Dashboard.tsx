import { useState, useEffect, useCallback, useMemo, type ReactNode, type CSSProperties } from "react";
import { useParams, useNavigate, Link } from "react-router";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import sgaiLogo from "@/assets/sgai-logo.svg";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import { Loader2, Inbox, Link as LinkIcon } from "lucide-react";
import { WorkspaceRepositoryAction } from "@/components/WorkspaceRepositoryAction";
import { useFactoryState } from "@/lib/factory-state";
import { useSidebarResize } from "@/hooks/useSidebarResize";
import { cn } from "@/lib/utils";
import type { ApiWorkspaceEntry } from "@/lib/factory-state";
import {
  getWorkspaceBaseLabel,
  buildWorkspaceNameDisambiguators,
  buildWorkspacePath,
  getWorkspaceDisplayLabel,
  isSameWorkspace,
  resolveWorkspaceByName,
} from "@/lib/workspace-identity";

type ForkEntry = NonNullable<ApiWorkspaceEntry["forks"]>[number];

function workspaceToForkEntry(ws: ApiWorkspaceEntry): ForkEntry {
  return {
    name: ws.name,
    dir: ws.dir,
    running: ws.running,
    needsInput: ws.needsInput,
    inProgress: ws.inProgress,
    pinned: ws.pinned,
    title: ws.title,
    computedTitle: ws.computedTitle,
  };
}

const tooltipMetadataClassName = "mt-1 text-xs text-background";

function getOrphanPinnedForkDisplayLabel(
  rootLabel: string,
  forkLabel: string,
  rootBaseLabel: string,
  forkBaseLabel: string,
): string {
  if (forkBaseLabel.startsWith(`${rootBaseLabel}/`)) {
    return forkLabel;
  }

  return `${rootLabel}/${forkLabel}`;
}

function WorkspaceTreeSkeleton() {
  return (
    <div className="space-y-2 p-2">
      {Array.from({ length: 5 }, (_, i) => (
        <Skeleton key={i} className="h-8 w-full rounded" />
      ))}
    </div>
  );
}

interface WorkspaceIndicatorFields {
  running: boolean;
  needsInput: boolean;
  pinned: boolean;
  external?: boolean;
}

interface WorkspaceIndicatorsProps {
  workspace: WorkspaceIndicatorFields;
}

function WorkspaceIndicators({ workspace }: WorkspaceIndicatorsProps) {
  const isActive = workspace.running;
  const runningLabel = workspace.running ? "Running" : "In progress";

  return (
    <span className="flex items-center gap-1 shrink-0">
      {workspace.external && (
        <Tooltip>
          <TooltipTrigger asChild>
            <LinkIcon className="h-3 w-3 text-muted-foreground" aria-label="External workspace" title="External workspace" />
          </TooltipTrigger>
          <TooltipContent>External workspace</TooltipContent>
        </Tooltip>
      )}
      {isActive && (
        <Loader2 className="h-3 w-3 text-primary animate-spin" aria-label={runningLabel} title={runningLabel} />
      )}
      {workspace.needsInput && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Inbox className="h-3 w-3 text-primary" aria-label="Waiting for response" title="Waiting for response" />
          </TooltipTrigger>
          <TooltipContent>Waiting for response</TooltipContent>
        </Tooltip>
      )}
      {workspace.pinned && (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="text-[0.7rem] opacity-70">📌</span>
          </TooltipTrigger>
          <TooltipContent>Pinned</TooltipContent>
        </Tooltip>
      )}
    </span>
  );
}

interface ForkItemProps {
  fork: ForkEntry;
  selectedWorkspace: ApiWorkspaceEntry | null;
  workspaceLookup: Map<string, ApiWorkspaceEntry>;
  rootWorkspace?: Pick<ApiWorkspaceEntry, "name" | "dir">;
  workspaceNameDisambiguators: Map<string, string>;
}

function ForkItem({
  fork,
  selectedWorkspace,
  workspaceLookup,
  rootWorkspace,
  workspaceNameDisambiguators,
}: ForkItemProps) {
  const navigate = useNavigate();
  const forkSelected = isSameWorkspace(fork, selectedWorkspace);
  const forkFullEntry = workspaceLookup.get(fork.dir);
  const forkLabelSource = {
    ...(forkFullEntry ?? fork),
    title: fork.title || forkFullEntry?.title || "",
    computedTitle: fork.computedTitle || forkFullEntry?.computedTitle || "",
  };
  const forkLabel = getWorkspaceDisplayLabel(forkLabelSource, workspaceNameDisambiguators);
  const showTechnicalName = forkLabel !== fork.name;
  const handleActionCompleted = useCallback(() => {
    if (isSameWorkspace(fork, selectedWorkspace) && rootWorkspace) {
      navigate(buildWorkspacePath(rootWorkspace, "forks"));
    }
  }, [fork, navigate, rootWorkspace, selectedWorkspace]);

  return (
    <SidebarMenuItem>
      <div className="flex items-center gap-0 group/row">
        <Tooltip>
          <SidebarMenuButton
            asChild
            isActive={forkSelected}
            className={cn(
              "flex-1 min-w-0 relative before:content-[''] before:absolute before:left-[-0.875rem] before:top-1/2 before:w-3.5 before:h-0.5 before:bg-border before:rounded-sm",
              forkSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
            <TooltipTrigger asChild>
              <Link to={buildWorkspacePath(forkFullEntry ?? fork, "progress")}>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {forkLabel}
                </span>
                <WorkspaceIndicators workspace={fork} />
              </Link>
            </TooltipTrigger>
          </SidebarMenuButton>
          <TooltipContent side="right">
            <div className="max-w-xs">
              <div className="font-medium">{forkLabel}</div>
              {showTechnicalName && (
                <div className={tooltipMetadataClassName}>Name: {fork.name}</div>
              )}
            </div>
          </TooltipContent>
        </Tooltip>
        {forkFullEntry ? (
          <WorkspaceRepositoryAction
            workspace={forkFullEntry}
            context="tree"
            triggerLabelSuffix={workspaceNameDisambiguators.get(forkFullEntry.dir)}
            onCompleted={handleActionCompleted}
          />
        ) : null}
      </div>
    </SidebarMenuItem>
  );
}

interface WorkspaceTreeItemProps {
  workspace: ApiWorkspaceEntry;
  selectedWorkspace: ApiWorkspaceEntry | null;
  workspaceLookup: Map<string, ApiWorkspaceEntry>;
  workspaceNameDisambiguators: Map<string, string>;
}

function WorkspaceTreeItem({
  workspace,
  selectedWorkspace,
  workspaceLookup,
  workspaceNameDisambiguators,
}: WorkspaceTreeItemProps) {
  const navigate = useNavigate();
  const fullWorkspace = workspaceLookup.get(workspace.dir);
  const forks = fullWorkspace?.forks || workspace.forks || [];
  const hasForks = forks.length > 0;
  const isSelected = isSameWorkspace(workspace, selectedWorkspace);
  const hasForkSelected = forks.some((fork) => isSameWorkspace(fork, selectedWorkspace));
  const [expanded, setExpanded] = useState(() => isSelected || hasForkSelected);

  useEffect(() => {
    if (isSelected || hasForkSelected) {
      setExpanded(true);
    }
  }, [isSelected, hasForkSelected]);

  const displayText = getWorkspaceDisplayLabel(fullWorkspace ?? workspace, workspaceNameDisambiguators);
  const showTechnicalName = displayText !== workspace.name;
  const handleActionCompleted = useCallback(() => {
    if (isSameWorkspace(workspace, selectedWorkspace)) {
      navigate("/");
    }
  }, [navigate, selectedWorkspace, workspace]);

  return (
    <SidebarMenuItem className="mb-0.5">
      <div className="flex items-center gap-0 group/row">
        {hasForks ? (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setExpanded((prev) => !prev)}
            className="w-5 h-6 p-0 text-xs font-semibold shrink-0 mr-1 bg-muted text-muted-foreground hover:bg-secondary hover:text-secondary-foreground transition-colors self-start mt-1"
            aria-label={expanded ? `Collapse forks for ${displayText}` : `Expand forks for ${displayText}`}
            aria-expanded={expanded}
          >
            {expanded ? "−" : "+"}
          </Button>
        ) : (
          <span className="w-5 h-5 inline-block shrink-0 mr-1" />
        )}
        <Tooltip>
          <SidebarMenuButton
            asChild
            isActive={isSelected}
            className={cn(
              "flex-1 min-w-0",
              isSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
            <TooltipTrigger asChild>
              <Link to={buildWorkspacePath(fullWorkspace ?? workspace, "progress")}>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {displayText}
                </span>
                <WorkspaceIndicators workspace={workspace} />
              </Link>
            </TooltipTrigger>
          </SidebarMenuButton>
          <TooltipContent side="right">
            <div className="max-w-xs">
              <div className="font-medium">{displayText}</div>
              {showTechnicalName && (
                <div className={tooltipMetadataClassName}>Name: {workspace.name}</div>
              )}
            </div>
          </TooltipContent>
        </Tooltip>
        <WorkspaceRepositoryAction
          workspace={fullWorkspace ?? workspace}
          context="tree"
          triggerLabelSuffix={workspaceNameDisambiguators.get((fullWorkspace ?? workspace).dir)}
          onCompleted={handleActionCompleted}
        />
      </div>

      {hasForks && expanded && (
        <div className="ml-2.5 pl-4 relative before:content-[''] before:absolute before:left-2.5 before:top-0 before:bottom-2 before:w-0.5 before:bg-border before:rounded-sm">
          <SidebarMenu>
            {forks.map((fork) => (
              <ForkItem
                key={fork.dir}
                fork={fork}
                selectedWorkspace={selectedWorkspace}
                workspaceLookup={workspaceLookup}
                rootWorkspace={fullWorkspace ?? workspace}
                workspaceNameDisambiguators={workspaceNameDisambiguators}
              />
            ))}
          </SidebarMenu>
        </div>
      )}
    </SidebarMenuItem>
  );
}

interface InProgressItemProps {
  workspace: ApiWorkspaceEntry;
  selectedWorkspace: ApiWorkspaceEntry | null;
  workspaceLookup: Map<string, ApiWorkspaceEntry>;
  workspaceNameDisambiguators: Map<string, string>;
}

function InProgressItem({
  workspace,
  selectedWorkspace,
  workspaceLookup,
  workspaceNameDisambiguators,
}: InProgressItemProps) {
  const isSelected = isSameWorkspace(workspace, selectedWorkspace);
  const fullWorkspace = workspaceLookup.get(workspace.dir);
  const displayText = getWorkspaceDisplayLabel(fullWorkspace ?? workspace, workspaceNameDisambiguators);
  const showTechnicalName = displayText !== workspace.name;

  return (
    <SidebarMenuItem>
      <Tooltip>
        <SidebarMenuButton
          asChild
          isActive={isSelected}
          className={cn(
            "ml-2 mb-0.5",
            !isSelected && "hover:bg-destructive/10",
            isSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
          )}
        >
          <TooltipTrigger asChild>
            <Link to={buildWorkspacePath(fullWorkspace ?? workspace, workspace.needsInput ? "respond" : "progress")}>
              <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                {displayText}
              </span>
              <WorkspaceIndicators workspace={workspace} />
            </Link>
          </TooltipTrigger>
        </SidebarMenuButton>
        <TooltipContent side="right">
          <div className="max-w-xs">
            <div className="font-medium">{displayText}</div>
            {showTechnicalName && (
              <div className={tooltipMetadataClassName}>Name: {workspace.name}</div>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </SidebarMenuItem>
  );
}

interface PinnedTreeItemProps {
  workspace: ApiWorkspaceEntry;
  selectedWorkspace: ApiWorkspaceEntry | null;
  workspaceLookup: Map<string, ApiWorkspaceEntry>;
  pinnedForks: ForkEntry[];
  workspaceNameDisambiguators: Map<string, string>;
}

function PinnedTreeItem({
  workspace,
  selectedWorkspace,
  workspaceLookup,
  pinnedForks,
  workspaceNameDisambiguators,
}: PinnedTreeItemProps) {
  const navigate = useNavigate();
  const fullWorkspace = workspaceLookup.get(workspace.dir);
  const isSelected = isSameWorkspace(workspace, selectedWorkspace);
  const hasForkSelected = pinnedForks.some((fork) => isSameWorkspace(fork, selectedWorkspace));
  const [expanded, setExpanded] = useState(() => isSelected || hasForkSelected);

  useEffect(() => {
    if (isSelected || hasForkSelected) {
      setExpanded(true);
    }
  }, [isSelected, hasForkSelected]);

  const displayText = getWorkspaceDisplayLabel(fullWorkspace ?? workspace, workspaceNameDisambiguators);
  const showTechnicalName = displayText !== workspace.name;
  const handleActionCompleted = useCallback(() => {
    if (isSameWorkspace(workspace, selectedWorkspace)) {
      navigate("/");
    }
  }, [navigate, selectedWorkspace, workspace]);

  return (
    <SidebarMenuItem className="mb-0.5">
      <div className="flex items-center gap-0 group/row">
        {pinnedForks.length > 0 ? (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setExpanded((prev) => !prev)}
            className="w-5 h-6 p-0 text-xs font-semibold shrink-0 mr-1 bg-muted text-muted-foreground hover:bg-secondary hover:text-secondary-foreground transition-colors self-start mt-1"
            aria-label={expanded ? `Collapse forks for ${displayText}` : `Expand forks for ${displayText}`}
            aria-expanded={expanded}
          >
            {expanded ? "−" : "+"}
          </Button>
        ) : (
          <span className="w-5 h-5 inline-block shrink-0 mr-1" />
        )}
        <Tooltip>
          <SidebarMenuButton
            asChild
            isActive={isSelected}
            className={cn(
              "flex-1 min-w-0",
              isSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
            <TooltipTrigger asChild>
              <Link to={buildWorkspacePath(fullWorkspace ?? workspace, "progress")}>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {displayText}
                </span>
                <WorkspaceIndicators workspace={workspace} />
              </Link>
            </TooltipTrigger>
          </SidebarMenuButton>
          <TooltipContent side="right">
            <div className="max-w-xs">
              <div className="font-medium">{displayText}</div>
              {showTechnicalName && (
                <div className={tooltipMetadataClassName}>Name: {workspace.name}</div>
              )}
            </div>
          </TooltipContent>
        </Tooltip>
        <WorkspaceRepositoryAction
          workspace={fullWorkspace ?? workspace}
          context="tree"
          triggerLabelSuffix={workspaceNameDisambiguators.get((fullWorkspace ?? workspace).dir)}
          onCompleted={handleActionCompleted}
        />
      </div>

      {pinnedForks.length > 0 && expanded && (
        <div className="ml-2.5 pl-4 relative before:content-[''] before:absolute before:left-2.5 before:top-0 before:bottom-2 before:w-0.5 before:bg-border before:rounded-sm">
          <SidebarMenu>
            {pinnedForks.map((fork) => (
              <ForkItem
                key={fork.dir}
                fork={fork}
                selectedWorkspace={selectedWorkspace}
                workspaceLookup={workspaceLookup}
                rootWorkspace={fullWorkspace ?? workspace}
                workspaceNameDisambiguators={workspaceNameDisambiguators}
              />
            ))}
          </SidebarMenu>
        </div>
      )}
    </SidebarMenuItem>
  );
}

interface OrphanPinnedForkItemProps {
  fork: ApiWorkspaceEntry;
  rootWorkspace: Pick<ApiWorkspaceEntry, "name" | "dir">;
  selectedWorkspace: ApiWorkspaceEntry | null;
  workspaceLookup: Map<string, ApiWorkspaceEntry>;
  workspaceNameDisambiguators: Map<string, string>;
}

function OrphanPinnedForkItem({
  fork,
  rootWorkspace,
  selectedWorkspace,
  workspaceLookup,
  workspaceNameDisambiguators,
}: OrphanPinnedForkItemProps) {
  const navigate = useNavigate();
  const forkSelected = isSameWorkspace(fork, selectedWorkspace);
  const forkFullEntry = workspaceLookup.get(fork.dir);
  const rootEntry = workspaceLookup.get(rootWorkspace.dir);
  const fallbackRootWorkspace = { ...rootWorkspace, title: "", computedTitle: "" };
  const rootWorkspaceLabelSource = rootEntry ?? fallbackRootWorkspace;
  const forkWorkspaceLabelSource = forkFullEntry ?? fork;
  const rootLabel = getWorkspaceDisplayLabel(rootWorkspaceLabelSource, workspaceNameDisambiguators);
  const forkLabel = getWorkspaceDisplayLabel(forkWorkspaceLabelSource, workspaceNameDisambiguators);
  const rootBaseLabel = getWorkspaceBaseLabel(rootWorkspaceLabelSource);
  const forkBaseLabel = getWorkspaceBaseLabel(forkWorkspaceLabelSource);
  const displayLabel = getOrphanPinnedForkDisplayLabel(rootLabel, forkLabel, rootBaseLabel, forkBaseLabel);
  const showTechnicalName = forkLabel !== fork.name;
  const handleActionCompleted = useCallback(() => {
    if (isSameWorkspace(fork, selectedWorkspace)) {
      navigate(buildWorkspacePath(rootWorkspace, "forks"));
    }
  }, [fork, navigate, rootWorkspace, selectedWorkspace]);

  return (
    <SidebarMenuItem>
      <div className="flex items-center gap-0 group/row">
        <span className="w-5 h-5 inline-block shrink-0 mr-1" />
        <Tooltip>
          <SidebarMenuButton
            asChild
            isActive={forkSelected}
            className={cn(
              "flex-1 min-w-0",
              forkSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
            <TooltipTrigger asChild>
              <Link to={buildWorkspacePath(forkFullEntry ?? fork, "progress")}>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {displayLabel}
                </span>
                <WorkspaceIndicators workspace={fork} />
              </Link>
            </TooltipTrigger>
          </SidebarMenuButton>
          <TooltipContent side="right">
            <div className="max-w-xs">
              <div className="font-medium">{forkLabel}</div>
              {showTechnicalName && (
                <div className={tooltipMetadataClassName}>Name: {fork.name}</div>
              )}
              <div className={tooltipMetadataClassName}>Root: {rootLabel}</div>
            </div>
          </TooltipContent>
        </Tooltip>
        {forkFullEntry ? (
          <WorkspaceRepositoryAction
            workspace={forkFullEntry}
            context="tree"
            triggerLabelSuffix={workspaceNameDisambiguators.get(forkFullEntry.dir)}
            onCompleted={handleActionCompleted}
          />
        ) : null}
      </div>
    </SidebarMenuItem>
  );
}

interface PinnedSectionProps {
  workspaces: ApiWorkspaceEntry[];
  selectedWorkspace: ApiWorkspaceEntry | null;
  workspaceLookup: Map<string, ApiWorkspaceEntry>;
  forkParentLookup: Map<string, ApiWorkspaceEntry>;
  workspaceNameDisambiguators: Map<string, string>;
}

function PinnedSection({
  workspaces,
  selectedWorkspace,
  workspaceLookup,
  forkParentLookup,
  workspaceNameDisambiguators,
}: PinnedSectionProps) {
  const pinned = useMemo(() => {
    return workspaces.filter((w) => w.pinned);
  }, [workspaces]);

  const pinnedRootsAndForks = useMemo(() => {
    const pinnedForks = pinned.filter((w) => w.isFork);
    const pinnedRoots = pinned.filter((w) => !w.isFork);
    const pinnedRootDirs = new Set(pinnedRoots.map((root) => root.dir));

    const forkGroups = new Map<string, ForkEntry[]>();
    const orphanForks: Array<{ fork: ApiWorkspaceEntry; rootWorkspace: Pick<ApiWorkspaceEntry, "name" | "dir"> }> = [];

    for (const fork of pinnedForks) {
      const parentWorkspace = forkParentLookup.get(fork.dir);
      if (parentWorkspace && pinnedRootDirs.has(parentWorkspace.dir)) {
        const existing = forkGroups.get(parentWorkspace.dir) || [];
        existing.push(workspaceToForkEntry(fork));
        forkGroups.set(parentWorkspace.dir, existing);
      } else {
        orphanForks.push({
          fork,
          rootWorkspace: parentWorkspace ?? { name: fork.name, dir: fork.dir },
        });
      }
    }

    return { pinnedRoots, forkGroups, orphanForks };
  }, [pinned, forkParentLookup]);

  if (pinned.length === 0) return null;

  const { pinnedRoots, forkGroups, orphanForks } = pinnedRootsAndForks;

  return (
    <div className="mb-3 pb-2 border-b" role="region" aria-label="Pinned">
      <SidebarMenu>
        {pinnedRoots.map((root) => (
          <PinnedTreeItem
            key={root.dir}
            workspace={root}
            selectedWorkspace={selectedWorkspace}
            workspaceLookup={workspaceLookup}
            pinnedForks={forkGroups.get(root.dir) || []}
            workspaceNameDisambiguators={workspaceNameDisambiguators}
          />
        ))}
        {orphanForks.map(({ fork, rootWorkspace }) => (
          <OrphanPinnedForkItem
            key={fork.dir}
            fork={fork}
            rootWorkspace={rootWorkspace}
            selectedWorkspace={selectedWorkspace}
            workspaceLookup={workspaceLookup}
            workspaceNameDisambiguators={workspaceNameDisambiguators}
          />
        ))}
      </SidebarMenu>
    </div>
  );
}

interface InProgressSectionProps {
  workspaces: ApiWorkspaceEntry[];
  selectedWorkspace: ApiWorkspaceEntry | null;
  workspaceLookup: Map<string, ApiWorkspaceEntry>;
  workspaceNameDisambiguators: Map<string, string>;
}

function InProgressSection({
  workspaces,
  selectedWorkspace,
  workspaceLookup,
  workspaceNameDisambiguators,
}: InProgressSectionProps) {
  const inProgress = useMemo(() => {
    return workspaces.filter((w) => (w.inProgress || w.running) && !w.pinned);
  }, [workspaces]);

  if (inProgress.length === 0) return null;

  return (
    <div className="mb-3 pb-2 border-b" role="region" aria-label="In progress">
      <SidebarMenu>
        {inProgress.map((w) => (
          <InProgressItem
            key={w.dir}
            workspace={w}
            selectedWorkspace={selectedWorkspace}
            workspaceLookup={workspaceLookup}
            workspaceNameDisambiguators={workspaceNameDisambiguators}
          />
        ))}
      </SidebarMenu>
    </div>
  );
}

interface WorkspaceListProps {
  workspaces: ApiWorkspaceEntry[];
  selectedWorkspace: ApiWorkspaceEntry | null;
}

function WorkspaceList({ workspaces, selectedWorkspace }: WorkspaceListProps) {
  const workspaceNameDisambiguators = useMemo(() => {
    return buildWorkspaceNameDisambiguators(workspaces);
  }, [workspaces]);

  const workspaceLookup = useMemo(() => {
    const map = new Map<string, ApiWorkspaceEntry>();
    for (const w of workspaces) {
      map.set(w.dir, w);
    }
    return map;
  }, [workspaces]);

  const forkParentLookup = useMemo(() => {
    const lookup = new Map<string, ApiWorkspaceEntry>();
    for (const w of workspaces) {
      if (w.forks) {
        for (const fork of w.forks) {
          lookup.set(fork.dir, w);
        }
      }
    }
    return lookup;
  }, [workspaces]);

  const deduplicatedWorkspaces = useMemo(() => {
    return deduplicateByDir(workspaces);
  }, [workspaces]);

  return (
    <>
      <PinnedSection
        workspaces={deduplicatedWorkspaces}
        selectedWorkspace={selectedWorkspace}
        workspaceLookup={workspaceLookup}
        forkParentLookup={forkParentLookup}
        workspaceNameDisambiguators={workspaceNameDisambiguators}
      />
      <InProgressSection
        workspaces={deduplicatedWorkspaces}
        selectedWorkspace={selectedWorkspace}
        workspaceLookup={workspaceLookup}
        workspaceNameDisambiguators={workspaceNameDisambiguators}
      />
      <SidebarMenu>
        {deduplicatedWorkspaces.length > 0 ? (
          deduplicatedWorkspaces.filter((w) => !w.isFork).map((workspace) => (
            <WorkspaceTreeItem
              key={workspace.dir}
              workspace={workspace}
              selectedWorkspace={selectedWorkspace}
              workspaceLookup={workspaceLookup}
              workspaceNameDisambiguators={workspaceNameDisambiguators}
            />
          ))
        ) : (
          <p className="text-sm text-muted-foreground italic p-2">No workspaces found.</p>
        )}
      </SidebarMenu>
    </>
  );
}

interface WorkspaceStatusEntry {
  name: string;
  dir: string;
  running: boolean;
  needsInput: boolean;
}

function deduplicateByDir<T extends { dir: string }>(workspaces: T[]): T[] {
  const seen = new Set<string>();
  return workspaces.filter((w) => {
    if (seen.has(w.dir)) return false;
    seen.add(w.dir);
    return true;
  });
}

function collectAllWorkspaces(workspaces: ApiWorkspaceEntry[]): WorkspaceStatusEntry[] {
  const all: WorkspaceStatusEntry[] = [];
  for (const w of workspaces) {
    all.push(w);
    if (w.forks) {
      for (const fork of w.forks) {
        all.push(fork);
      }
    }
  }
  return deduplicateByDir(all);
}

interface SidebarHeaderIndicatorsProps {
  workspaces: ApiWorkspaceEntry[];
}

function SidebarHeaderIndicators({ workspaces }: SidebarHeaderIndicatorsProps) {
  const navigate = useNavigate();
  const allWorkspaces = useMemo(() => collectAllWorkspaces(workspaces), [workspaces]);

  const needsInputCount = useMemo(
    () => allWorkspaces.filter((w) => w.needsInput).length,
    [allWorkspaces],
  );

  const hasAnyRunning = useMemo(
    () => allWorkspaces.some((w) => w.running || w.needsInput),
    [allWorkspaces],
  );

  const handleInboxClick = useCallback(() => {
    const firstNeedsInput = allWorkspaces.find((w) => w.needsInput);
    if (firstNeedsInput) {
      navigate(buildWorkspacePath(firstNeedsInput, "respond"));
    }
  }, [allWorkspaces, navigate]);

  return (
    <div className="flex items-center gap-2">
      {needsInputCount > 0 && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              onClick={handleInboxClick}
              aria-label={needsInputCount === 1
                ? "1 workspace waiting for response"
                : `${needsInputCount} workspaces waiting for response`}
              className="relative inline-flex items-center text-primary bg-transparent border-0 p-0 h-auto w-auto"
            >
              <Inbox className="h-4 w-4" />
              <Badge
                variant="destructive"
                className="absolute -top-2 -right-2.5 h-4 min-w-4 px-1 text-[0.6rem] leading-none flex items-center justify-center rounded-full"
              >
                {needsInputCount}
              </Badge>
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {needsInputCount === 1
              ? "1 workspace waiting for response"
              : `${needsInputCount} workspaces waiting for response`}
          </TooltipContent>
        </Tooltip>
      )}
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="text-sm cursor-help" aria-label={hasAnyRunning ? "Some factories running" : "All factories stopped"}>
            {hasAnyRunning ? "●" : "○"}
          </span>
        </TooltipTrigger>
        <TooltipContent>
          {hasAnyRunning ? "Some factories are running" : "All factories stopped"}
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

function MobileHeader({ workspaces, loading, error }: { workspaces: ApiWorkspaceEntry[]; loading: boolean; error: Error | null }) {
  return (
    <div className="flex items-center gap-2 pb-3 md:hidden">
      <SidebarTrigger />
      <span className="text-sm font-semibold">Workspaces</span>
      <span className="flex-1 flex justify-center">
        <img src={sgaiLogo} alt="SGAI" className="h-[28px] w-auto" />
      </span>
      {!loading && !error && (
        <SidebarHeaderIndicators workspaces={workspaces} />
      )}
    </div>
  );
}

interface DashboardContentProps {
  children: ReactNode;
  onSidebarResizeMouseDown: (e: React.MouseEvent) => void;
}

function DashboardContent({ children, onSidebarResizeMouseDown }: DashboardContentProps): JSX.Element {
  const { name: selectedName } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const { setOpenMobile } = useSidebar();

  const { workspaces, fetchStatus } = useFactoryState();
  const selectedWorkspace = useMemo(() => {
    if (!selectedName) {
      return null;
    }

    return resolveWorkspaceByName(workspaces, selectedName);
  }, [selectedName, workspaces]);
  const loading = fetchStatus === "fetching" && workspaces.length === 0;
  const error = fetchStatus === "error" && workspaces.length === 0
    ? new Error("Failed to load workspaces")
    : null;

  useEffect(() => {
    if (selectedName) {
      setOpenMobile(false);
    }
  }, [selectedName, setOpenMobile]);

  const handleAttachExternal = useCallback(() => {
    navigate("/workspaces/attach");
  }, [navigate]);

  return (
    <>
      <Sidebar side="left" collapsible="offcanvas">
        <SidebarHeader className="px-3 py-2">
          <div>
            <img src={sgaiLogo} alt="SGAI" className="h-[35px] w-auto" />
          </div>
          <Separator />
          <div className="flex items-center justify-between pt-2">
            <span className="text-sm font-semibold">Workspaces</span>
            {!loading && !error && <SidebarHeaderIndicators workspaces={workspaces} />}
          </div>
        </SidebarHeader>
        <Separator />
        <SidebarContent>
          <ScrollArea className="flex-1 px-1 py-2">
            {loading && <WorkspaceTreeSkeleton />}

            {error && (
              <p className="text-sm text-destructive p-2">
                Failed to load workspaces: {error.message}
              </p>
            )}

            {!loading && !error && (
              <WorkspaceList workspaces={workspaces} selectedWorkspace={selectedWorkspace} />
            )}
          </ScrollArea>
        </SidebarContent>
        <Separator />
        <SidebarFooter className="p-2">
          <Button
            type="button"
            variant="outline"
            className="w-full justify-center gap-2"
            aria-label="Attach external repository"
            onClick={handleAttachExternal}
          >
            <span>[ + ]</span>
            <LinkIcon className="h-4 w-4" />
            <span>Attach External</span>
          </Button>
        </SidebarFooter>
        <SidebarRail />
        <div
          className="absolute inset-y-0 right-0 z-30 hidden w-1.5 cursor-col-resize bg-transparent hover:bg-primary/20 transition-colors md:block"
          onMouseDown={onSidebarResizeMouseDown}
          aria-hidden="true"
        />
      </Sidebar>

      <div className="flex-1 flex flex-col min-w-0">
        <MobileHeader workspaces={workspaces} loading={loading} error={error} />
        <div className="hidden md:flex items-center gap-2 pl-2 pt-2">
          <SidebarTrigger />
        </div>
        <main className="flex-1 overflow-auto pt-2 md:pt-0 md:pl-4">
          {children}
        </main>
      </div>
    </>
  );
}

interface DashboardProps {
  children: ReactNode;
}

export function Dashboard({ children }: DashboardProps): JSX.Element {
  const { sidebarWidth, handleMouseDown } = useSidebarResize();

  const sidebarStyle = useMemo(
    () => ({ "--sidebar-width": `${sidebarWidth}px` } as CSSProperties),
    [sidebarWidth]
  );

  return (
    <SidebarProvider style={sidebarStyle}>
      <div className="flex min-h-[calc(100vh-4rem)] w-full">
        <DashboardContent onSidebarResizeMouseDown={handleMouseDown}>{children}</DashboardContent>
      </div>
    </SidebarProvider>
  );
}
