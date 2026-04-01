import { forwardRef, useCallback, useEffect, useState, type MouseEvent } from "react";
import { ArrowRightLeft, Link2Off, Trash2, type LucideIcon } from "lucide-react";
import { Button, type ButtonProps } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { api } from "@/lib/api";
import { triggerFactoryRefresh } from "@/lib/factory-state";
import { cn } from "@/lib/utils";
import type {
  ApiRepositoryAction,
  ApiRepositoryActionIcon,
  ApiRepositoryActionOperationPresentation,
  ApiRepositoryActionTone,
  ApiRepositoryOperation,
  ApiWorkspaceEntry,
} from "@/types";

type WorkspaceActionContext = "tree" | "detail" | "fork-row";

interface WorkspaceRepositoryActionProps {
  workspace: Pick<ApiWorkspaceEntry, "name" | "dir" | "repositoryAction">;
  context: WorkspaceActionContext;
  triggerLabelSuffix?: string;
  onCompleted?: (operation: ApiRepositoryOperation) => void;
}

const repositoryActionIcons: Record<ApiRepositoryActionIcon, LucideIcon> = {
  choose: ArrowRightLeft,
  detach: Link2Off,
  delete: Trash2,
};

const treeTriggerGlyphs: Record<ApiRepositoryActionIcon, string> = {
  choose: "⋯",
  detach: "⊘",
  delete: "✕",
};

function getConfirmOperation(action: ApiRepositoryAction): ApiRepositoryOperation | null {
  if (!action.defaultOperation) {
    return null;
  }

  return action.allowedOperations.includes(action.defaultOperation) ? action.defaultOperation : null;
}

function getOperationPresentation(
  action: ApiRepositoryAction,
  operation: ApiRepositoryOperation,
): ApiRepositoryActionOperationPresentation | null {
  return action.presentation.operations.find((item) => item.operation === operation) ?? null;
}

function getTriggerLabel(action: ApiRepositoryAction, context: WorkspaceActionContext): string {
  if (context === "detail") {
    return action.presentation.detailTriggerLabel;
  }

  if (context === "fork-row") {
    return action.presentation.forkRowTriggerLabel;
  }

  return action.presentation.treeTriggerLabel;
}

function getButtonVariant(tone: ApiRepositoryActionTone): "outline" | "destructive" {
  return tone === "destructive" ? "destructive" : "outline";
}

type TriggerButtonProps = Omit<ButtonProps, "children" | "size" | "variant"> & {
  label: string;
  icon: ApiRepositoryActionIcon;
  tone: ApiRepositoryActionTone;
};

const TreeTrigger = forwardRef<HTMLButtonElement, TriggerButtonProps>(function TreeTrigger({
  label,
  icon,
  tone,
  className,
  ...props
}, ref) {
  const isDestructive = tone === "destructive";
  const glyph = treeTriggerGlyphs[icon];

  return (
    <Button
      ref={ref}
      type="button"
      variant="ghost"
      size="icon"
      className={cn(
        "h-6 w-5 shrink-0 rounded px-0 font-mono text-[0.75rem] font-semibold leading-none opacity-80 transition-colors hover:opacity-100 focus-visible:opacity-100",
        isDestructive
          ? "text-destructive/75 hover:bg-destructive/15 hover:text-destructive"
          : "text-muted-foreground hover:bg-accent hover:text-foreground",
        className,
      )}
      aria-label={label}
      {...props}
    >
      <span aria-hidden="true">{glyph}</span>
    </Button>
  );
});

TreeTrigger.displayName = "TreeTrigger";

const ForkRowTrigger = forwardRef<HTMLButtonElement, TriggerButtonProps>(function ForkRowTrigger({
  label,
  icon,
  tone,
  className,
  ...props
}, ref) {
  const Icon = repositoryActionIcons[icon];
  const isDestructive = tone === "destructive";

  return (
    <Button
      ref={ref}
      type="button"
      size="icon"
      variant="ghost"
      className={cn(
        "h-7 w-7",
        isDestructive ? "text-destructive hover:text-destructive" : "text-muted-foreground",
        className,
      )}
      aria-label={label}
      {...props}
    >
      <Icon className="h-3.5 w-3.5" />
    </Button>
  );
});

ForkRowTrigger.displayName = "ForkRowTrigger";

