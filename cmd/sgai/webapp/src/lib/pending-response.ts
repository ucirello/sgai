import type { ApiWorkspaceEntry } from "@/types";

interface WorkspaceStatusEntry {
  name: string;
  dir: string;
  running: boolean;
  needsInput: boolean;
}

export function deduplicateByDir<T extends { dir: string }>(workspaces: T[]): T[] {
  const seen = new Set<string>();
  return workspaces.filter((workspace) => {
    if (seen.has(workspace.dir)) {
      return false;
    }

    seen.add(workspace.dir);
    return true;
  });
}

export function collectWorkspaceStatusEntries(workspaces: ApiWorkspaceEntry[]): WorkspaceStatusEntry[] {
  const allWorkspaces: WorkspaceStatusEntry[] = [];

  for (const workspace of workspaces) {
    allWorkspaces.push(workspace);
    if (workspace.forks) {
      allWorkspaces.push(...workspace.forks);
    }
  }

  return deduplicateByDir(allWorkspaces);
}

export function getFirstPendingResponseTarget(workspaces: ApiWorkspaceEntry[]): WorkspaceStatusEntry | undefined {
  return collectWorkspaceStatusEntries(workspaces).find((workspace) => workspace.needsInput);
}
