import { useState, useTransition } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Textarea } from "@/components/ui/textarea";
import { Button } from "@/components/ui/button";
import { MarkdownContent } from "@/components/MarkdownContent";
import { ChevronRight } from "lucide-react";
import { api } from "@/lib/api";
import { useFactoryState, triggerFactoryRefresh } from "@/lib/factory-state";
import { resolveWorkspaceByName } from "@/lib/workspace-identity";
import type { ApiAgentCost, ApiDollarBreakdown, ApiStepCost, ApiTodoEntry, ApiSessionCost, ApiTokenUsage } from "@/types";

interface SessionTabProps {
  workspaceName: string;
  pmContent?: string;
  hasProjectMgmt?: boolean;
}

function formatCost(cost: number): string {
  return `$${cost.toFixed(4)}`;
}

function formatStepCost(cost: number): string {
  return `$${cost.toFixed(6)}`;
}

type TokenMetricKey = keyof ApiTokenUsage | "total";

const TOKEN_METRICS: Array<{ key: TokenMetricKey; label: string; description: string }> = [
  { key: "input", label: "Input", description: "New prompt tokens processed for the session." },
  { key: "output", label: "Output", description: "Tokens generated in responses." },
  { key: "reasoning", label: "Reasoning", description: "Reasoning tokens consumed while the model worked." },
  { key: "cacheRead", label: "Cache Read", description: "Tokens served from cache instead of reprocessing." },
  { key: "cacheWrite", label: "Cache Write", description: "Tokens written into cache for reuse." },
  { key: "total", label: "Total", description: "Combined token usage and total dollar cost." },
] as const;

function totalTokens(tokens: Partial<ApiTokenUsage> | undefined): number {
  if (!tokens) {
    return 0;
  }
  return (tokens.input ?? 0)
    + (tokens.output ?? 0)
    + (tokens.reasoning ?? 0)
    + (tokens.cacheRead ?? 0)
    + (tokens.cacheWrite ?? 0);
}

function tokensForMetric(key: TokenMetricKey, tokens: Partial<ApiTokenUsage> | undefined): number {
  if (key === "total") {
    return totalTokens(tokens);
  }
  return tokens?.[key] ?? 0;
}

function dollarsForMetric(key: TokenMetricKey, dollars: Partial<ApiDollarBreakdown> | undefined, totalCost: number): number {
  if (key === "total") {
    return dollars?.total ?? totalCost;
  }
  return dollars?.[key] ?? 0;
}

