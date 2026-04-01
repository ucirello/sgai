import { afterEach, describe, expect, it } from "bun:test";

import { Workbench } from "./workbench";

const sessionEnvKeys = ["SGAI_BIN_PATH", "SGAI_MCP_URL", "SGAI_AGENT_IDENTITY"] as const;

type SessionEnvKey = (typeof sessionEnvKeys)[number];
type SessionEnv = Record<SessionEnvKey, string>;

const originalSessionEnv: Partial<SessionEnv> = Object.fromEntries(
  sessionEnvKeys.map((key) => [key, process.env[key]]),
);

const fullSessionEnv: SessionEnv = {
  SGAI_BIN_PATH: "/tmp/sgai",
  SGAI_MCP_URL: "http://127.0.0.1:9999/mcp",
  SGAI_AGENT_IDENTITY: "test-agent",
};

function applySessionEnv(env: Partial<SessionEnv>) {
  for (const key of sessionEnvKeys) {
    const value = env[key];
    if (typeof value === "string") {
      process.env[key] = value;
      continue;
    }
    delete process.env[key];
  }
}

async function configureWorkbench(env: Partial<SessionEnv>, config: Record<string, unknown>) {
  applySessionEnv(env);
  const plugin = await Workbench({ directory: "/tmp/test-workspace" } as never);
  const nextConfig = structuredClone(config);
  await plugin.config?.(nextConfig);
  return nextConfig;
}

afterEach(() => {
  applySessionEnv(originalSessionEnv);
});

describe("Workbench config", () => {
  for (const missingKey of sessionEnvKeys) {
    it(`keeps sgai MCP disabled when ${missingKey} is missing`, async () => {
      const env: Partial<SessionEnv> = { ...fullSessionEnv };
      delete env[missingKey];

      const config = await configureWorkbench(env, {
        mcp: {
          existing: {
            type: "local",
            command: ["existing"],
          },
        },
      });

      expect(config).toMatchObject({
        mcp: {
          existing: {
            type: "local",
            command: ["existing"],
          },
        },
      });
      expect((config.mcp as Record<string, unknown>).sgai).toBeUndefined();
    });
  }

  it("registers sgai MCP when the session-scoped env is complete", async () => {
    const config = await configureWorkbench(fullSessionEnv, {
      mcp: {
        existing: {
          type: "local",
          command: ["existing"],
        },
      },
    });

    expect(config).toMatchObject({
      mcp: {
        existing: {
          type: "local",
          command: ["existing"],
        },
        sgai: {
          type: "local",
          command: ["/tmp/sgai", "internal-mcp", "http://127.0.0.1:9999/mcp", "test-agent"],
        },
      },
    });
  });
});
