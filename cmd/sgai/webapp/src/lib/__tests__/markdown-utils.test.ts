import { describe, it, expect } from "bun:test";
import { stripFrontmatter } from "@/lib/markdown-utils";

describe("stripFrontmatter", () => {
  it("removes YAML frontmatter from content", () => {
    const content = "---\ntitle: Test\n---\n# Hello World";
    expect(stripFrontmatter(content)).toBe("# Hello World");
  });

  it("returns content unchanged when no frontmatter present", () => {
    const content = "# Hello World";
    expect(stripFrontmatter(content)).toBe("# Hello World");
  });

  it("handles empty content", () => {
    expect(stripFrontmatter("")).toBe("");
  });

  it("handles frontmatter with leading whitespace", () => {
    const content = "  ---\ntitle: Test\n---\n# Hello";
    expect(stripFrontmatter(content)).toBe("# Hello");
  });

  it("handles content that is only frontmatter", () => {
    const content = "---\ntitle: Test\n---\n";
    expect(stripFrontmatter(content)).toBe("");
  });
});
