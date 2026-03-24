const FRONTMATTER_RE = /^---\r?\n[\s\S]*?\r?\n---\r?\n?/;

export function stripFrontmatter(content: string): string {
  const trimmed = content.trimStart();
  const match = trimmed.match(FRONTMATTER_RE);
  if (match) {
    return trimmed.slice(match[0].length).trim();
  }
  return trimmed;
}
