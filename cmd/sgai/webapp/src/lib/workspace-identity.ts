import { getRepositoryTitle } from "@/lib/repository-title";
import type { ApiWorkspaceEntry } from "@/types";

type WorkspaceIdentity = Pick<ApiWorkspaceEntry, "name" | "dir">;

type WorkspaceLabelSource = WorkspaceIdentity & Pick<ApiWorkspaceEntry, "title" | "computedTitle">;

interface WorkspacePathOptions {
  workspaceDir?: string;
}

const workspaceDirQueryParam = "workspaceDir";

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

export function buildWorkspaceNameDisambiguators(workspaces: WorkspaceLabelSource[]): Map<string, string> {
  const grouped = new Map<string, WorkspaceIdentity[]>();

  for (const workspace of workspaces) {
    const baseLabel = getWorkspaceBaseLabel(workspace);
    const existing = grouped.get(baseLabel) ?? [];
    existing.push(workspace);
    grouped.set(baseLabel, existing);
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

export function buildWorkspaceRouteDisambiguators(workspaces: WorkspaceIdentity[]): Set<string> {
  const grouped = new Map<string, WorkspaceIdentity[]>();

  for (const workspace of workspaces) {
    const existing = grouped.get(workspace.name) ?? [];
    existing.push(workspace);
    grouped.set(workspace.name, existing);
  }

  const disambiguators = new Set<string>();

  for (const [, group] of grouped) {
    if (group.length < 2) {
      continue;
    }

    for (const workspace of group) {
      disambiguators.add(workspace.dir);
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

export function buildWorkspacePathFromName(
  workspaceName: string,
  suffix = "",
  options: WorkspacePathOptions = {},
): string {
  const normalizedSuffix = suffix.replace(/^\/+/, "");
  const basePath = `/workspaces/${encodeURIComponent(workspaceName)}`;
  const path = normalizedSuffix ? `${basePath}/${normalizedSuffix}` : basePath;
  const workspaceDir = options.workspaceDir?.trim();

  if (!workspaceDir) {
    return path;
  }

  const searchParams = new URLSearchParams({ [workspaceDirQueryParam]: workspaceDir });
  return `${path}?${searchParams.toString()}`;
}

export function buildWorkspacePath(
  workspace: WorkspaceIdentity,
  suffix = "",
  options: WorkspacePathOptions = {},
): string {
  return buildWorkspacePathFromName(workspace.name, suffix, options);
}

export function buildWorkspaceGoalEditPath(workspace: WorkspaceIdentity): string {
  return buildWorkspacePathFromName(workspace.name, "goal/edit");
}

export function buildWorkspacePathWithDisambiguator(
  workspace: WorkspaceIdentity,
  workspaceRouteDisambiguators: Set<string>,
  suffix = "",
): string {
  return buildWorkspacePath(workspace, suffix, {
    workspaceDir: workspaceRouteDisambiguators.has(workspace.dir) ? workspace.dir : undefined,
  });
}

export function readWorkspaceDirFromSearchParams(searchParams: URLSearchParams): string {
  return searchParams.get(workspaceDirQueryParam)?.trim() ?? "";
}

export function resolveWorkspaceTarget<T extends WorkspaceIdentity>(
  workspaces: T[],
  workspaceName: string,
  workspaceDir = "",
): T | null {
  const matches = workspaces.filter((workspace) => workspace.name === workspaceName);

  if (workspaceDir) {
    return matches.find((workspace) => workspace.dir === workspaceDir) ?? null;
  }

  return matches.length === 1 ? (matches[0] ?? null) : null;
}

export function isSameWorkspace(
  left: WorkspaceIdentity | null | undefined,
  right: WorkspaceIdentity | null | undefined,
): boolean {
  return Boolean(left && right && left.name === right.name && left.dir === right.dir);
}
