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
import { Inbox, Link as LinkIcon } from "lucide-react";
import { WorkspaceRepositoryAction } from "@/components/WorkspaceRepositoryAction";
import { useFactoryState } from "@/lib/factory-state";
import { useSidebarResize } from "@/hooks/useSidebarResize";
import { collectWorkspaceStatusEntries, deduplicateByDir } from "@/lib/pending-response";
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
import { sortByVisibleLabel } from "@/lib/workspace-sort";
import type { ApiRepositoryOperation } from "@/types";

type ForkEntry = NonNullable<ApiWorkspaceEntry["forks"]>[number];
type WorkspaceLabelSource = Pick<ApiWorkspaceEntry, "name" | "dir"> & Partial<Pick<ApiWorkspaceEntry, "title" | "computedTitle">>;

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

function getWorkspaceLabelSource(
  workspace: WorkspaceLabelSource,
  workspaceLookup: Map<string, ApiWorkspaceEntry>,
): Pick<ApiWorkspaceEntry, "name" | "dir" | "title" | "computedTitle"> {
  const fullWorkspace = workspaceLookup.get(workspace.dir);

  if (fullWorkspace) {
    return fullWorkspace;
  }

  return {
    ...workspace,
    title: workspace.title ?? "",
  };
}

function getVisibleWorkspaceLabel(
  workspace: WorkspaceLabelSource,
  workspaceLookup: Map<string, ApiWorkspaceEntry>,
  workspaceNameDisambiguators: Map<string, string>,
): string {
  return getWorkspaceDisplayLabel(
    getWorkspaceLabelSource(workspace, workspaceLookup),
    workspaceNameDisambiguators,
  );
}

function getForkLabelSource(
  fork: ForkEntry,
  workspaceLookup: Map<string, ApiWorkspaceEntry>,
): WorkspaceLabelSource {
  const fullWorkspace = workspaceLookup.get(fork.dir);

  return {
    ...(fullWorkspace ?? fork),
    title: fork.title || fullWorkspace?.title || "",
    computedTitle: fork.computedTitle || fullWorkspace?.computedTitle || "",
  };
}

function getVisibleForkLabel(
  fork: ForkEntry,
  workspaceLookup: Map<string, ApiWorkspaceEntry>,
  workspaceNameDisambiguators: Map<string, string>,
): string {
  return getWorkspaceDisplayLabel(
    getForkLabelSource(fork, workspaceLookup),
    workspaceNameDisambiguators,
  );
}

function getVisibleOrphanPinnedForkLabel(
  fork: ApiWorkspaceEntry,
  rootWorkspace: Pick<ApiWorkspaceEntry, "name" | "dir">,
  workspaceLookup: Map<string, ApiWorkspaceEntry>,
  workspaceNameDisambiguators: Map<string, string>,
): string {
  const rootEntry = workspaceLookup.get(rootWorkspace.dir);
  const forkEntry = workspaceLookup.get(fork.dir);
  const fallbackRootWorkspace = { ...rootWorkspace, title: "", computedTitle: "" };
  const rootWorkspaceLabelSource = rootEntry ?? fallbackRootWorkspace;
  const forkWorkspaceLabelSource = forkEntry ?? fork;
  const rootLabel = getWorkspaceDisplayLabel(rootWorkspaceLabelSource, workspaceNameDisambiguators);
  const forkLabel = getWorkspaceDisplayLabel(forkWorkspaceLabelSource, workspaceNameDisambiguators);
  const rootBaseLabel = getWorkspaceBaseLabel(rootWorkspaceLabelSource);
  const forkBaseLabel = getWorkspaceBaseLabel(forkWorkspaceLabelSource);

  return getOrphanPinnedForkDisplayLabel(rootLabel, forkLabel, rootBaseLabel, forkBaseLabel);
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
}

interface WorkspaceIndicatorsProps {
  workspace: WorkspaceIndicatorFields;
}

type WorkspaceIndicatorKey = keyof WorkspaceIndicatorFields;

