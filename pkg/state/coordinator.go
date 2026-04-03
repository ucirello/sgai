package state

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"sync"
	"time"
)

// AgentDoneWatchdogTimeout bounds how long a finished agent process may linger.
const AgentDoneWatchdogTimeout = time.Minute

type pendingHumanInput struct {
	question     *MultiChoiceQuestion
	humanMessage string
	agent        string
	promptToken  string
	responseCh   chan string
}

// Coordinator manages workflow state in memory with blocking ask/answer delivery.
type Coordinator struct {
	mu             sync.Mutex
	wf             Workflow
	currentPrompt  *pendingHumanInput
	pendingPrompts []*pendingHumanInput
	promptSeq      uint64
	savePath       string
	saveWorkflow   func(string, *Workflow) error

	onUpdate func()
}

// NewCoordinator creates a Coordinator for the given state.json path.
// It loads the initial workflow state from disk exactly once; all subsequent
// state access is in-memory via the returned Coordinator.
func NewCoordinator(path string) (*Coordinator, error) {
	wf, err := load(path)
	if err != nil {
		return nil, fmt.Errorf("loading coordinator state: %w", err)
	}
	return newCoordinator(path, &wf), nil
}

// NewCoordinatorEmpty creates a Coordinator with an empty workflow state for the given path.
// Use this when state.json does not yet exist and the workflow is starting fresh.
func NewCoordinatorEmpty(path string) *Coordinator {
	wf := NewWorkflow()
	wf.Status = StatusWorking
	return newCoordinator(path, &wf)
}

// NewCoordinatorWith creates a Coordinator seeded with the given workflow state and
// persists it to disk immediately. Use this in tests and setup code that needs to
// establish a known on-disk state before a Coordinator is loaded by the server.
//
//nolint:gocritic // Tests seed coordinators with value snapshots to avoid later aliasing.
func NewCoordinatorWith(path string, wf Workflow) (*Coordinator, error) {
	c := newCoordinator(path, &wf)
	snapshot := wf.detached()
	if err := c.saveWorkflow(path, &snapshot); err != nil {
		return nil, fmt.Errorf("saving initial coordinator state: %w", err)
	}
	return c, nil
}

func newCoordinator(path string, wf *Workflow) *Coordinator {
	coord := new(Coordinator)
	coord.wf = wf.detached()
	coord.savePath = path
	coord.saveWorkflow = save
	return coord
}

// State returns a snapshot of the current workflow under the coordinator lock.
func (c *Coordinator) State() Workflow {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wf.detached()
}

// OnUpdate registers a callback that fires after every successful UpdateState.
// The callback is called outside the coordinator lock and is safe to use for
// invalidating UI caches or publishing SSE events.
func (c *Coordinator) OnUpdate(fn func()) {
	c.mu.Lock()
	c.onUpdate = fn
	c.mu.Unlock()
}

// UpdateState applies fn to the workflow under the coordinator lock and saves
// the result to disk for retrospective persistence.
func (c *Coordinator) UpdateState(fn func(*Workflow)) error {
	c.mu.Lock()
	fn(&c.wf)
	snapshot := c.wf.detached()
	notify := c.onUpdate
	if err := c.saveWorkflow(c.savePath, &snapshot); err != nil {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	if notify != nil {
		notify()
	}
	return nil
}

// ReplaceState replaces the coordinator workflow with a detached snapshot of wf
// and persists it to disk.
func (c *Coordinator) ReplaceState(wf *Workflow) error {
	if wf == nil {
		return errors.New("nil workflow")
	}

	snapshot := wf.detached()

	c.mu.Lock()
	notify := c.onUpdate
	if errSave := c.saveWorkflow(c.savePath, &snapshot); errSave != nil {
		c.mu.Unlock()
		return errSave
	}
	c.wf = snapshot
	c.mu.Unlock()

	if notify != nil {
		notify()
	}
	return nil
}

// AskAndWait stores the pending question in memory and blocks until Respond
// delivers an answer or ctx ends.
func (c *Coordinator) AskAndWait(ctx context.Context, question *MultiChoiceQuestion, humanMessage, askingAgent string) (string, error) {
	prompt, isCurrent := c.enqueuePendingPrompt(question, humanMessage, askingAgent)
	if isCurrent {
		if err := c.persistPendingPrompt(prompt); err != nil {
			c.advancePendingPrompt(prompt)
			return "", fmt.Errorf("saving question state: %w", err)
		}
	}

	log.Println("askandwait: blocking for human answer")
	select {
	case answer := <-prompt.responseCh:
		log.Println("askandwait: answer received from human")
		c.advancePendingPrompt(prompt)
		return answer, nil
	case <-ctx.Done():
		log.Println("askandwait: context cancelled:", ctx.Err())
		c.advancePendingPrompt(prompt)
		return "", fmt.Errorf("waiting for human response: %w", ctx.Err())
	}
}

// Respond delivers the human's answer to the blocked AskAndWait call.
func (c *Coordinator) Respond(answer string) bool {
	return c.respondIfCurrent("", answer)
}

// CurrentPromptToken returns the in-memory token for the current pending prompt.
func (c *Coordinator) CurrentPromptToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.currentPrompt == nil {
		return ""
	}
	return c.currentPrompt.promptToken
}

