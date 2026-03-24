package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/sgai/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAllAgents(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  []string
	}{
		{"alreadyHasCoordinator", []string{"coordinator", "builder", "reviewer"}, []string{"coordinator", "builder", "reviewer"}},
		{"noCoordinator", []string{"builder", "reviewer"}, []string{"coordinator", "builder", "reviewer"}},
		{"empty", []string{}, []string{"coordinator"}},
		{"onlyCoordinator", []string{"coordinator"}, []string{"coordinator"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAllAgents(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestComputeLongestNameLen(t *testing.T) {
	cases := []struct {
		name   string
		agents []string
		want   int
	}{
		{"empty", []string{}, len("sgai")},
		{"shortNames", []string{"a", "bb"}, len("sgai")},
		{"longName", []string{"very-long-agent-name"}, len("very-long-agent-name")},
		{"mixedLengths", []string{"ab", "coordinator", "z"}, len("coordinator")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeLongestNameLen(tc.agents)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveCurrentAgent(t *testing.T) {
	t.Run("emptyDefaultsToCoordinator", func(t *testing.T) {
		r := &workflowRunner{wfState: state.Workflow{CurrentAgent: ""}}
		assert.Equal(t, "coordinator", r.resolveCurrentAgent())
	})

	t.Run("returnsCurrentAgent", func(t *testing.T) {
		r := &workflowRunner{wfState: state.Workflow{CurrentAgent: "builder"}}
		assert.Equal(t, "builder", r.resolveCurrentAgent())
	})
}

func buildTestDag(edges map[string][]string, entryNodes []string) *dag {
	d := &dag{
		Nodes:      make(map[string]*dagNode),
		EntryNodes: entryNodes,
	}
	for from, toList := range edges {
		node := d.ensureNode(from)
		for _, to := range toList {
			toNode := d.ensureNode(to)
			node.Successors = append(node.Successors, to)
			toNode.Predecessors = append(toNode.Predecessors, from)
		}
	}
	return d
}

func TestResolveNextAgent(t *testing.T) {
	t.Run("redirectsToPendingMessages", func(t *testing.T) {
		r := &workflowRunner{
			paddedsgai: "test",
			flowDag:    buildTestDag(map[string][]string{"coordinator": {"reviewer"}}, []string{"coordinator"}),
			wfState: state.Workflow{
				Messages: []state.Message{
					{ID: 1, FromAgent: "coordinator", ToAgent: "reviewer", Body: "review please", Read: false},
				},
			},
		}
		got := r.resolveNextAgent("coordinator")
		assert.Equal(t, "reviewer", got)
	})

	t.Run("terminalNodeReturnsCoordinator", func(t *testing.T) {
		r := &workflowRunner{
			paddedsgai: "test",
			flowDag:    buildTestDag(map[string][]string{"coordinator": {"reviewer"}}, []string{"coordinator"}),
			wfState:    state.Workflow{Messages: []state.Message{}},
		}
		got := r.resolveNextAgent("reviewer")
		assert.Equal(t, "coordinator", got)
	})

	t.Run("coordinatorGoesToFirstEntry", func(t *testing.T) {
		r := &workflowRunner{
			paddedsgai: "test",
			flowDag:    buildTestDag(map[string][]string{"coordinator": {"builder"}}, []string{"builder"}),
			wfState:    state.Workflow{Messages: []state.Message{}},
		}
		got := r.resolveNextAgent("coordinator")
		assert.Equal(t, "builder", got)
	})
}

func TestPrepareAgent(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))

	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{
		Status:      state.StatusWorking,
		VisitCounts: map[string]int{},
	})
	require.NoError(t, errCoord)

	r := &workflowRunner{
		dir:           dir,
		paddedsgai:    "test",
		coord:         coord,
		previousAgent: "",
		wfState: state.Workflow{
			Status:      state.StatusWorking,
			VisitCounts: map[string]int{},
		},
	}

	require.NoError(t, r.prepareAgent("coordinator"))
	assert.Equal(t, "coordinator", r.previousAgent)
	assert.Equal(t, "coordinator", r.wfState.CurrentAgent)
	assert.Equal(t, 1, r.wfState.VisitCounts["coordinator"])

	require.NoError(t, r.prepareAgent("builder"))
	assert.Equal(t, "builder", r.previousAgent)
	assert.Equal(t, "builder", r.wfState.CurrentAgent)
	assert.Equal(t, 1, r.wfState.VisitCounts["builder"])
	assert.Empty(t, r.wfState.Todos)
}

func TestHandleTrigger(t *testing.T) {
	t.Run("ignoresNonSteeringTrigger", func(t *testing.T) {
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{})
		require.NoError(t, errCoord)

		r := &workflowRunner{coord: coord}
		goalPath := filepath.Join(dir, "GOAL.md")
		require.NoError(t, os.WriteFile(goalPath, []byte("# Goal"), 0o644))

		r.handleTrigger(triggerAuto, goalPath)
	})

	t.Run("steeringNoMessages", func(t *testing.T) {
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{Messages: []state.Message{}})
		require.NoError(t, errCoord)

		r := &workflowRunner{coord: coord}
		goalPath := filepath.Join(dir, "GOAL.md")
		require.NoError(t, os.WriteFile(goalPath, []byte("# Goal"), 0o644))

		r.handleTrigger(triggerSteering, goalPath)
	})

	t.Run("steeringWithHumanMessage", func(t *testing.T) {
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{
			Messages: []state.Message{
				{
					ID:        1,
					FromAgent: "Human Partner",
					ToAgent:   "coordinator",
					Body:      "Add logging",
					Read:      false,
				},
			},
		})
		require.NoError(t, errCoord)

		r := &workflowRunner{coord: coord}
		goalPath := filepath.Join(dir, "GOAL.md")
		require.NoError(t, os.WriteFile(goalPath, []byte("# Goal\n\nOriginal content"), 0o644))

		r.handleTrigger(triggerSteering, goalPath)

		goalContent, errRead := os.ReadFile(goalPath)
		require.NoError(t, errRead)
		assert.Contains(t, string(goalContent), "Add logging")
	})
}

func TestPrepareAgentReappliesOverlayWithoutSkeletonUnpack(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".sgai", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "agent"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sgai", "skills", "handoff-overlay"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".sgai", "agent", "coordinator.md"), []byte("runtime coordinator"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sgai", "skills", "handoff-overlay", "SKILL.md"), []byte("handoff overlay"), 0o644))

	coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{VisitCounts: map[string]int{}})
	require.NoError(t, errCoord)

	r := &workflowRunner{
		dir:           dir,
		coord:         coord,
		paddedsgai:    "test",
		previousAgent: "coordinator",
		wfState: state.Workflow{
			VisitCounts: map[string]int{},
		},
	}

	err := r.prepareAgent("builder")
	require.NoError(t, err)

	coordinatorContent, errRead := os.ReadFile(filepath.Join(dir, ".sgai", "agent", "coordinator.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "runtime coordinator", string(coordinatorContent))

	overlayContent, errRead := os.ReadFile(filepath.Join(dir, ".sgai", "skills", "handoff-overlay", "SKILL.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "handoff overlay", string(overlayContent))
}

func TestRunAgentInterruptsWhenOverlayRefreshFails(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".sgai", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".sgai", "skills"), []byte("not a directory"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sgai", "skills", "broken-overlay"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sgai", "skills", "broken-overlay", "SKILL.md"), []byte("overlay"), 0o644))

	coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{VisitCounts: map[string]int{}})
	require.NoError(t, errCoord)

	r := &workflowRunner{
		dir:           dir,
		coord:         coord,
		paddedsgai:    "test",
		previousAgent: "coordinator",
		wfState: state.Workflow{
			VisitCounts: map[string]int{},
		},
	}

	result := r.runAgent(context.Background(), "builder")
	assert.Equal(t, resultInterrupt, result)
}

