import type { Plugin } from "@opencode-ai/plugin"
import { readFile, writeFile } from 'fs/promises';
import { join } from 'path';

export const Workbench: Plugin = async ({ directory }) => {
  const stateFilePath = join(directory, ".sgai", "state.json");

  return {
    config: async (config: any) => {
      config.snapshot = false;
      config.share = "disabled";
      config.autoupdate = false;
      if (!config.instructions) {
        config.instructions = [];
      }
      config.instructions?.unshift(directory + "/.sgai/AGENTS.md");
      config.model = "opencode/big-pickle";

      // Configure MCP server for sgai custom tools via local stdio bridge
      if (!config.mcp) {
        config.mcp = {};
      }
      config.mcp.sgai = {
        type: "local",
        command: [
          process.env.SGAI_BIN_PATH || "",
          "internal-mcp",
          process.env.SGAI_MCP_URL || "",
          process.env.SGAI_AGENT_IDENTITY || ""
        ]
      };
    },
    // Tools are now provided by the MCP server configured above
    tool: {},
    event: async (input: { event: any; client: any }) => {
      if (input.event.type === "todo.updated") {
        const currentAgent = process.env.OPENCODE_AGENT_NAME || "unknown";
        if (currentAgent === "coordinator") {
          return;
        }

        try {
          let currentState: any;
          try {
            const content = await readFile(stateFilePath, 'utf8');
            currentState = JSON.parse(content);
          } catch (error) {
            currentState = {};
          }

          currentState.todos = input.event.properties.todos || [];

          await writeFile(stateFilePath, JSON.stringify(currentState, null, 2));
        } catch (error: any) {
          console.error("\033[1m|\033[0m  \033[0;31mWorkbench\033[0m   Error saving todos: " + error.message + "\033[0m");
        }
      }

      if (input.event.type === "session.compacted") {
        try {
          await input.client.session.prompt({
            path: { id: input.event.properties.sessionID },
            body: {
              parts: [{
                type: "text",
                text: `🔄 **Conversation Compacted**\n\n` +
                      `To maintain context within limits, earlier messages have been summarized.\n\n` +
                      `You MUST re-read @GOAL.md and @.sgai/PROJECT_MANAGEMENT.md before continuing.\n\n` +
                      `You MUST call \`check_inbox()\` to read these messages.`,
                metadata: {
                  source: "compaction-detector",
                  timestamp: Date.now()
                }
              }]
            }
          });
        } catch (error: any) {
          console.error("\033[1m|\033[0m  \033[0;31mWorkbench\033[0m   Error handling compaction: " + error.message + "\033[0m");
        }
      }
    }
  }
}