interface WorkspaceIndicatorDefinition {
  key: WorkspaceIndicatorKey;
  label: string;
  activeToneClassName: string;
  activeGlyph: string;
  inactiveGlyph: string;
}

interface WorkspaceIndicatorSlotProps {
  active: boolean;
  label: string;
  activeToneClassName: string;
  activeGlyph: string;
  inactiveGlyph: string;
}

const workspaceIndicatorDefinitions: readonly WorkspaceIndicatorDefinition[] = [
  {
    key: "running",
    label: "Running",
    activeToneClassName: "text-emerald-600 dark:text-emerald-400",
    activeGlyph: "▲",
    inactiveGlyph: "△",
  },
  {
    key: "needsInput",
    label: "Waiting for response",
    activeToneClassName: "text-amber-600 dark:text-amber-400",
    activeGlyph: "●",
    inactiveGlyph: "○",
  },
  {
    key: "pinned",
    label: "Pinned",
    activeToneClassName: "text-primary",
    activeGlyph: "■",
    inactiveGlyph: "□",
  },
];

function WorkspaceIndicatorSlot({
  active,
  label,
  activeToneClassName,
  activeGlyph,
  inactiveGlyph,
}: WorkspaceIndicatorSlotProps) {
  const stateLabel = `${label}: ${active ? "on" : "off"}`;
  const glyph = active ? activeGlyph : inactiveGlyph;
  const slot = (
    <span
      className={cn(
        "inline-flex w-4 cursor-help justify-center font-mono text-[0.75rem] font-semibold leading-none",
        active ? activeToneClassName : "text-muted-foreground/45",
      )}
      aria-hidden={!active}
      aria-label={active ? label : undefined}
      role={active ? "img" : undefined}
    >
      {glyph}
    </span>
  );

  return (
    <Tooltip>
      <TooltipTrigger asChild>{slot}</TooltipTrigger>
      <TooltipContent>{stateLabel}</TooltipContent>
    </Tooltip>
  );
}

function WorkspaceIndicators({ workspace }: WorkspaceIndicatorsProps) {
  const indicatorSummary = workspaceIndicatorDefinitions
    .map(({ key, label }) => `${label}: ${workspace[key] ? "on" : "off"}`)
    .join(", ");

  return (
    <span
      className="ml-2 inline-grid shrink-0 grid-cols-3 items-center justify-items-center gap-0.5"
      role="group"
      aria-label={indicatorSummary}
    >
      {workspaceIndicatorDefinitions.map(({ key, label, activeToneClassName, activeGlyph, inactiveGlyph }) => (
        <WorkspaceIndicatorSlot
          key={key}
          active={workspace[key]}
          label={label}
          activeToneClassName={activeToneClassName}
          activeGlyph={activeGlyph}
          inactiveGlyph={inactiveGlyph}
        />
      ))}
    </span>
  );
}

function useTreeRowTooltipState() {
  const [rowTooltipOpen, setRowTooltipOpen] = useState(false);

  const handleRowTooltipOpenChange = useCallback((nextOpen: boolean) => {
    setRowTooltipOpen(nextOpen);
  }, []);

  const handleLinkFocus = useCallback(() => {
    setRowTooltipOpen(true);
  }, []);

  const handleLinkBlur = useCallback(() => {
    setRowTooltipOpen(false);
  }, []);

  return {
    rowTooltipOpen,
    handleRowTooltipOpenChange,
    handleLinkFocus,
    handleLinkBlur,
  };
}

interface WorkspaceTreeActionSlotProps {
  workspace: Pick<ApiWorkspaceEntry, "name" | "dir" | "repositoryAction">;
  triggerLabelSuffix?: string;
  onCompleted?: (operation: ApiRepositoryOperation) => void;
}

interface WorkspaceTreeTrailingActionProps {
  workspace?: Pick<ApiWorkspaceEntry, "name" | "dir" | "repositoryAction"> | null;
  triggerLabelSuffix?: string;
  onCompleted?: (operation: ApiRepositoryOperation) => void;
}