function MetricBreakdown(
  {
    tokens,
    dollars,
    totalCost,
    formatDollarValue = formatCost,
  }: {
    tokens: Partial<ApiTokenUsage> | undefined;
    dollars: Partial<ApiDollarBreakdown> | undefined;
    totalCost: number;
    formatDollarValue?: (cost: number) => string;
  },
) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {TOKEN_METRICS.map((metric) => (
        <Tooltip key={metric.key}>
          <TooltipTrigger asChild>
            <div className="rounded-lg border bg-muted/20 px-3 py-2 text-sm">
              <div className="text-xs text-muted-foreground">{metric.label}</div>
              <div className="mt-1 flex items-baseline justify-between gap-3">
                <span className="font-semibold">{tokensForMetric(metric.key, tokens).toLocaleString()} tok</span>
                <span className="font-mono text-xs text-muted-foreground">
                  {formatDollarValue(dollarsForMetric(metric.key, dollars, totalCost))}
                </span>
              </div>
            </div>
          </TooltipTrigger>
          <TooltipContent>{metric.description}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}

function CostSection({ cost }: { cost: ApiSessionCost }) {
  const agentCosts = Array.isArray(cost.byAgent) ? cost.byAgent : [];

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Cost Tracking</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2">
          <div>
            <div className="text-sm font-medium">Session Breakdown</div>
            <p className="text-xs text-muted-foreground">
              Token counts and dollar totals across the full session from the API.
            </p>
          </div>
          <MetricBreakdown tokens={cost.totalTokens} dollars={cost.dollars} totalCost={cost.totalCost} />
        </div>

        {agentCosts.length > 0 && (
          <details className="mt-4">
            <summary className="cursor-pointer text-sm font-medium">
              By Agent ({agentCosts.length} agents)
            </summary>
            <div className="mt-2 space-y-2">
              {agentCosts.map((agent) => (
                <AgentCostDetail key={agent.agent} agentCost={agent} />
              ))}
            </div>
          </details>
        )}
      </CardContent>
    </Card>
  );
}

function AgentCostDetail({ agentCost }: { agentCost: ApiAgentCost }) {
  const steps = Array.isArray(agentCost.steps) ? agentCost.steps : [];

  const stepTokenSummary = (tokens: ApiTokenUsage): string => {
    const parts = [
      `${tokens.input} in`,
      `${tokens.output} out`,
      `${tokens.reasoning} reason`,
      `${tokens.cacheRead} cache-read`,
      `${tokens.cacheWrite} cache-write`,
    ];
    return `${parts.join(" | ")} | ${totalTokens(tokens)} total`;
  };

  return (
    <details className="ml-2">
      <summary className="cursor-pointer text-sm flex items-center gap-2">
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="font-medium truncate max-w-[200px]">{agentCost.agent}</span>
          </TooltipTrigger>
          <TooltipContent>{agentCost.agent}</TooltipContent>
        </Tooltip>
        <span className="text-muted-foreground">{formatCost(agentCost.cost)}</span>
        <span className="text-xs text-muted-foreground">
          {totalTokens(agentCost.tokens).toLocaleString()} tok | {steps.length} steps
        </span>
      </summary>
      <div className="ml-4 mt-2 space-y-3">
        <MetricBreakdown tokens={agentCost.tokens} dollars={agentCost.dollars} totalCost={agentCost.cost} />
        {steps.length > 0 && (
          <div className="space-y-1">
            {steps.map((step: ApiStepCost) => (
              <div key={`${step.stepId}-${step.timestamp}`} className="rounded-md border bg-muted/10 p-2">
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="truncate max-w-[150px] cursor-help">{step.stepId}</span>
                    </TooltipTrigger>
                    <TooltipContent>{step.stepId}</TooltipContent>
                  </Tooltip>
                  <span>{formatStepCost(step.cost)}</span>
                  <span>{stepTokenSummary(step.tokens)}</span>
                </div>
                <div className="mt-2">
                  <MetricBreakdown
                    tokens={step.tokens}
                    dollars={step.dollars}
                    totalCost={step.cost}
                    formatDollarValue={formatStepCost}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </details>
  );
}

function TodoStatusIcon({ status }: { status: string }) {
  switch (status) {
    case "pending":
      return <span>{"○"}</span>;
    case "in_progress":
      return <span>{"◐"}</span>;
    case "completed":
      return <span>{"●"}</span>;
    case "cancelled":
      return <span>{"✕"}</span>;
    default:
      return <span>{"○"}</span>;
  }
}

function TodoList({ todos, emptyMessage }: { todos: ApiTodoEntry[]; emptyMessage: string }) {
  if (!todos || todos.length === 0) {
    return <p className="text-sm italic text-muted-foreground">{emptyMessage}</p>;
  }

  return (
    <ul className="space-y-1.5">
      {todos.map((todo) => (
        <li key={`${todo.id}-${todo.content}-${todo.status}-${todo.priority}`} className="flex items-start gap-2 text-sm">
          <TodoStatusIcon status={todo.status} />
          <span className="flex-1">
            {todo.content}
            <span className="text-xs text-muted-foreground ml-1">({todo.priority})</span>
          </span>
        </li>
      ))}
    </ul>
  );
}

function TasksSection({ projectTodos, agentTodos }: { projectTodos: ApiTodoEntry[]; agentTodos: ApiTodoEntry[] }) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Project TODO</CardTitle>
        </CardHeader>
        <CardContent>
          <TodoList todos={projectTodos ?? []} emptyMessage="No project todos" />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Agent TODO</CardTitle>
        </CardHeader>
        <CardContent>
          <TodoList todos={agentTodos ?? []} emptyMessage="No active agent todos" />
        </CardContent>
      </Card>
    </div>
  );
}

export function SessionTab({ workspaceName, pmContent, hasProjectMgmt }: SessionTabProps) {
  const [steerMessage, setSteerMessage] = useState("");
  const [steerError, setSteerError] = useState<string | null>(null);
  const [steerSuccess, setSteerSuccess] = useState(false);
  const [isSteering, startSteerTransition] = useTransition();
  const { workspaces } = useFactoryState();
  const workspace = resolveWorkspaceByName(workspaces, workspaceName);

  const agentSequence = workspace?.agentSequence ?? [];
  const cost = workspace?.cost;
  const modelStatuses = workspace?.modelStatuses;
  const projectTodos = workspace?.projectTodos ?? [];
  const agentTodos = workspace?.agentTodos ?? [];

  const submitSteer = () => {
    if (!workspaceName || !steerMessage.trim()) return;
    setSteerError(null);
    setSteerSuccess(false);
    startSteerTransition(async () => {
      try {
        const response = await api.workspaces.steer(workspaceName, steerMessage.trim());
        triggerFactoryRefresh();
        if (response.success) {
          setSteerSuccess(true);
          setSteerMessage("");
        } else {
          setSteerError(response.message || "Failed to submit steering message");
        }
      } catch (err) {
        setSteerError(err instanceof Error ? err.message : "Failed to submit steering message");
      }
    });
  };

  const handleSteerSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    submitSteer();
  };

  const handleSteerKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      submitSteer();
    }
  };

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Steer Next Turn</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSteerSubmit} className="space-y-3">
            <div className="space-y-2">
              <Textarea
                id="steer-message"
                value={steerMessage}
                onChange={(event) => setSteerMessage(event.target.value)}
                onKeyDown={handleSteerKeyDown}
                placeholder="Enter re-steering instruction..."
                rows={4}
                className="resize-y"
                disabled={isSteering}
              />
            </div>
            {steerError && (
              <p className="text-sm text-destructive">{steerError}</p>
            )}
            {steerSuccess && !steerError && (
              <p className="text-sm text-primary">Steering instruction sent.</p>
            )}
            <Button type="submit" disabled={isSteering || !steerMessage.trim()}>
              Submit
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Tasks</CardTitle>
        </CardHeader>
        <CardContent>
          <TasksSection projectTodos={projectTodos} agentTodos={agentTodos} />
        </CardContent>
      </Card>

      {cost && <CostSection cost={cost} />}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Agent Sequence</CardTitle>
        </CardHeader>
        <CardContent>
          {agentSequence && agentSequence.length > 0 ? (
            <ScrollArea className="max-h-[300px]">
              <ol className="list-decimal list-inside space-y-1 text-sm">
                {agentSequence.map((entry) => (
                  <li key={`${entry.agent}-${entry.elapsedTime}`} className="flex items-center gap-2">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span className={entry.isCurrent ? "font-bold" : ""}>
                          {entry.agent}
                        </span>
                      </TooltipTrigger>
                      <TooltipContent>{entry.model ? `${entry.agent} | ${entry.model}` : entry.agent}</TooltipContent>
                    </Tooltip>
                    <span className="text-xs text-muted-foreground">
                      ({entry.elapsedTime})
                    </span>
                  </li>
                ))}
              </ol>
            </ScrollArea>
          ) : (
            <p className="text-sm italic text-muted-foreground">No agent sequence yet</p>
          )}
        </CardContent>
      </Card>

      {modelStatuses && modelStatuses.length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Model Consensus</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-1 text-sm">
              {modelStatuses.map((ms) => (
                <li key={`${ms.modelId}-${ms.status}`} className="flex items-center gap-2">
                  <span>
                    {ms.status === "model-working" ? "◐" : ms.status === "model-done" ? "●" : "✕"}
                  </span>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="truncate max-w-[200px] cursor-help">
                        {ms.modelId.split("/").pop() ?? ms.modelId}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>{ms.modelId}</TooltipContent>
                  </Tooltip>
                  <Badge variant="secondary" className="text-xs">{ms.status}</Badge>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      {hasProjectMgmt && (
        <details className="group">
          <summary className="cursor-pointer font-semibold text-sm mb-2 flex items-center gap-2 list-none [&::-webkit-details-marker]:hidden">
            <ChevronRight
              className="h-4 w-4 text-muted-foreground transition-transform duration-200 group-open:rotate-90"
              aria-hidden="true"
            />
            <span>PROJECT_MANAGEMENT.md</span>
          </summary>
          {pmContent ? (
            <MarkdownContent
              content={pmContent}
              className="p-4 border rounded-lg bg-muted/20"
            />
          ) : (
            <p className="text-sm italic text-muted-foreground p-4">No content available</p>
          )}
        </details>
      )}
    </div>
  );
}
