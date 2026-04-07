import { useCallback, useState, type FormEvent } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button, type ButtonProps } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ApiActionEntry } from "@/types";

interface ActionBarProps {
  actions: ApiActionEntry[];
  isRunning: boolean;
  onActionClick: (action: ApiActionEntry, variables: Record<string, string>) => void;
  actionConfigError?: string;
  disabledReason?: string;
  accessibilityContext?: string;
  className?: string;
  buttonClassName?: string;
  buttonSize?: ButtonProps["size"];
  showValidationErrors?: boolean;
}

function normalizeAccessibilityContext(accessibilityContext?: string): string {
  return accessibilityContext?.trim() ?? "";
}

function actionToolbarLabel(accessibilityContext?: string): string {
  const normalizedAccessibilityContext = normalizeAccessibilityContext(accessibilityContext);
  return normalizedAccessibilityContext ? `Action buttons for ${normalizedAccessibilityContext}` : "Action buttons";
}

function actionButtonLabel(actionName: string, accessibilityContext?: string): string {
  const normalizedAccessibilityContext = normalizeAccessibilityContext(accessibilityContext);
  return normalizedAccessibilityContext ? `${actionName} for ${normalizedAccessibilityContext}` : actionName;
}

function actionTooltipText(action: ApiActionEntry): string {
  const validationError = action.validationError?.trim();
  if (validationError) {
    return validationError;
  }

  const description = action.description?.trim();
  if (description) {
    return description;
  }

  if (action.kind === "prompt" && action.model) {
    return action.model;
  }

  if (action.kind === "script") {
    return "Script action";
  }

  return "Action";
}

function actionVariableDefaults(action: ApiActionEntry | null): Record<string, string> {
  if (!action?.variables || action.variables.length === 0) {
    return {};
  }

  return Object.fromEntries(action.variables.map((variable) => [variable, ""]));
}

export function ActionBar({
  actions,
  isRunning,
  onActionClick,
  actionConfigError,
  disabledReason,
  accessibilityContext,
  className,
  buttonClassName,
  buttonSize = "sm",
  showValidationErrors = true,
}: ActionBarProps) {
  const [dialogAction, setDialogAction] = useState<ApiActionEntry | null>(null);
  const [variableValues, setVariableValues] = useState<Record<string, string>>({});

  const normalizedActionConfigError = actionConfigError?.trim() ?? "";
  const normalizedDisabledReason = disabledReason?.trim() ?? "";
  const invalidActions = actions.filter((action) => Boolean(action.validationError?.trim()));
  const disableAllActions = isRunning || Boolean(normalizedActionConfigError) || Boolean(normalizedDisabledReason);

  const closeDialog = useCallback(() => {
    setDialogAction(null);
    setVariableValues({});
  }, []);

  const handleActionRequest = useCallback((action: ApiActionEntry) => {
    if (disableAllActions || action.validationError?.trim()) {
      return;
    }

    if (!action.variables || action.variables.length === 0) {
      onActionClick(action, {});
      return;
    }

    setDialogAction(action);
    setVariableValues(actionVariableDefaults(action));
  }, [disableAllActions, onActionClick]);

  const handleVariableChange = useCallback((variable: string, value: string) => {
    setVariableValues((current) => ({ ...current, [variable]: value }));
  }, []);

  const handleDialogSubmit = useCallback((event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!dialogAction || disableAllActions) {
      return;
    }

    onActionClick(dialogAction, variableValues);
    closeDialog();
  }, [closeDialog, dialogAction, disableAllActions, onActionClick, variableValues]);

  if (actions.length === 0 && !normalizedActionConfigError) {
    return null;
  }

  return (
    <>
      {actions.length > 0 ? (
        <div
          className={cn("flex flex-wrap items-center gap-2", className)}
          role="toolbar"
          aria-label={actionToolbarLabel(accessibilityContext)}
        >
          {actions.map((action) => (
            <Tooltip key={`${action.name}-${action.kind ?? "action"}`}>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  size={buttonSize}
                  className={buttonClassName}
                  disabled={disableAllActions || Boolean(action.validationError?.trim())}
                  aria-label={actionButtonLabel(action.name, accessibilityContext)}
                  onClick={() => handleActionRequest(action)}
                >
                  {action.name}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{actionTooltipText(action)}</TooltipContent>
            </Tooltip>
          ))}
        </div>
      ) : null}

      {normalizedActionConfigError ? (
        <Alert className="border-destructive/50 text-destructive">
          <AlertDescription>
            <span className="font-medium">Action configuration error:</span> {normalizedActionConfigError}
          </AlertDescription>
        </Alert>
      ) : null}

      {normalizedDisabledReason ? (
        <Alert>
          <AlertDescription>{normalizedDisabledReason}</AlertDescription>
        </Alert>
      ) : null}

      {showValidationErrors && invalidActions.length > 0 ? (
        <Alert className="border-destructive/50 text-destructive">
          <AlertDescription>
            <ul className="space-y-1">
              {invalidActions.map((action) => (
                <li key={`${action.name}-error`}>
                  <span className="font-medium">{action.name}:</span> {action.validationError}
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : null}

      <Dialog open={dialogAction !== null} onOpenChange={(open) => { if (!open) closeDialog(); }}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{dialogAction?.name}</DialogTitle>
            <DialogDescription>
              {dialogAction?.description || "Provide values for the action variables before running."}
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleDialogSubmit} className="space-y-4">
            {(dialogAction?.variables ?? []).map((variable, index) => {
              const inputId = `action-variable-${variable}`;
              return (
                <div key={variable} className="space-y-2">
                  <Label htmlFor={inputId}>{variable}</Label>
                  <Input
                    id={inputId}
                    value={variableValues[variable] ?? ""}
                    onChange={(event) => handleVariableChange(variable, event.target.value)}
                    autoFocus={index === 0}
                  />
                </div>
              );
            })}

            <DialogFooter>
              <Button type="button" variant="secondary" onClick={closeDialog}>
                Cancel
              </Button>
              <Button type="submit" disabled={disableAllActions}>
                Run
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