function usesLeftTreeDetachSlot(
  workspace: Pick<ApiWorkspaceEntry, "repositoryAction">,
): boolean {
  const action = workspace.repositoryAction;

  return Boolean(action && action.entryPoint !== "hidden" && action.presentation.icon === "detach");
}

function usesTrailingTreeAction(
  workspace: Pick<ApiWorkspaceEntry, "repositoryAction">,
): boolean {
  const action = workspace.repositoryAction;

  return Boolean(action && action.entryPoint !== "hidden" && action.presentation.icon !== "detach");
}

function WorkspaceTreeActionSlot({
  workspace,
  triggerLabelSuffix,
  onCompleted,
}: WorkspaceTreeActionSlotProps) {
  if (!usesLeftTreeDetachSlot(workspace)) {
    return null;
  }

  return (
    <span
      data-slot="workspace-tree-action-slot"
      className="mr-1 inline-flex h-6 w-5 shrink-0 items-center justify-center"
    >
      <WorkspaceRepositoryAction
        workspace={workspace}
        context="tree"
        triggerLabelSuffix={triggerLabelSuffix}
        onCompleted={onCompleted}
      />
    </span>
  );
}

function WorkspaceTreeTrailingRail({ children }: { children?: ReactNode }) {
  return (
    <span
      data-slot="workspace-tree-trailing-action-rail"
      className="ml-1 inline-flex h-6 w-5 shrink-0 items-center justify-center"
      aria-hidden={children ? undefined : true}
    >
      {children}
    </span>
  );
}

