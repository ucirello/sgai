import { describe, expect, it } from "bun:test";

import { computeAgentsAndFlowFromTechStack } from "../composeWizardTechStack";

describe("useComposeWizard tech stack mapping", () => {
  it("maps Go tech stack to the renamed reviewer pair", () => {
    const { agents, flow } = computeAgentsAndFlowFromTechStack(["go"], false);

    expect(agents.map((agent) => agent.name)).toEqual([
      "coordinator",
      "go-developer",
      "go-reviewer",
    ]);
    expect(flow).toContain('"go-developer" -> "go-reviewer"');
    expect(flow).not.toContain("backend-go-developer");
    expect(flow).not.toContain("go-readability-reviewer");
  });

  it("routes Go safety-analysis flow from the renamed developer", () => {
    const { agents, flow } = computeAgentsAndFlowFromTechStack(["go"], true);

    expect(agents.map((agent) => agent.name)).toEqual([
      "coordinator",
      "go-developer",
      "go-reviewer",
      "stpa-analyst",
    ]);
    expect(flow).toContain('"go-developer" -> "go-reviewer"');
    expect(flow).toContain('"go-developer" -> "stpa-analyst"');
    expect(flow).toContain('"go-reviewer" -> "stpa-analyst"');
    expect(agents.map((agent) => agent.name)).not.toContain("backend-go-developer");
    expect(agents.map((agent) => agent.name)).not.toContain("go-readability-reviewer");
  });
});