// CurrentHumanInput returns the current pending prompt token and visible
// human-input payload from one coordinator snapshot.
func (c *Coordinator) CurrentHumanInput() (promptToken, humanMessage, askingAgent string, question *MultiChoiceQuestion) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.currentPrompt != nil {
		return c.currentPrompt.promptToken, c.currentPrompt.humanMessage, c.currentPrompt.agent, detachedMultiChoiceQuestion(c.currentPrompt.question)
	}
	if c.promptSeq == 0 && c.wf.NeedsHumanInput() {
		return "", c.wf.HumanMessage, c.wf.HumanInputAgent, detachedMultiChoiceQuestion(c.wf.MultiChoiceQuestion)
	}
	return "", "", "", nil
}

// RespondIfCurrent delivers the answer only when promptToken matches the
// current in-memory prompt. An empty promptToken responds to whichever prompt
// is currently pending.
func (c *Coordinator) RespondIfCurrent(promptToken, answer string) bool {
	return c.respondIfCurrent(promptToken, answer)
}

func (c *Coordinator) respondIfCurrent(promptToken, answer string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.respondCurrentPromptLocked(c.currentPrompt, promptToken, answer)
}

func (c *Coordinator) respondCurrentPromptLocked(prompt *pendingHumanInput, promptToken, answer string) bool {
	if prompt == nil {
		log.Println("askandwait: no pending question, discarding response")
		return false
	}

	if c.currentPrompt != prompt {
		log.Println("askandwait: prompt no longer current, discarding response")
		return false
	}

	if promptToken != "" && promptToken != prompt.promptToken {
		log.Println("askandwait: stale prompt token, discarding response")
		return false
	}

	select {
	case prompt.responseCh <- answer:
		log.Println("askandwait: response queued for delivery")
		return true
	default:
		log.Println("askandwait: response channel full, response discarded")
		return false
	}
}

func (c *Coordinator) enqueuePendingPrompt(question *MultiChoiceQuestion, humanMessage, askingAgent string) (*pendingHumanInput, bool) {
	prompt := &pendingHumanInput{
		question:     question,
		humanMessage: humanMessage,
		agent:        askingAgent,
		promptToken:  "",
		responseCh:   make(chan string, 1),
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.promptSeq++
	prompt.promptToken = strconv.FormatUint(c.promptSeq, 10)
	if c.currentPrompt == nil {
		c.currentPrompt = prompt
		return prompt, true
	}
	c.pendingPrompts = append(c.pendingPrompts, prompt)
	return prompt, false
}

func (c *Coordinator) advancePendingPrompt(prompt *pendingHumanInput) {
	nextPrompt, updateVisiblePrompt := c.removePendingPrompt(prompt)
	if !updateVisiblePrompt {
		return
	}
	if err := c.persistPendingPrompt(nextPrompt); err != nil {
		log.Println("failed to update pending human input:", err)
	}
}

func (c *Coordinator) removePendingPrompt(prompt *pendingHumanInput) (*pendingHumanInput, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentPrompt == prompt {
		if len(c.pendingPrompts) == 0 {
			c.currentPrompt = nil
			return nil, true
		}
		nextPrompt := c.pendingPrompts[0]
		c.pendingPrompts = c.pendingPrompts[1:]
		c.currentPrompt = nextPrompt
		return nextPrompt, true
	}

	idx := slices.Index(c.pendingPrompts, prompt)
	if idx == -1 {
		return nil, false
	}
	c.pendingPrompts = slices.Delete(c.pendingPrompts, idx, idx+1)
	return nil, false
}

func (c *Coordinator) persistPendingPrompt(prompt *pendingHumanInput) error {
	return c.UpdateState(func(wf *Workflow) {
		if prompt == nil {
			wf.MultiChoiceQuestion = nil
			wf.HumanMessage = ""
			wf.HumanInputAgent = ""
			return
		}
		wf.MultiChoiceQuestion = prompt.question
		wf.HumanMessage = prompt.humanMessage
		wf.HumanInputAgent = prompt.agent
	})
}