func TestResolveRetrospectiveDirResuming(t *testing.T) {
	dir := t.TempDir()
	retroDir := filepath.Join(dir, ".sgai", "retrospectives", "2026-03-06-12-00.abcd")
	require.NoError(t, os.MkdirAll(retroDir, 0755))

	pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")
	retroDirRel, errRel := filepath.Rel(dir, retroDir)
	require.NoError(t, errRel)
	pmContent := "---\nRetrospective Session: " + retroDirRel + "\n---\n"
	require.NoError(t, os.WriteFile(pmPath, []byte(pmContent), 0644))

	stateJSONPath := filepath.Join(dir, ".sgai", "state.json")
	goalPath := filepath.Join(dir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("# Test Goal"), 0644))

	result, errResolve := resolveRetrospectiveDir(true, dir, filepath.Join(dir, ".sgai", "retrospectives"), pmPath, stateJSONPath, goalPath)
	require.NoError(t, errResolve)
	assert.Equal(t, retroDir, result)
}

func TestResolveRetrospectiveDirNewSession(t *testing.T) {
	dir := t.TempDir()
	retrospectivesBaseDir := filepath.Join(dir, ".sgai", "retrospectives")
	pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")
	stateJSONPath := filepath.Join(dir, ".sgai", "state.json")
	goalPath := filepath.Join(dir, "GOAL.md")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(goalPath, []byte("# Test Goal"), 0o644))

	retroDir, errResolve := resolveRetrospectiveDir(false, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath)
	require.NoError(t, errResolve)
	assert.NotEmpty(t, retroDir)
	assert.DirExists(t, retroDir)

	goalCopy := filepath.Join(retroDir, "GOAL.md")
	assert.FileExists(t, goalCopy)

	assert.FileExists(t, pmPath)

	_, errStatState := os.Stat(stateJSONPath)
	assert.True(t, os.IsNotExist(errStatState))
}

