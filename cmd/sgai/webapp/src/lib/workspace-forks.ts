import type { ApiWorkspaceEntry } from "@/types";

export function canCreateForkFromWorkspace(
  workspace: ApiWorkspaceEntry | null | undefined,
): workspace is ApiWorkspaceEntry {
  return Boolean(workspace && workspace.hasSgai && !workspace.isFork);
}
