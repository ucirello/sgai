import type {
  AgentsResponse,
  Skill,
  SkillsResponse,
  Snippet,
  SnippetsResponse,
  ApiRespondRequest,
  ApiRespondResponse,
  ApiSessionActionResponse,
  ApiGoalResponse,
  ApiCreateWorkspaceResponse,
  ApiForkResponse,
  ApiForkTemplateResponse,
  ApiUpdateGoalResponse,
  ApiAdhocResponse,
  ApiActionRunRequest,
  ApiModelsResponse,
  ApiSteerResponse,
  ApiTogglePinResponse,
  ApiOpenEditorResponse,
  ApiDeleteWorkspaceResponse,
  ApiDeleteMessageResponse,
  ApiAttachWorkspaceResponse,
  ApiDetachWorkspaceResponse,
  ApiBrowseDirectoriesResponse,
  ApiRepositoryOperation,
} from "../types";

class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const headers: HeadersInit = { ...options?.headers };
  if (options?.body) {
    (headers as Record<string, string>)["Content-Type"] = "application/json";
  }

  const response = await fetch(url, {
    ...options,
    headers,
  });

  if (!response.ok) {
    const text = await response.text().catch(() => "Unknown error");
    throw new ApiError(response.status, text);
  }

  if (response.status === 204) {
    return null as T;
  }

  return response.json() as Promise<T>;
}

export const api = {
  workspaces: {
    create: (name: string) =>
      fetchJSON<ApiCreateWorkspaceResponse>("/api/v1/workspaces", {
        method: "POST",
        body: JSON.stringify({ name }),
      }),
    start: (name: string, auto = false) =>
      fetchJSON<ApiSessionActionResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/start`,
        {
          method: "POST",
          body: JSON.stringify({ auto }),
        },
      ),
    stop: (name: string) =>
      fetchJSON<ApiSessionActionResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/stop`,
        { method: "POST" },
      ),
    respond: (name: string, request: ApiRespondRequest) =>
      fetchJSON<ApiRespondResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/respond`,
        {
          method: "POST",
          body: JSON.stringify(request),
        },
      ),
    fork: (name: string, goalContent: string) =>
      fetchJSON<ApiForkResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/fork`,
        { method: "POST", body: JSON.stringify({ goalContent }) },
      ),
    getGoal: (name: string) =>
      fetchJSON<ApiGoalResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/goal`,
      ),
    updateGoal: (name: string, content: string) =>
      fetchJSON<ApiUpdateGoalResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/goal`,
        { method: "PUT", body: JSON.stringify({ content }) },
      ),
    adhoc: (name: string, prompt: string, model: string) =>
      fetchJSON<ApiAdhocResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/adhoc`,
        { method: "POST", body: JSON.stringify({ prompt, model }) },
      ),
    actionRun: (name: string, request: ApiActionRunRequest) =>
      fetchJSON<ApiAdhocResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/actions/run`,
        { method: "POST", body: JSON.stringify(request) },
      ),
    adhocStatus: (name: string) =>
      fetchJSON<ApiAdhocResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/adhoc`,
      ),
    adhocStop: (name: string) =>
      fetchJSON<ApiAdhocResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/adhoc`,
        { method: "DELETE" },
      ),

    steer: (name: string, message: string) =>
      fetchJSON<ApiSteerResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/steer`,
        { method: "POST", body: JSON.stringify({ message }) },
      ),
    togglePin: (name: string) =>
      fetchJSON<ApiTogglePinResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/pin`,
        { method: "POST" },
      ),
    openEditor: (name: string) =>
      fetchJSON<ApiOpenEditorResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/open-editor`,
        { method: "POST" },
      ),
    openEditorGoal: (name: string) =>
      fetchJSON<ApiOpenEditorResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/open-editor/goal`,
        { method: "POST" },
      ),
    openEditorProjectManagement: (name: string) =>
      fetchJSON<ApiOpenEditorResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/open-editor/project-management`,
        { method: "POST" },
      ),
    deleteWorkspace: (name: string, operation?: ApiRepositoryOperation, workspaceDir?: string) =>
      fetchJSON<ApiDeleteWorkspaceResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/delete`,
        {
          method: "POST",
          body: JSON.stringify({
            confirm: true,
            ...(operation ? { operation } : {}),
            ...(workspaceDir ? { workspaceDir } : {}),
          }),
        },
      ),
    deleteMessage: (name: string, messageId: number) =>
      fetchJSON<ApiDeleteMessageResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/messages/${messageId}`,
        { method: "DELETE" },
      ),
    forkTemplate: (name: string) =>
      fetchJSON<ApiForkTemplateResponse>(
        `/api/v1/workspaces/${encodeURIComponent(name)}/fork-template`,
      ),
    attach: (path: string) =>
      fetchJSON<ApiAttachWorkspaceResponse>("/api/v1/workspaces/attach", {
        method: "POST",
        body: JSON.stringify({ path }),
      }),
    detach: (path: string) =>
      fetchJSON<ApiDetachWorkspaceResponse>("/api/v1/workspaces/detach", {
        method: "POST",
        body: JSON.stringify({ path }),
      }),
    reset: (name: string) =>
      fetchJSON<void>(`/api/v1/workspaces/${encodeURIComponent(name)}/reset`, {
        method: "POST",
      }),
  },

  browse: {
    directories: (path: string) =>
      fetchJSON<ApiBrowseDirectoriesResponse>(
        `/api/v1/browse-directories?path=${encodeURIComponent(path)}`,
      ),
  },

  agents: {
    list: (workspace: string) =>
      fetchJSON<AgentsResponse>(
        `/api/v1/agents?workspace=${encodeURIComponent(workspace)}`,
      ),
  },

  skills: {
    list: (workspace: string) =>
      fetchJSON<SkillsResponse>(
        `/api/v1/skills?workspace=${encodeURIComponent(workspace)}`,
      ),
    get: (fullPath: string, workspace: string) =>
      fetchJSON<Skill>(
        `/api/v1/skills/${fullPath.split("/").map(encodeURIComponent).join("/")}?workspace=${encodeURIComponent(workspace)}`,
      ),
  },

  models: {
    list: (workspace: string) =>
      fetchJSON<ApiModelsResponse>(
        `/api/v1/models?workspace=${encodeURIComponent(workspace)}`,
      ),
  },

  snippets: {
    list: (workspace: string) =>
      fetchJSON<SnippetsResponse>(
        `/api/v1/snippets?workspace=${encodeURIComponent(workspace)}`,
      ),
    get: (lang: string, fileName: string, workspace: string) =>
      fetchJSON<Snippet>(
        `/api/v1/snippets/${encodeURIComponent(lang)}/${encodeURIComponent(fileName)}?workspace=${encodeURIComponent(workspace)}`,
      ),
  },
} as const;

export { ApiError };
