export type ApiRepositoryMode = "standalone" | "root" | "fork";

export type ApiRepositoryActionEntryPoint = "choose" | "confirm" | "hidden";

export type ApiRepositoryOperation = "detach" | "delete";

export type ApiRepositoryActionIcon = "choose" | ApiRepositoryOperation;

export type ApiRepositoryActionTone = "neutral" | "destructive";

export interface ApiRepositoryActionOperationPresentation {
  operation: ApiRepositoryOperation;
  label: string;
  icon: ApiRepositoryActionIcon;
  tone: ApiRepositoryActionTone;
}

export interface ApiRepositoryActionPresentation {
  detailTriggerLabel: string;
  treeTriggerLabel: string;
  forkRowTriggerLabel: string;
  dialogTitle: string;
  dialogDescription: string;
  icon: ApiRepositoryActionIcon;
  tone: ApiRepositoryActionTone;
  operations: ApiRepositoryActionOperationPresentation[];
}

export interface ApiRepositoryAction {
  repositoryMode: ApiRepositoryMode;
  entryPoint: ApiRepositoryActionEntryPoint;
  allowedOperations: ApiRepositoryOperation[];
  defaultOperation?: ApiRepositoryOperation;
  disabledReason?: string;
  attachedForkCount: number;
  running: boolean;
  presentation: ApiRepositoryActionPresentation;
}

export interface ApiWorkspaceEntry {
  name: string;
  dir: string;
  running: boolean;
  needsInput: boolean;
  inProgress: boolean;
  pinned: boolean;
  isRoot: boolean;
  isFork: boolean;
  title: string;
  computedTitle?: string;
  status: string;
  badgeClass: string;
  badgeText: string;
  hasSgai: boolean;
  hasEditedGoal: boolean;
  interactiveAuto: boolean;
  continuousMode: boolean;
  currentAgent: string;
  currentModel: string;
  task: string;
  goalContent: string;
  rawGoalContent: string;
  fullGoalContent?: string;
  pmContent: string;
  hasProjectMgmt: boolean;
  svgHash: string;
  totalExecTime: string;
  latestProgress: string;
  humanMessage: string;
  agentSequence: ApiAgentSequenceEntry[];
  cost: ApiSessionCost;
  modelStatuses?: ApiModelStatusEntry[];
  agentModels?: ApiAgentModelEntry[];
  events: ApiEventEntry[];
  messages: ApiMessageEntry[];
  projectTodos: ApiTodoEntry[];
  agentTodos: ApiTodoEntry[];
  forks?: ApiForkEntry[];
  log: ApiLogEntry[];
  pendingQuestion?: ApiPendingQuestionResponse;
  actions?: ApiActionEntry[];
  actionConfigError?: string;
  external?: boolean;
  repositoryAction?: ApiRepositoryAction;
}

export interface ApiAgentSequenceEntry {
  agent: string;
  model: string;
  elapsedTime: string;
  isCurrent: boolean;
}

export interface ApiActionEntry {
  name: string;
  model: string;
  prompt: string;
  script?: string;
  description?: string;
  kind?: "prompt" | "script" | "";
  variables?: string[];
  validationError?: string;
}

export interface ApiActionRunRequest {
  name: string;
  variables?: Record<string, string>;
}

export interface ApiGoalResponse {
  content: string;
}

export interface ApiCreateWorkspaceResponse {
  name: string;
  dir: string;
}

export interface Agent {
  name: string;
  description: string;
}

export interface AgentsResponse {
  agents: Agent[];
}

export interface SkillSummary {
  name: string;
  fullPath: string;
  description: string;
}

export interface SkillCategory {
  name: string;
  skills: SkillSummary[];
}

export interface SkillsResponse {
  categories: SkillCategory[];
}

export interface Skill {
  name: string;
  fullPath: string;
  description: string;
  content: string;
  rawContent: string;
}

export interface SnippetSummary {
  name: string;
  fileName: string;
  fullPath: string;
  description: string;
  language: string;
}

export interface SnippetLanguage {
  name: string;
  snippets: SnippetSummary[];
}

export interface SnippetsResponse {
  languages: SnippetLanguage[];
}

export interface Snippet {
  name: string;
  fileName: string;
  language: string;
  description: string;
  whenToUse: string;
  content: string;
}

export interface MultiChoiceQuestion {
  question: string;
  choices: string[];
  multiSelect: boolean;
}

export interface ApiPendingQuestionResponse {
  promptToken: string;
  type: "multi-choice" | "work-gate" | "free-text" | "";
  agentName: string;
  message: string;
  questions?: MultiChoiceQuestion[];
}

export interface ApiRespondRequest {
  promptToken: string;
  answer: string;
  selectedChoices: string[];
}

