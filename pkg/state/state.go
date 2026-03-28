// Package state provides types and functions for managing workflow state.
// The state is persisted as JSON in .sgai/state.json within project directories.
package state

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// Workflow status constants define the possible states of a sgai workflow.
const (
	StatusWorking   = "working"
	StatusAgentDone = "agent-done"
	StatusComplete  = "complete"
)

// InteractionMode constants define the possible interaction modes of a sgai session.
const (
	ModeSelfDrive     = "self-drive"
	ModeBrainstorming = "brainstorming"
	ModeBuilding      = "building"
	ModeRetrospective = "retrospective"
	ModeContinuous    = "continuous"
)

// TodoItem represents a single item in the agent's TODO list.
// The structure matches the opencode todo.updated event payload.
type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// AgentSequenceEntry represents an agent's visit in the workflow sequence.
// It tracks when the agent started and whether it is the current agent.
type AgentSequenceEntry struct {
	Agent     string `json:"agent"`
	StartTime string `json:"startTime"`
	IsCurrent bool   `json:"isCurrent"`
}

// ProgressEntry represents a single progress log entry.
type ProgressEntry struct {
	Timestamp   string `json:"timestamp"`
	Agent       string `json:"agent"`
	Description string `json:"description"`
}

// QuestionItem represents a single question in a multi-question batch.
type QuestionItem struct {
	Question    string   `json:"question"`
	Choices     []string `json:"choices"`
	MultiSelect bool     `json:"multiSelect"`
}

// MultiChoiceQuestion stores an active batch of questions for human response.
// Used by the AskUserQuestion tool to present choices to the human partner.
type MultiChoiceQuestion struct {
	Questions  []QuestionItem `json:"questions"`
	IsWorkGate bool           `json:"isWorkGate,omitempty"`
}

