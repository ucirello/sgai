type RepositoryTitleSource = {
  name: string;
  title?: string;
  computedTitle?: string;
};

function nonEmptyTrimmed(value: string | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

export function getRepositoryTitle(source: RepositoryTitleSource | null | undefined): string {
  if (!source) {
    return "";
  }

  return nonEmptyTrimmed(source.title)
    ?? nonEmptyTrimmed(source.computedTitle)
    ?? source.name;
}
