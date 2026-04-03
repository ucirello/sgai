import { useEffect, useRef } from "react";
import { useFactoryState } from "../lib/factory-state";
import type { ApiWorkspaceEntry } from "../lib/factory-state";
import { buildWorkspaceNameDisambiguators, getWorkspaceDisplayLabel } from "../lib/workspace-identity";

type NotificationWorkspaceEntry = Pick<ApiWorkspaceEntry, "name" | "dir" | "needsInput" | "title" | "computedTitle">;

type NotificationWorkspaceSource = NotificationWorkspaceEntry & Pick<ApiWorkspaceEntry, "forks">;

function mergeNotificationWorkspace(
  current: NotificationWorkspaceEntry | undefined,
  next: NotificationWorkspaceEntry,
): NotificationWorkspaceEntry {
  if (!current) {
    return next;
  }

  return {
    ...current,
    ...next,
    needsInput: next.needsInput,
    title: next.title || current.title,
    computedTitle: next.computedTitle || current.computedTitle,
  };
}

function collectNotificationWorkspaces(
  workspaces: NotificationWorkspaceSource[],
): NotificationWorkspaceEntry[] {
  const byDir = new Map<string, NotificationWorkspaceEntry>();

  for (const workspace of workspaces) {
    byDir.set(workspace.dir, mergeNotificationWorkspace(byDir.get(workspace.dir), workspace));

    for (const fork of workspace.forks ?? []) {
      byDir.set(fork.dir, mergeNotificationWorkspace(byDir.get(fork.dir), fork));
    }
  }

  return [...byDir.values()];
}

function fireNotification(workspaceLabel: string, workspaceTag: string): void {
  if (!("Notification" in window)) {
    return;
  }

  if (Notification.permission !== "granted") {
    return;
  }

  const notification = new Notification("Approval Needed", {
    body: `Workspace ${workspaceLabel} needs your input`,
    tag: workspaceTag,
  });

  notification.onclick = () => {
    window.focus();
  };
}

export function useNotifications(): void {
  const { workspaces, lastFetchedAt } = useFactoryState();
  const previousStateRef = useRef<Map<string, boolean>>(new Map());

  useEffect(() => {
    if (lastFetchedAt === null) {
      return;
    }

    const notificationWorkspaces = collectNotificationWorkspaces(workspaces);
    const workspaceNameDisambiguators = buildWorkspaceNameDisambiguators(notificationWorkspaces);
    const currentState = new Map<string, boolean>();

    for (const workspace of notificationWorkspaces) {
      currentState.set(workspace.dir, workspace.needsInput);
    }

    const previous = previousStateRef.current;

    for (const workspace of notificationWorkspaces) {
      const wasNeedingInput = previous.get(workspace.dir) ?? false;

      if (!wasNeedingInput && workspace.needsInput) {
        fireNotification(
          getWorkspaceDisplayLabel(workspace, workspaceNameDisambiguators),
          workspace.dir,
        );
      }
    }

    previousStateRef.current = currentState;
  }, [workspaces, lastFetchedAt]);
}
