package main

import "github.com/ucirello/sgai/pkg/state"

func testTokenUsage() state.TokenUsage {
	return state.TokenUsage{
		Input:      0,
		Output:     0,
		Reasoning:  0,
		CacheRead:  0,
		CacheWrite: 0,
	}
}

func testDollarBreakdown() state.DollarBreakdown {
	return state.DollarBreakdown{
		Input:      0,
		Output:     0,
		Reasoning:  0,
		CacheRead:  0,
		CacheWrite: 0,
		Total:      0,
	}
}

func testSessionCost() state.SessionCost {
	return state.SessionCost{
		TotalCost:   0,
		Dollars:     testDollarBreakdown(),
		TotalTokens: testTokenUsage(),
		ByAgent:     nil,
	}
}

func testWorkflow() state.Workflow {
	return state.Workflow{
		Status:              "",
		Task:                "",
		Progress:            nil,
		HumanMessage:        "",
		HumanInputAgent:     "",
		MultiChoiceQuestion: nil,
		Messages:            nil,
		VisitCounts:         nil,
		CurrentAgent:        "",
		AgentStates:         nil,
		Todos:               nil,
		TodosByAgent:        nil,
		ProjectTodos:        nil,
		AgentSequence:       nil,
		SessionID:           "",
		Cost:                testSessionCost(),
		InteractionMode:     "",
		ModelStatuses:       nil,
		CurrentModel:        "",
		Summary:             "",
		SummaryManual:       false,
	}
}

func testMessage() state.Message {
	return state.Message{
		ID:        0,
		FromAgent: "",
		ToAgent:   "",
		Body:      "",
		Read:      false,
		ReadAt:    "",
		ReadBy:    "",
		CreatedAt: "",
	}
}

func testProgressEntry() state.ProgressEntry {
	return state.ProgressEntry{
		Timestamp:   "",
		Agent:       "",
		Description: "",
	}
}

func testDag() *dag {
	return &dag{
		Nodes:      map[string]*dagNode{},
		EntryNodes: nil,
	}
}