func TestPrepareRetrospectiveDirFallsBackToFreshSessionOnResumeError(t *testing.T) {
	testCases := []struct {
		name                   string
		writeProjectManagement func(*testing.T, string, string)
		wantLogContains        string
	}{
		{
			name: "emptyRetrospectiveSession",
			writeProjectManagement: func(t *testing.T, _, pmPath string) {
				t.Helper()
				require.NoError(t, os.WriteFile(pmPath, []byte("---\nRetrospective Session: \n---\n"), 0o644))
			},
			wantLogContains: "empty Retrospective Session in PROJECT_MANAGEMENT.md",
		},
		{
			name: "missingRetrospectiveSession",
			writeProjectManagement: func(t *testing.T, _, pmPath string) {
				t.Helper()
				require.NoError(t, os.WriteFile(pmPath, []byte("---\n---\n"), 0o644))
			},
			wantLogContains: "missing Retrospective Session in PROJECT_MANAGEMENT.md",
		},
		{
			name: "missingClosingFrontmatterDelimiter",
			writeProjectManagement: func(t *testing.T, _, pmPath string) {
				t.Helper()
				content := "---\nRetrospective Session: .sgai/retrospectives/2026-03-06-12-00.malformed\n## Content\n"
				require.NoError(t, os.WriteFile(pmPath, []byte(content), 0o644))
			},
			wantLogContains: "missing closing frontmatter delimiter in PROJECT_MANAGEMENT.md",
		},
		{
			name: "malformedClosingFrontmatterDelimiter",
			writeProjectManagement: func(t *testing.T, _, pmPath string) {
				t.Helper()
				content := "---\nRetrospective Session: .sgai/retrospectives/2026-03-06-12-00.malformed\n----\n"
				require.NoError(t, os.WriteFile(pmPath, []byte(content), 0o644))
			},
			wantLogContains: "missing closing frontmatter delimiter in PROJECT_MANAGEMENT.md",
		},
		{
			name: "unreadableProjectManagement",
			writeProjectManagement: func(t *testing.T, _, pmPath string) {
				t.Helper()
				require.NoError(t, os.Mkdir(pmPath, 0o755))
			},
			wantLogContains: "read PROJECT_MANAGEMENT.md",
		},
		{
			name: "missingRetrospectiveDirectory",
			writeProjectManagement: func(t *testing.T, dir, pmPath string) {
				t.Helper()
				missingRetroDirRel := filepath.Join(".sgai", "retrospectives", "2026-03-06-12-00.missing")
				pmContent := "---\nRetrospective Session: " + missingRetroDirRel + "\n---\n"
				require.NoError(t, os.WriteFile(pmPath, []byte(pmContent), 0o644))
				assert.NoDirExists(t, filepath.Join(dir, missingRetroDirRel))
			},
			wantLogContains: "retrospective directory from PROJECT_MANAGEMENT.md does not exist",
		},
		{
			name: "retrospectiveSessionPointsToFile",
			writeProjectManagement: func(t *testing.T, dir, pmPath string) {
				t.Helper()
				retrospectiveFileRel := filepath.Join(".sgai", "retrospectives", "2026-03-06-12-00.file")
				retrospectiveFilePath := filepath.Join(dir, retrospectiveFileRel)
				require.NoError(t, os.MkdirAll(filepath.Dir(retrospectiveFilePath), 0o755))
				require.NoError(t, os.WriteFile(retrospectiveFilePath, []byte("not a directory"), 0o644))
				pmContent := "---\nRetrospective Session: " + retrospectiveFileRel + "\n---\n"
				require.NoError(t, os.WriteFile(pmPath, []byte(pmContent), 0o644))
			},
			wantLogContains: "retrospective directory from PROJECT_MANAGEMENT.md is not a directory",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			retrospectivesBaseDir := filepath.Join(dir, ".sgai", "retrospectives")
			pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")
			stateJSONPath := filepath.Join(dir, ".sgai", "state.json")
			goalPath := filepath.Join(dir, "GOAL.md")

			require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
			require.NoError(t, os.WriteFile(goalPath, []byte("# Test Goal"), 0o644))
			require.NoError(t, os.WriteFile(stateJSONPath, []byte(`{"status":"working"}`), 0o644))
			tc.writeProjectManagement(t, dir, pmPath)

			var logOutput bytes.Buffer
			originalWriter := log.Writer()
			log.SetOutput(&logOutput)
			t.Cleanup(func() {
				log.SetOutput(originalWriter)
			})

			retroDir, resuming := prepareRetrospectiveDir(true, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath)

			assert.False(t, resuming)
			assert.NotEmpty(t, retroDir)
			assert.DirExists(t, retroDir)
			assert.FileExists(t, filepath.Join(retroDir, "GOAL.md"))

			retroDirRel, errRel := filepath.Rel(dir, retroDir)
			require.NoError(t, errRel)

			pmContent, errRead := os.ReadFile(pmPath)
			require.NoError(t, errRead)
			assert.Contains(t, string(pmContent), "Retrospective Session: "+retroDirRel)
			assert.Contains(t, logOutput.String(), tc.wantLogContains)

			_, errStatState := os.Stat(stateJSONPath)
			assert.True(t, os.IsNotExist(errStatState))
		})
	}
}