// TokenUsage tracks token counts from a step.
type TokenUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	Reasoning  int `json:"reasoning"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
}

// Add accumulates token counts from another TokenUsage into this one.
func (t *TokenUsage) Add(other TokenUsage) {
	t.Input += other.Input
	t.Output += other.Output
	t.Reasoning += other.Reasoning
	t.CacheRead += other.CacheRead
	t.CacheWrite += other.CacheWrite
}

// DollarBreakdown tracks dollar amounts by token category.
type DollarBreakdown struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Add accumulates dollar amounts from another DollarBreakdown into this one.
func (d *DollarBreakdown) Add(other DollarBreakdown) {
	d.Input += other.Input
	d.Output += other.Output
	d.Reasoning += other.Reasoning
	d.CacheRead += other.CacheRead
	d.CacheWrite += other.CacheWrite
	d.Total += other.Total
}

// StepCost tracks cost for a single step.
type StepCost struct {
	StepID    string          `json:"stepId"`
	Agent     string          `json:"agent"`
	Cost      float64         `json:"cost"`
	Dollars   DollarBreakdown `json:"dollars"`
	Tokens    TokenUsage      `json:"tokens"`
	Timestamp string          `json:"timestamp"`
}

// AgentCost aggregates costs for an agent.
type AgentCost struct {
	Agent   string          `json:"agent"`
	Cost    float64         `json:"cost"`
	Dollars DollarBreakdown `json:"dollars"`
	Tokens  TokenUsage      `json:"tokens"`
	Steps   []StepCost      `json:"steps"`
}

// SessionCost tracks all costs for the session.
type SessionCost struct {
	TotalCost   float64         `json:"totalCost"`
	Dollars     DollarBreakdown `json:"dollars"`
	TotalTokens TokenUsage      `json:"totalTokens"`
	ByAgent     []AgentCost     `json:"byAgent"`
}

// Workflow represents the complete workflow state for a sgai session.
// It tracks progress, inter-agent messaging, and workflow status.
type Workflow struct {
	Status              string               `json:"status"`
	Task                string               `json:"task"`
	Progress            []ProgressEntry      `json:"progress"`
	HumanMessage        string               `json:"humanMessage"`
	MultiChoiceQuestion *MultiChoiceQuestion `json:"multiChoiceQuestion,omitempty"`
	Messages            []Message            `json:"messages"`
	VisitCounts         map[string]int       `json:"visitCounts,omitempty"`
	CurrentAgent        string               `json:"currentAgent,omitempty"`
	Todos               []TodoItem           `json:"todos,omitempty"`
	ProjectTodos        []TodoItem           `json:"projectTodos,omitempty"`
	AgentSequence       []AgentSequenceEntry `json:"agentSequence,omitempty"`
	SessionID           string               `json:"sessionId,omitempty"`

	Cost SessionCost `json:"cost"`

	InteractionMode string `json:"interactionMode,omitempty"`

	// ModelStatuses tracks per-model status in multi-model agents.
	// Key is model ID (agent:modelSpec), value is "model-working", "model-done", or "model-error".
	// This field is ephemeral and cleared when the agent transitions.
	ModelStatuses map[string]string `json:"modelStatuses,omitempty"`

	// CurrentModel tracks the currently executing model in multi-model agents.
	// Format is "agentName:modelSpec".
	CurrentModel string `json:"currentModel,omitempty"`

	// Summary is a single-sentence summary of the project goal.
	// Generated automatically when GOAL.md is saved or workspace starts,
	// unless SummaryManual is true (indicating user has manually edited it).
	Summary string `json:"summary,omitempty"`

	// SummaryManual indicates whether the summary was manually edited by user.
	// When true, automatic summary generation is skipped.
	SummaryManual bool `json:"summaryManual,omitempty"`
}

// NewWorkflow returns a workflow with the non-nil collections expected by a
// fresh session state.
func NewWorkflow() Workflow {
	var wf Workflow
	wf.Progress = []ProgressEntry{}
	wf.Messages = []Message{}
	wf.VisitCounts = map[string]int{}
	wf.Todos = []TodoItem{}
	wf.ProjectTodos = []TodoItem{}
	wf.AgentSequence = []AgentSequenceEntry{}
	wf.Cost.ByAgent = []AgentCost{}
	wf.ModelStatuses = map[string]string{}
	return wf
}

// NeedsHumanInput reports whether this workflow currently has pending
// in-memory human input.
//
//nolint:gocritic // Workflow state is intentionally queried as a value snapshot throughout the server.
func (w Workflow) NeedsHumanInput() bool {
	return w.MultiChoiceQuestion != nil || w.HumanMessage != ""
}

func (w *Workflow) detached() Workflow {
	detached := *w
	detached.Progress = slices.Clone(w.Progress)
	detached.Messages = slices.Clone(w.Messages)
	detached.VisitCounts = maps.Clone(w.VisitCounts)
	detached.Todos = slices.Clone(w.Todos)
	detached.ProjectTodos = slices.Clone(w.ProjectTodos)
	detached.AgentSequence = slices.Clone(w.AgentSequence)
	detached.Cost.ByAgent = detachedAgentCosts(w.Cost.ByAgent)
	detached.ModelStatuses = maps.Clone(w.ModelStatuses)
	detached.MultiChoiceQuestion = detachedMultiChoiceQuestion(w.MultiChoiceQuestion)
	return detached
}

func detachedMultiChoiceQuestion(question *MultiChoiceQuestion) *MultiChoiceQuestion {
	if question == nil {
		return nil
	}
	detached := *question
	detached.Questions = detachedQuestionItems(question.Questions)
	return &detached
}

func detachedQuestionItems(items []QuestionItem) []QuestionItem {
	detached := slices.Clone(items)
	for i := range items {
		detached[i].Choices = slices.Clone(items[i].Choices)
	}
	return detached
}

func detachedAgentCosts(agentCosts []AgentCost) []AgentCost {
	detached := slices.Clone(agentCosts)
	for i := range agentCosts {
		detached[i].Steps = slices.Clone(agentCosts[i].Steps)
	}
	return detached
}

// Message represents an inter-agent message in the workflow system.
type Message struct {
	ID        int    `json:"id"`
	FromAgent string `json:"fromAgent"`
	ToAgent   string `json:"toAgent"`
	Body      string `json:"body"`
	Read      bool   `json:"read"`
	ReadAt    string `json:"readAt,omitempty"`
	ReadBy    string `json:"readBy,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"` // ISO 8601 format (e.g., "2025-12-19T14:30:00Z")
}

func load(path string) (Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Workflow{}, fmt.Errorf("reading workflow state %s: %w", path, err)
	}
	var wf Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return Workflow{}, fmt.Errorf("decoding workflow state %s: %w", path, err)
	}
	if wf.VisitCounts == nil {
		wf.VisitCounts = make(map[string]int)
	}
	if wf.ModelStatuses == nil {
		wf.ModelStatuses = make(map[string]string)
	}
	wf.HumanMessage = ""
	wf.MultiChoiceQuestion = nil
	return wf, nil
}

func save(path string, wf *Workflow) error {
	snapshot := wf.detached()
	snapshot.HumanMessage = ""
	snapshot.MultiChoiceQuestion = nil
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating workflow state directory for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding workflow state %s: %w", path, err)
	}
	if errWrite := os.WriteFile(path, data, 0o644); errWrite != nil {
		return fmt.Errorf("writing workflow state %s: %w", path, errWrite)
	}
	return nil
}