function WorkspaceTreeTrailingAction({
  workspace,
  triggerLabelSuffix,
  onCompleted,
}: WorkspaceTreeTrailingActionProps) {
  const showTrailingAction = Boolean(workspace && usesTrailingTreeAction(workspace));

  return (
    <WorkspaceTreeTrailingRail>
      {showTrailingAction && workspace ? (
        <span
          data-slot="workspace-tree-trailing-action"
          className="inline-flex h-full w-full items-center justify-center"
        >
          <WorkspaceRepositoryAction
            workspace={workspace}
            context="tree"
            triggerLabelSuffix={triggerLabelSuffix}
            onCompleted={onCompleted}
          />
        </span>
      ) : null}
    </WorkspaceTreeTrailingRail>
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
  const forkLabel = getVisibleForkLabel(fork, workspaceLookup, workspaceNameDisambiguators);
  const showTechnicalName = forkLabel !== fork.name;
  const { rowTooltipOpen, handleRowTooltipOpenChange, handleLinkFocus, handleLinkBlur } = useTreeRowTooltipState();
  const handleActionCompleted = useCallback(() => {
    if (isSameWorkspace(fork, selectedWorkspace) && rootWorkspace) {
      navigate(buildWorkspacePath(rootWorkspace, "forks"));
    }
  }, [fork, navigate, rootWorkspace, selectedWorkspace]);

  return (
    <SidebarMenuItem>
      <div className="flex items-center gap-0 group/row">
        {forkFullEntry ? (
          <WorkspaceTreeActionSlot
            workspace={forkFullEntry}
            triggerLabelSuffix={workspaceNameDisambiguators.get(forkFullEntry.dir)}
            onCompleted={handleActionCompleted}
          />
        ) : null}
        <Tooltip open={rowTooltipOpen} onOpenChange={handleRowTooltipOpenChange}>
          <SidebarMenuButton
            asChild
            isActive={forkSelected}
            className={cn(
              "flex-1 min-w-0 relative before:content-[''] before:absolute before:left-[-0.875rem] before:top-1/2 before:w-3.5 before:h-0.5 before:bg-border before:rounded-sm",
              forkSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
              <Link
                to={buildWorkspacePath(forkFullEntry ?? fork, "progress")}
                onFocus={handleLinkFocus}
                onBlur={handleLinkBlur}
              >
                <TooltipTrigger asChild>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {forkLabel}
                </span>
                </TooltipTrigger>
                <WorkspaceIndicators workspace={fork} />
              </Link>
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
        <WorkspaceTreeTrailingAction
          workspace={forkFullEntry}
          triggerLabelSuffix={forkFullEntry ? workspaceNameDisambiguators.get(forkFullEntry.dir) : undefined}
          onCompleted={handleActionCompleted}
        />
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
  const sortedForks = useMemo(
    () => sortByVisibleLabel(
      forks,
      (fork) => getVisibleForkLabel(fork, workspaceLookup, workspaceNameDisambiguators),
      (fork) => fork.dir,
    ),
    [forks, workspaceLookup, workspaceNameDisambiguators],
  );
  const hasForks = sortedForks.length > 0;
  const isSelected = isSameWorkspace(workspace, selectedWorkspace);
  const hasForkSelected = sortedForks.some((fork) => isSameWorkspace(fork, selectedWorkspace));
  const [expanded, setExpanded] = useState(() => isSelected || hasForkSelected);

  useEffect(() => {
    if (isSelected || hasForkSelected) {
      setExpanded(true);
    }
  }, [isSelected, hasForkSelected]);

  const displayText = getVisibleWorkspaceLabel(workspace, workspaceLookup, workspaceNameDisambiguators);
  const showTechnicalName = displayText !== workspace.name;
  const { rowTooltipOpen, handleRowTooltipOpenChange, handleLinkFocus, handleLinkBlur } = useTreeRowTooltipState();
  const treeActionWorkspace = fullWorkspace ?? workspace;
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
        <WorkspaceTreeActionSlot
          workspace={treeActionWorkspace}
          triggerLabelSuffix={workspaceNameDisambiguators.get(treeActionWorkspace.dir)}
          onCompleted={handleActionCompleted}
        />
        <Tooltip open={rowTooltipOpen} onOpenChange={handleRowTooltipOpenChange}>
          <SidebarMenuButton
            asChild
            isActive={isSelected}
            className={cn(
              "flex-1 min-w-0",
                isSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
              <Link
                to={buildWorkspacePath(treeActionWorkspace, "progress")}
                onFocus={handleLinkFocus}
                onBlur={handleLinkBlur}
              >
                <TooltipTrigger asChild>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {displayText}
                </span>
                </TooltipTrigger>
                <WorkspaceIndicators workspace={workspace} />
              </Link>
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
        <WorkspaceTreeTrailingAction
          workspace={treeActionWorkspace}
          triggerLabelSuffix={workspaceNameDisambiguators.get(treeActionWorkspace.dir)}
          onCompleted={handleActionCompleted}
        />
      </div>

      {hasForks && expanded && (
        <div className="ml-2.5 pl-4 relative before:content-[''] before:absolute before:left-2.5 before:top-0 before:bottom-2 before:w-0.5 before:bg-border before:rounded-sm">
          <SidebarMenu>
            {sortedForks.map((fork) => (
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
  const displayText = getVisibleWorkspaceLabel(workspace, workspaceLookup, workspaceNameDisambiguators);
  const showTechnicalName = displayText !== workspace.name;
  const { rowTooltipOpen, handleRowTooltipOpenChange, handleLinkFocus, handleLinkBlur } = useTreeRowTooltipState();

  return (
    <SidebarMenuItem>
      <div className="flex items-center gap-0 group/row">
        <Tooltip open={rowTooltipOpen} onOpenChange={handleRowTooltipOpenChange}>
          <SidebarMenuButton
            asChild
            isActive={isSelected}
            className={cn(
              "ml-2 mb-0.5 flex-1 min-w-0",
              !isSelected && "hover:bg-destructive/10",
              isSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
              <Link
                to={buildWorkspacePath(fullWorkspace ?? workspace, workspace.needsInput ? "respond" : "progress")}
                onFocus={handleLinkFocus}
                onBlur={handleLinkBlur}
              >
                <TooltipTrigger asChild>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {displayText}
                </span>
                </TooltipTrigger>
                <WorkspaceIndicators workspace={workspace} />
              </Link>
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
        <WorkspaceTreeTrailingRail />
      </div>
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
  const sortedPinnedForks = useMemo(
    () => sortByVisibleLabel(
      pinnedForks,
      (fork) => getVisibleForkLabel(fork, workspaceLookup, workspaceNameDisambiguators),
      (fork) => fork.dir,
    ),
    [pinnedForks, workspaceLookup, workspaceNameDisambiguators],
  );
  const hasForkSelected = sortedPinnedForks.some((fork) => isSameWorkspace(fork, selectedWorkspace));
  const [expanded, setExpanded] = useState(() => isSelected || hasForkSelected);

  useEffect(() => {
    if (isSelected || hasForkSelected) {
      setExpanded(true);
    }
  }, [isSelected, hasForkSelected]);

  const displayText = getVisibleWorkspaceLabel(workspace, workspaceLookup, workspaceNameDisambiguators);
  const showTechnicalName = displayText !== workspace.name;
  const { rowTooltipOpen, handleRowTooltipOpenChange, handleLinkFocus, handleLinkBlur } = useTreeRowTooltipState();
  const treeActionWorkspace = fullWorkspace ?? workspace;
  const handleActionCompleted = useCallback(() => {
    if (isSameWorkspace(workspace, selectedWorkspace)) {
      navigate("/");
    }
  }, [navigate, selectedWorkspace, workspace]);

  return (
    <SidebarMenuItem className="mb-0.5">
      <div className="flex items-center gap-0 group/row">
        {sortedPinnedForks.length > 0 ? (
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
        <WorkspaceTreeActionSlot
          workspace={treeActionWorkspace}
          triggerLabelSuffix={workspaceNameDisambiguators.get(treeActionWorkspace.dir)}
          onCompleted={handleActionCompleted}
        />
        <Tooltip open={rowTooltipOpen} onOpenChange={handleRowTooltipOpenChange}>
          <SidebarMenuButton
            asChild
            isActive={isSelected}
            className={cn(
              "flex-1 min-w-0",
                isSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
              <Link
                to={buildWorkspacePath(treeActionWorkspace, "progress")}
                onFocus={handleLinkFocus}
                onBlur={handleLinkBlur}
              >
                <TooltipTrigger asChild>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {displayText}
                </span>
                </TooltipTrigger>
                <WorkspaceIndicators workspace={workspace} />
              </Link>
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
        <WorkspaceTreeTrailingAction
          workspace={treeActionWorkspace}
          triggerLabelSuffix={workspaceNameDisambiguators.get(treeActionWorkspace.dir)}
          onCompleted={handleActionCompleted}
        />
      </div>

      {sortedPinnedForks.length > 0 && expanded && (
        <div className="ml-2.5 pl-4 relative before:content-[''] before:absolute before:left-2.5 before:top-0 before:bottom-2 before:w-0.5 before:bg-border before:rounded-sm">
          <SidebarMenu>
            {sortedPinnedForks.map((fork) => (
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
  const rootLabel = getVisibleWorkspaceLabel(rootWorkspace, workspaceLookup, workspaceNameDisambiguators);
  const forkLabel = getVisibleWorkspaceLabel(fork, workspaceLookup, workspaceNameDisambiguators);
  const displayLabel = getVisibleOrphanPinnedForkLabel(
    fork,
    rootWorkspace,
    workspaceLookup,
    workspaceNameDisambiguators,
  );
  const showTechnicalName = forkLabel !== fork.name;
  const { rowTooltipOpen, handleRowTooltipOpenChange, handleLinkFocus, handleLinkBlur } = useTreeRowTooltipState();
  const handleActionCompleted = useCallback(() => {
    if (isSameWorkspace(fork, selectedWorkspace)) {
      navigate(buildWorkspacePath(rootWorkspace, "forks"));
    }
  }, [fork, navigate, rootWorkspace, selectedWorkspace]);

  return (
    <SidebarMenuItem>
      <div className="flex items-center gap-0 group/row">
        <span className="w-5 h-5 inline-block shrink-0 mr-1" />
        {forkFullEntry ? (
          <WorkspaceTreeActionSlot
            workspace={forkFullEntry}
            triggerLabelSuffix={workspaceNameDisambiguators.get(forkFullEntry.dir)}
            onCompleted={handleActionCompleted}
          />
        ) : null}
        <Tooltip open={rowTooltipOpen} onOpenChange={handleRowTooltipOpenChange}>
          <SidebarMenuButton
            asChild
            isActive={forkSelected}
            className={cn(
              "flex-1 min-w-0",
              forkSelected && "border-l-[3px] border-l-primary bg-primary/15 font-medium"
            )}
          >
              <Link
                to={buildWorkspacePath(forkFullEntry ?? fork, "progress")}
                onFocus={handleLinkFocus}
                onBlur={handleLinkBlur}
              >
                <TooltipTrigger asChild>
                <span className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {displayLabel}
                </span>
                </TooltipTrigger>
                <WorkspaceIndicators workspace={fork} />
              </Link>
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
        <WorkspaceTreeTrailingAction
          workspace={forkFullEntry}
          triggerLabelSuffix={forkFullEntry ? workspaceNameDisambiguators.get(forkFullEntry.dir) : undefined}
          onCompleted={handleActionCompleted}
        />
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

  const pinnedRows = useMemo(() => {
    return sortByVisibleLabel(
      [
        ...pinnedRootsAndForks.pinnedRoots.map((workspace) => ({ kind: "root" as const, workspace })),
        ...pinnedRootsAndForks.orphanForks.map(({ fork, rootWorkspace }) => ({
          kind: "orphan-fork" as const,
          fork,
          rootWorkspace,
        })),
      ],
      (item) => item.kind === "root"
        ? getVisibleWorkspaceLabel(item.workspace, workspaceLookup, workspaceNameDisambiguators)
        : getVisibleOrphanPinnedForkLabel(
          item.fork,
          item.rootWorkspace,
          workspaceLookup,
          workspaceNameDisambiguators,
        ),
      (item) => item.kind === "root" ? item.workspace.dir : item.fork.dir,
    );
  }, [pinnedRootsAndForks, workspaceLookup, workspaceNameDisambiguators]);

  if (pinned.length === 0) return null;

  return (
    <div className="mb-3 pb-2 border-b" role="region" aria-label="Pinned">
      <SidebarMenu>
        {pinnedRows.map((item) => item.kind === "root" ? (
          <PinnedTreeItem
            key={item.workspace.dir}
            workspace={item.workspace}
            selectedWorkspace={selectedWorkspace}
            workspaceLookup={workspaceLookup}
            pinnedForks={pinnedRootsAndForks.forkGroups.get(item.workspace.dir) || []}
            workspaceNameDisambiguators={workspaceNameDisambiguators}
          />
        ) : (
          <OrphanPinnedForkItem
            key={item.fork.dir}
            fork={item.fork}
            rootWorkspace={item.rootWorkspace}
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
  const sortedInProgress = useMemo(
    () => sortByVisibleLabel(
      inProgress,
      (workspace) => getVisibleWorkspaceLabel(workspace, workspaceLookup, workspaceNameDisambiguators),
      (workspace) => workspace.dir,
    ),
    [inProgress, workspaceLookup, workspaceNameDisambiguators],
  );

  if (inProgress.length === 0) return null;

  return (
    <div className="mb-3 pb-2 border-b" role="region" aria-label="In progress">
      <SidebarMenu>
        {sortedInProgress.map((w) => (
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
  const sortedTopLevelWorkspaces = useMemo(
    () => sortByVisibleLabel(
      deduplicatedWorkspaces.filter((workspace) => !workspace.isFork),
      (workspace) => getVisibleWorkspaceLabel(workspace, workspaceLookup, workspaceNameDisambiguators),
      (workspace) => workspace.dir,
    ),
    [deduplicatedWorkspaces, workspaceLookup, workspaceNameDisambiguators],
  );

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
          sortedTopLevelWorkspaces.map((workspace) => (
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

interface SidebarHeaderIndicatorsProps {
  workspaces: ApiWorkspaceEntry[];
}

function SidebarHeaderIndicators({ workspaces }: SidebarHeaderIndicatorsProps) {
  const navigate = useNavigate();
  const allWorkspaces = useMemo(() => collectWorkspaceStatusEntries(workspaces), [workspaces]);

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
