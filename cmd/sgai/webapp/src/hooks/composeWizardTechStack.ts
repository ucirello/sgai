import type { ApiComposerAgentConf } from "@/types";

const DEFAULT_MODEL = "anthropic/claude-opus-4-6";

interface TechStackMapping {
  agents: string[];
  flow: string[];
}

const TECH_STACK_AGENT_MAP: Record<string, TechStackMapping> = {
  go: {
    agents: ["go-developer", "go-reviewer"],
    flow: ['"go-developer" -> "go-reviewer"'],
  },
  htmx: {
    agents: ["htmx-picocss-frontend-developer", "htmx-picocss-frontend-reviewer"],
    flow: ['"htmx-picocss-frontend-developer" -> "htmx-picocss-frontend-reviewer"'],
  },
  react: {
    agents: ["react-developer", "react-reviewer"],
    flow: ['"react-developer" -> "react-reviewer"'],
  },
  python: {
    agents: ["general-purpose"],
    flow: [],
  },
  typescript: {
    agents: [],
    flow: [],
  },
  shell: {
    agents: ["shell-script-coder", "shell-script-reviewer"],
    flow: ['"shell-script-coder" -> "shell-script-reviewer"'],
  },
  "general-purpose": {
    agents: ["general-purpose"],
    flow: [],
  },
  claudesdk: {
    agents: ["general-purpose", "agent-sdk-verifier-ts", "agent-sdk-verifier-py"],
    flow: [
      '"general-purpose" -> "agent-sdk-verifier-ts"',
      '"general-purpose" -> "agent-sdk-verifier-py"',
    ],
  },
  openaisdk: {
    agents: ["general-purpose", "openai-sdk-verifier-ts", "openai-sdk-verifier-py"],
    flow: [
      '"general-purpose" -> "openai-sdk-verifier-ts"',
      '"general-purpose" -> "openai-sdk-verifier-py"',
    ],
  },
};

export function computeAgentsAndFlowFromTechStack(
  techStack: string[],
  safetyAnalysis: boolean,
): { agents: ApiComposerAgentConf[]; flow: string } {
  const agentSet = new Set<string>(["coordinator"]);
  const flowLines: string[] = [];

  for (const tech of techStack) {
    const mapping = TECH_STACK_AGENT_MAP[tech];
    if (!mapping) continue;
    for (const agent of mapping.agents) {
      agentSet.add(agent);
    }
    for (const line of mapping.flow) {
      flowLines.push(line);
    }
  }

  if (safetyAnalysis) {
    agentSet.add("stpa-analyst");
    for (const tech of techStack) {
      const mapping = TECH_STACK_AGENT_MAP[tech];
      if (!mapping) continue;
      const reviewers = mapping.agents.filter(
        (agent) => agent.includes("reviewer") || agent.includes("verifier"),
      );
      for (const reviewer of reviewers) {
        flowLines.push(`"${reviewer}" -> "stpa-analyst"`);
      }
      if (tech === "go") {
        flowLines.push('"go-developer" -> "stpa-analyst"');
      }
      if (tech === "general-purpose") {
        flowLines.push('"general-purpose" -> "stpa-analyst"');
      }
    }
  }

  const agents: ApiComposerAgentConf[] = Array.from(agentSet)
    .sort()
    .map((name) => ({ name, selected: true, model: DEFAULT_MODEL }));

  return { agents, flow: [...new Set(flowLines)].join("\n") };
}
