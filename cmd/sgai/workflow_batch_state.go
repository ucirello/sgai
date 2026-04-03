package main

import (
	"slices"

	"github.com/ucirello/sgai/pkg/state"
)

func prepareCurrentBatchState(wf *state.Workflow, currentAgents []string) {
	if len(currentAgents) < 2 {
		wf.AgentStates = nil
		wf.Status = state.StatusWorking
		wf.Task = ""
		return
	}

	wf.AgentStates = make(map[string]state.AgentExecutionState, len(currentAgents))
	for _, currentAgent := range currentAgents {
		wf.AgentStates[currentAgent] = state.AgentExecutionState{Status: state.StatusWorking, Task: ""}
	}
	wf.Status = parallelBatchStatus(wf)
	wf.Task = ""
}

func updateAgentWorkflowState(wf *state.Workflow, agent, status string, updateStatus bool, task string, updateTask bool) {
	if !parallelBatchIncludesAgent(wf, agent) {
		if updateStatus {
			wf.Status = status
		}
		if updateTask {
			wf.Task = task
		}
		if workflowStatusClearsTask(wf.Status) {
			wf.Task = ""
		}
		return
	}

	if wf.AgentStates == nil {
		wf.AgentStates = make(map[string]state.AgentExecutionState, len(splitCurrentAgents(wf.CurrentAgent)))
	}
	agentState := wf.AgentStates[agent]
	if updateStatus {
		agentState.Status = status
	}
	if updateTask {
		agentState.Task = task
	}
	if workflowStatusClearsTask(agentState.Status) {
		agentState.Task = ""
	}
	wf.AgentStates[agent] = agentState
	wf.Status = parallelBatchStatus(wf)
	wf.Task = ""
}

func visibleWorkflowStatus(wf *state.Workflow) string {
	if !hasParallelCurrentAgents(wf.CurrentAgent) {
		return wf.Status
	}
	return parallelBatchStatus(wf)
}

func visibleWorkflowTask(wf *state.Workflow) string {
	if hasParallelCurrentAgents(wf.CurrentAgent) {
		return ""
	}
	return wf.Task
}

func parallelBatchStatus(wf *state.Workflow) string {
	currentAgents := splitCurrentAgents(wf.CurrentAgent)
	if len(currentAgents) < 2 {
		return wf.Status
	}
	for _, currentAgent := range currentAgents {
		if wf.AgentStates[currentAgent].Status != state.StatusAgentDone {
			return state.StatusWorking
		}
	}
	return state.StatusAgentDone
}

func parallelBatchIncludesAgent(wf *state.Workflow, agent string) bool {
	currentAgents := splitCurrentAgents(wf.CurrentAgent)
	return len(currentAgents) > 1 && slices.Contains(currentAgents, agent)
}

func workflowStatusClearsTask(status string) bool {
	return status == state.StatusAgentDone || status == state.StatusComplete
}