export function WorkspaceRepositoryAction({
  workspace,
  context,
  triggerLabelSuffix,
  onCompleted,
}: WorkspaceRepositoryActionProps) {
  const action = workspace.repositoryAction;
  const actionResetKey = JSON.stringify({
    name: workspace.name,
    dir: workspace.dir,
    entryPoint: action?.entryPoint ?? "",
    defaultOperation: action?.defaultOperation ?? "",
    allowedOperations: action?.allowedOperations ?? [],
    detailTriggerLabel: action?.presentation?.detailTriggerLabel ?? "",
    treeTriggerLabel: action?.presentation?.treeTriggerLabel ?? "",
    forkRowTriggerLabel: action?.presentation?.forkRowTriggerLabel ?? "",
    triggerLabelSuffix: triggerLabelSuffix ?? "",
  });
  const [open, setOpen] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [completed, setCompleted] = useState(false);
  const [isPending, setIsPending] = useState(false);

  useEffect(() => {
    setCompleted(false);
    setIsPending(false);
    setOpen(false);
    setActionError(null);
  }, [actionResetKey]);

  const handleOpenChange = useCallback((nextOpen: boolean) => {
    if (completed) {
      return;
    }
    if (isPending && !nextOpen) {
      return;
    }
    setOpen(nextOpen);
    if (!nextOpen) {
      setActionError(null);
    }
  }, [completed, isPending]);

  const handleTriggerClick = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
  }, []);

  const handleOperation = useCallback((operation: ApiRepositoryOperation) => {
    if (isPending) {
      return;
    }
    setActionError(null);
    setIsPending(true);
    void (async () => {
      try {
        await api.workspaces.deleteWorkspace(workspace.name, operation);
        triggerFactoryRefresh();
        setOpen(false);
        setCompleted(true);
        onCompleted?.(operation);
      } catch (err) {
        setActionError(err instanceof Error ? err.message : "Failed to update workspace");
        setIsPending(false);
      }
    })();
  }, [isPending, onCompleted, workspace.name]);

  if (!action || action.entryPoint === "hidden" || !action.presentation || completed) {
    return null;
  }

  const triggerLabel = getTriggerLabel(action, context);
  const accessibleTriggerLabel = triggerLabelSuffix && context !== "detail"
    ? `${triggerLabel} · ${triggerLabelSuffix}`
    : triggerLabel;
  const choose = action.entryPoint === "choose";
  const confirmOperation = choose ? null : getConfirmOperation(action);
  const confirmOperationPresentation = confirmOperation ? getOperationPresentation(action, confirmOperation) : null;
  const allowedOperations = new Set(action.allowedOperations);

  if (!choose && (!confirmOperation || !confirmOperationPresentation)) {
    return null;
  }

  const triggerTone = action.presentation.tone;
  const TriggerIcon = repositoryActionIcons[action.presentation.icon];
  const chooseActionButtons = choose ? (
    action.presentation.operations.filter((operationPresentation) => allowedOperations.has(operationPresentation.operation)).map((operationPresentation) => {
      const OperationIcon = repositoryActionIcons[operationPresentation.icon];

      return (
        <AlertDialogAction asChild key={operationPresentation.operation}>
          <Button
            type="button"
            variant={getButtonVariant(operationPresentation.tone)}
            disabled={isPending}
            onClick={(event) => {
              event.preventDefault();
              handleOperation(operationPresentation.operation);
            }}
          >
            <OperationIcon className="mr-2 h-4 w-4" />
            {operationPresentation.label}
          </Button>
        </AlertDialogAction>
      );
    })
  ) : null;
  const confirmActionButton = !choose && confirmOperation && confirmOperationPresentation ? (
    <AlertDialogAction asChild>
      <Button
        type="button"
        variant={getButtonVariant(confirmOperationPresentation.tone)}
        disabled={isPending}
        onClick={(event) => {
          event.preventDefault();
          if (confirmOperation) {
            handleOperation(confirmOperation);
          }
        }}
      >
        <TriggerIcon className="mr-2 h-4 w-4" />
        {confirmOperationPresentation.label}
      </Button>
    </AlertDialogAction>
  ) : null;

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogTrigger asChild>
        {context === "detail" ? (
          <Button
            type="button"
            size="sm"
            variant={getButtonVariant(triggerTone)}
            onClick={handleTriggerClick}
          >
            <TriggerIcon className="mr-2 h-4 w-4" />
            {triggerLabel}
          </Button>
        ) : context === "fork-row" ? (
            <ForkRowTrigger
              label={accessibleTriggerLabel}
              icon={action.presentation.icon}
              tone={triggerTone}
              onClick={handleTriggerClick}
            />
          ) : (
            <TreeTrigger
              label={accessibleTriggerLabel}
              icon={action.presentation.icon}
              tone={triggerTone}
              onClick={handleTriggerClick}
          />
        )}
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{action.presentation.dialogTitle}</AlertDialogTitle>
          <AlertDialogDescription>{action.presentation.dialogDescription}</AlertDialogDescription>
        </AlertDialogHeader>
        {actionError ? (
          <p className="text-sm text-destructive" role="alert">{actionError}</p>
        ) : null}
        {choose ? (
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
            <div
              data-slot="repository-action-buttons"
              className="flex flex-col gap-2 sm:flex-row"
            >
              {chooseActionButtons}
            </div>
            <AlertDialogCancel className="mt-0 sm:order-first" disabled={isPending}>Cancel</AlertDialogCancel>
          </div>
        ) : (
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isPending}>Cancel</AlertDialogCancel>
            {confirmActionButton}
          </AlertDialogFooter>
        )}
      </AlertDialogContent>
    </AlertDialog>
  );
}