func TestPrepareRetrospectiveDirDoesNotRetryFreshSessionFailure(t *testing.T) {
	dir := t.TempDir()
	retrospectivesBaseDir := filepath.Join(dir, ".sgai", "retrospectives")
	pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")
	stateJSONPath := filepath.Join(dir, ".sgai", "state.json")
	goalPath := filepath.Join(dir, "missing-goal.md")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))

	var logOutput bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&logOutput)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
	})

	retroDir, resuming := prepareRetrospectiveDir(false, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath)

	assert.Empty(t, retroDir)
	assert.False(t, resuming)
	assert.NotContains(t, logOutput.String(), "[sgai] warning:")

	entries, errReadDir := os.ReadDir(retrospectivesBaseDir)
	require.NoError(t, errReadDir)
	assert.Len(t, entries, 1)
}

func TestResolveRetrospectiveDirNewSessionErrorsReturnInsteadOfFatal(t *testing.T) {
	if os.Getenv("SGAI_HELPER_RETRO_DIR_NEW_SESSION_ERROR") == "1" {
		dir := t.TempDir()
		retrospectivesBaseDir := filepath.Join(dir, "blocked")
		pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")
		stateJSONPath := filepath.Join(dir, ".sgai", "state.json")
		goalPath := filepath.Join(dir, "GOAL.md")

		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
		require.NoError(t, os.WriteFile(goalPath, []byte("# Test Goal"), 0o644))
		require.NoError(t, os.WriteFile(retrospectivesBaseDir, []byte("blocked"), 0o644))

		retroDir, errResolve := resolveRetrospectiveDir(false, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath)
		require.Empty(t, retroDir)
		require.ErrorContains(t, errResolve, "failed to create retrospective directory")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestResolveRetrospectiveDirNewSessionErrorsReturnInsteadOfFatal$")
	cmd.Env = append(os.Environ(), "SGAI_HELPER_RETRO_DIR_NEW_SESSION_ERROR=1")
	output, errRun := cmd.CombinedOutput()
	require.NoError(t, errRun, "subprocess output:\n%s", string(output))
}

func TestHandleWorkingLoop(t *testing.T) {
	cfg := multiModelConfig{paddedsgai: "test", agent: "builder"}
	sessionID := "session-123"

	t.Run("incrementsCounter", func(t *testing.T) {
		got := handleWorkingLoop(cfg, &sessionID, 0)
		assert.Equal(t, 1, got)
		assert.Equal(t, "session-123", sessionID)
	})

	t.Run("resetsOnMaxIterations", func(t *testing.T) {
		sid := "session-456"
		got := handleWorkingLoop(cfg, &sid, maxConsecutiveWorkingIterations-1)
		assert.Equal(t, 0, got)
		assert.Empty(t, sid)
	})
}

func TestUnlockInteractiveForRetrospective(t *testing.T) {
	t.Run("nonRetrospectiveAgent", func(t *testing.T) {
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{
			InteractionMode: state.ModeBuilding,
		})
		require.NoError(t, errCoord)

		wfState := coord.State()
		unlockInteractiveForRetrospective(&wfState, "coordinator", coord, "test")
		assert.Equal(t, state.ModeBuilding, wfState.InteractionMode)
	})

	t.Run("retrospectiveAlreadyInMode", func(t *testing.T) {
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{
			InteractionMode: state.ModeRetrospective,
		})
		require.NoError(t, errCoord)

		wfState := coord.State()
		unlockInteractiveForRetrospective(&wfState, "retrospective", coord, "test")
		assert.Equal(t, state.ModeRetrospective, wfState.InteractionMode)
	})

	t.Run("retrospectiveUnlocks", func(t *testing.T) {
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, state.Workflow{
			InteractionMode: state.ModeBuilding,
		})
		require.NoError(t, errCoord)

		wfState := coord.State()
		unlockInteractiveForRetrospective(&wfState, "retrospective", coord, "test")
		assert.Equal(t, state.ModeRetrospective, wfState.InteractionMode)
	})
}

func TestCopyProjectManagementToRetrospective(t *testing.T) {
	t.Run("emptyRetrospectiveDir", func(_ *testing.T) {
		copyProjectManagementToRetrospective("/tmp", "")
	})

	t.Run("noPMFile", func(t *testing.T) {
		dir := t.TempDir()
		retroDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
		copyProjectManagementToRetrospective(dir, retroDir)
		_, err := os.Stat(filepath.Join(retroDir, "PROJECT_MANAGEMENT.md"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("copiesPMFile", func(t *testing.T) {
		dir := t.TempDir()
		retroDir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "PROJECT_MANAGEMENT.md"), []byte("# PM\nSome content"), 0o644))

		copyProjectManagementToRetrospective(dir, retroDir)

		content, err := os.ReadFile(filepath.Join(retroDir, "PROJECT_MANAGEMENT.md"))
		require.NoError(t, err)
		assert.Contains(t, string(content), "Some content")
	})
}