export interface ApiRespondResponse {
  success: boolean;
  message: string;
}

export interface ApiSessionActionResponse {
  name: string;
  status: string;
  running: boolean;
  message: string;
}

export interface ApiModelStatusEntry {
  modelId: string;
  status: string;
}

export interface ApiModelEntry {
  id: string;
  name: string;
}

export interface ApiModelsResponse {
  models: ApiModelEntry[];
  defaultModel?: string;
}

export interface ApiSessionCost {
  totalCost: number;
  dollars: ApiDollarBreakdown;
  totalTokens: ApiTokenUsage;
  byAgent: ApiAgentCost[];
}

export interface ApiDollarBreakdown {
  input: number;
  output: number;
  reasoning: number;
  cacheRead: number;
  cacheWrite: number;
  total: number;
}

export interface ApiTokenUsage {
  input: number;
  output: number;
  reasoning: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface ApiStepCost {
  stepId: string;
  agent: string;
  cost: number;
  dollars: ApiDollarBreakdown;
  tokens: ApiTokenUsage;
  timestamp: string;
}

export interface ApiAgentCost {
  agent: string;
  cost: number;
  dollars: ApiDollarBreakdown;
  tokens: ApiTokenUsage;
  steps: ApiStepCost[];
}

export interface ApiMessageEntry {
  id: number;
  fromAgent: string;
  toAgent: string;
  body: string;
  subject: string;
  read: boolean;
  readAt?: string;
  createdAt?: string;
}

export interface ApiTodoEntry {
  id: string;
  content: string;
  status: string;
  priority: string;
}

export interface ApiLogEntry {
  prefix: string;
  text: string;
}

export interface ApiEventEntry {
  timestamp: string;
  formattedTime: string;
  agent: string;
  description: string;
  showDateDivider: boolean;
  dateDivider: string;
}

export interface ApiAgentModelEntry {
  agent: string;
  models: string[];
}

export interface ApiForkEntry {
  name: string;
  dir: string;
  running: boolean;
  needsInput: boolean;
  inProgress: boolean;
  pinned: boolean;
  title: string;
  computedTitle?: string;
}

export interface ApiComposerAgentConf {
  name: string;
  selected: boolean;
  model: string;
}

export interface ApiComposerState {
  description: string;
  completionGate: string;
  agents: ApiComposerAgentConf[];
  flow: string;
  tasks: string;
}

export interface ApiWizardState {
  currentStep: number;
  fromTemplate?: string;
  description?: string;
  techStack: string[];
  safetyAnalysis: boolean;
  completionGate?: string;
}

export interface ApiTechStackItem {
  id: string;
  name: string;
  selected: boolean;
}

export interface ApiComposeStateResponse {
  workspace: string;
  state: ApiComposerState;
  wizard: ApiWizardState;
  techStackItems: ApiTechStackItem[];
  flowError?: string;
}

export interface ApiComposeTemplateEntry {
  id: string;
  name: string;
  description: string;
  icon: string;
  agents: ApiComposerAgentConf[];
  flow: string;
}

export interface ApiComposeTemplatesResponse {
  templates: ApiComposeTemplateEntry[];
}

export interface ApiComposePreviewResponse {
  content: string;
  flowError?: string;
  etag: string;
}

export interface ApiComposeDraftRequest {
  state: ApiComposerState;
  wizard: ApiWizardState;
}

export interface ApiComposeDraftResponse {
  saved: boolean;
}

export interface ApiComposeSaveResponse {
  saved: boolean;
  workspace: string;
}

export interface ApiForkResponse {
  name: string;
  dir: string;
  parent: string;
}

export interface ApiDeleteWorkspaceResponse {
  deleted: boolean;
  detached?: boolean;
  message: string;
}

export interface ApiUpdateGoalResponse {
  updated: boolean;
  workspace: string;
}

export interface ApiSteerResponse {
  success: boolean;
  message: string;
}

export interface ApiTogglePinResponse {
  pinned: boolean;
  message: string;
}

export interface ApiOpenEditorResponse {
  opened: boolean;
  editor: string;
  message: string;
}

export interface ApiDeleteMessageResponse {
  deleted: boolean;
  id: number;
  message: string;
}

export interface ApiAdhocResponse {
  running: boolean;
  output: string;
  message: string;
}

export interface ApiForkTemplateResponse {
  content: string;
}

export interface ApiAttachWorkspaceResponse {
  name: string;
  dir: string;
  hasGoal: boolean;
}

export interface ApiDetachWorkspaceResponse {
  detached: boolean;
  message: string;
}

export interface ApiBrowseDirectoryEntry {
  name: string;
  path: string;
}

export interface ApiBrowseDirectoriesResponse {
  entries: ApiBrowseDirectoryEntry[];
}
