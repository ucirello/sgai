interface RepositoryLabelSource {
  name: string;
  title?: string | null;
}

function normalizeRepositoryLabelPart(value: string | null | undefined): string {
  return value?.trim() ?? "";
}

export function resolveRepositoryLabel(source: RepositoryLabelSource): string {
  const title = normalizeRepositoryLabelPart(source.title);
  return title || normalizeRepositoryLabelPart(source.name);
}

export function resolveRepositoryLabelFromCandidates(
  name: string,
  ...titleCandidates: Array<string | null | undefined>
): string {
  for (const titleCandidate of titleCandidates) {
    const title = normalizeRepositoryLabelPart(titleCandidate);
    if (title) {
      return title;
    }
  }

  return normalizeRepositoryLabelPart(name);
}
