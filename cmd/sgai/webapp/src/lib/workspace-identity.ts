import { getRepositoryTitle } from "@/lib/repository-title";
import type { ApiWorkspaceEntry } from "@/types";

type WorkspaceIdentity = Pick<ApiWorkspaceEntry, "name" | "dir">;

type WorkspaceLabelSource = WorkspaceIdentity & Pick<ApiWorkspaceEntry, "title" | "computedTitle">;

export function getWorkspaceBaseLabel(workspace: WorkspaceLabelSource): string {
  const computedTitle = workspace.computedTitle?.trim();

  if (computedTitle) {
    return computedTitle;
  }

  return getRepositoryTitle(workspace);
}

function splitPathSegments(path: string): string[] {
  return path
    .split(/[\\/]+/)
    .filter(Boolean);
}

function buildGroupDisambiguators(workspaces: WorkspaceIdentity[]): Map<string, string> {
  const disambiguators = new Map<string, string>();
  const parentSegments = workspaces.map((workspace) => ({
    dir: workspace.dir,
    segments: splitPathSegments(workspace.dir).slice(0, -1),
  }));
  const maxDepth = parentSegments.reduce((largest, current) => Math.max(largest, current.segments.length), 0);

  for (const current of parentSegments) {
    let resolved = current.dir;

    for (let depth = 1; depth <= Math.max(maxDepth, 1); depth += 1) {
      const candidate = current.segments.slice(-depth).join("/");
      const normalizedCandidate = candidate || current.dir;
      const isUnique = parentSegments.every((other) => {
        if (other.dir === current.dir) {
          return true;
        }

        const otherCandidate = other.segments.slice(-depth).join("/") || other.dir;
        return otherCandidate !== normalizedCandidate;
      });

      if (isUnique) {
        resolved = normalizedCandidate;
        break;
      }
    }

    disambiguators.set(current.dir, resolved);
  }

  return disambiguators;
}

export function buildWorkspaceNameDisambiguators(workspaces: WorkspaceIdentity[]): Map<string, string> {
  const grouped = new Map<string, WorkspaceIdentity[]>();

  for (const workspace of workspaces) {
    const existing = grouped.get(workspace.name) ?? [];
    existing.push(workspace);
    grouped.set(workspace.name, existing);
  }

  const disambiguators = new Map<string, string>();

  for (const [, group] of grouped) {
    if (group.length < 2) {
      continue;
    }

    const groupDisambiguators = buildGroupDisambiguators(group);
    for (const [dir, label] of groupDisambiguators) {
      disambiguators.set(dir, label);
    }
  }

  return disambiguators;
}

export function getWorkspaceDisplayLabel(
  workspace: WorkspaceLabelSource,
  workspaceNameDisambiguators: Map<string, string>,
): string {
  const baseLabel = getWorkspaceBaseLabel(workspace);
  const disambiguator = workspaceNameDisambiguators.get(workspace.dir);

  if (!disambiguator) {
    return baseLabel;
  }

  return `${baseLabel} · ${disambiguator}`;
}

export function buildWorkspacePath(workspace: WorkspaceIdentity, suffix = ""): string {
  const normalizedSuffix = suffix.replace(/^\/+/, "");
  const basePath = `/workspaces/${encodeURIComponent(workspace.name)}`;
  return normalizedSuffix ? `${basePath}/${normalizedSuffix}` : basePath;
}

export function buildWorkspaceGoalEditPath(workspace: WorkspaceIdentity): string {
  return `/workspaces/${encodeURIComponent(workspace.name)}/goal/edit`;
}

export function resolveWorkspaceByName<T extends WorkspaceIdentity>(
  workspaces: T[],
  workspaceName: string,
): T | null {
  const matches = workspaces.filter((workspace) => workspace.name === workspaceName);

  return matches.length === 1 ? (matches[0] ?? null) : null;
}

export function isSameWorkspace(
  left: WorkspaceIdentity | null | undefined,
  right: WorkspaceIdentity | null | undefined,
): boolean {
  return Boolean(left && right && left.name === right.name && left.dir === right.dir);
}
