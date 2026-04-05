import type { ApiWorkspaceEntry } from "@/types";

type ActiveAgentSource = Pick<ApiWorkspaceEntry, "currentAgent" | "activeAgents">;

export function normalizeActiveAgents({ currentAgent, activeAgents }: ActiveAgentSource): string[] {
  const normalizedActiveAgents = Array.from(new Set((activeAgents ?? [])
    .map((agent) => agent.trim())
    .filter(Boolean)));

  if (normalizedActiveAgents.length > 0) {
    return normalizedActiveAgents;
  }

  const normalizedCurrentAgent = currentAgent.trim();
  if (!normalizedCurrentAgent || normalizedCurrentAgent === "Unknown") {
    return [];
  }

  return Array.from(new Set(normalizedCurrentAgent
    .split(",")
    .map((agent) => agent.trim())
    .filter(Boolean)));
}
