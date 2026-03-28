package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ucirello/sgai/pkg/state"
)

const todoReadDescription = `Use this tool to read the current to-do list for the session. This tool should be used proactively and frequently to ensure that you are aware of the status of the current task list. You should make use of this tool as often as possible, especially in the following situations:
- At the beginning of conversations to see what's pending
- Before starting new tasks to prioritize work
- When the user asks about previous tasks or plans
- Whenever you're uncertain about what to do next
- After completing tasks to update your understanding of remaining work
- After every few messages to ensure you're on track

Usage:
- This tool takes in no parameters. So leave the input blank or empty. DO NOT include a dummy object, placeholder string or a key like "input" or "empty". LEAVE IT BLANK.
- Returns a list of todo items with their status, priority, and content
- Use this information to track progress and plan next steps
- If no todos exist yet, an empty list will be returned`

const todoWriteDescription = `Use this tool to create and manage a structured task list for your current coding session. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user.
It also helps the user understand the progress of the task and overall progress of their requests.

## When to Use This Tool
Use this tool proactively in these scenarios:

1. Complex multi-step tasks - When a task requires 3 or more distinct steps or actions
2. Non-trivial and complex tasks - Tasks that require careful planning or multiple operations
3. User explicitly requests todo list - When the user directly asks you to use the todo list
4. User provides multiple tasks - When users provide a list of things to be done (numbered or comma-separated)
5. After receiving new instructions - Immediately capture user requirements as todos. Feel free to edit the todo list based on new information.
6. After completing a task - Mark it complete and add any new follow-up tasks
7. When you start working on a new task, mark the todo as in_progress. Ideally you should only have one todo as in_progress at a time. Complete existing tasks before starting new ones.

## When NOT to Use This Tool

Skip using this tool when:
1. There is only a single, straightforward task
2. The task is trivial and tracking it provides no organizational benefit
3. The task can be completed in less than 3 trivial steps
4. The task is purely conversational or informational

NOTE that you should not use this tool if there is only one trivial task to do. In this case you are better off just doing the task directly.

## Examples of When to Use the Todo List

<example>
User: I want to add a dark mode toggle to the application settings. Make sure you run the tests and build when you're done!
Assistant: I'll help add a dark mode toggle to your application settings. Let me create a todo list to track this implementation.
*Creates todo list with the following items:*
1. Create dark mode toggle component in Settings page
2. Add dark mode state management (context/store)
3. Implement CSS-in-JS styles for dark theme
4. Update existing components to support theme switching
5. Run tests and build process, addressing any failures or errors that occur
*Begins working on the first task*

<reasoning>
The assistant used the todo list because:
1. Adding dark mode is a multi-step feature requiring UI, state management, and styling changes
2. The user explicitly requested tests and build be run afterward
3. The assistant inferred that tests and build need to pass by adding "Ensure tests and build succeed" as the final task
</reasoning>
</example>

<example>
User: Help me rename the function getCwd to getCurrentWorkingDirectory across my project
Assistant: Let me first search through your codebase to find all occurrences of 'getCwd'.
*Uses grep or search tools to locate all instances of getCwd in the codebase*
Assistant: I've found 15 instances of 'getCwd' across 8 different files. Let me create a todo list to track these changes.
*Creates todo list with specific items for each file that needs updating*

<reasoning>
The assistant used the todo list because:
1. First, the assistant searched to understand the scope of the task
2. Upon finding multiple occurrences across different files, it determined this was a complex task with multiple steps
3. The todo list helps ensure every instance is tracked and updated systematically
4. This approach prevents missing any occurrences and maintains code consistency
</reasoning>
</example>


<example>
User: I need to implement these features for my e-commerce site: user registration, product catalog, shopping cart, and checkout flow.
Assistant: I'll help implement these features. First, let's add all the features to the todo list.
*Creates a todo list breaking down each feature into specific tasks based on the project architecture*
Assistant: Let's start with user registration. This will involve creating database models, API endpoints, and frontend forms that integrate with your existing authentication system.

<reasoning>
The assistant used the todo list because:
1. The user provided multiple complex features to implement in a comma separated list
2. The todo list helps organize these large features into manageable tasks
3. This approach allows for tracking progress across the entire implementation
</reasoning>
</example>

<example>
User: Can you help optimize my React application? It's rendering slowly and has performance issues.</user>
Assistant: I'll help optimize your React application. First, let me examine your codebase to identify potential performance bottlenecks.
*Reviews component structure, render patterns, state management, and data fetching*
Assistant: After analyzing your codebase, I've identified several performance issues. Let me create a todo list to track our optimization efforts.
*Creates todo list with items like: 1) Implement memoization for expensive calculations in ProductList, 2) Add virtualization for long lists in Dashboard, 3) Optimize image loading in Gallery component, 4) Fix state update loops in ShoppingCart, 5) Review bundle size and implement code splitting*
Let's start by implementing memoization for the expensive calculations in your ProductList component.</assistant>

<reasoning>
The assistant used the todo list because:
1. First, the assistant examined the codebase to identify specific performance issues
2. Based on this analysis, it identified multiple optimization opportunities
3. Performance optimization is a non-trivial task requiring multiple steps
4. The todo list helps methodically track improvements across different components
5. This systematic approach ensures all performance bottlenecks are addressed
</reasoning>
</example>

## Examples of When NOT to Use the Todo List

<example>
User: How do I print 'Hello World' in Python?
Assistant: In Python, you can print "Hello World" with this simple code:

python
print("Hello World")

This will output the text "Hello World" to the console when executed.</assistant>

<reasoning>
The assistant did not use the todo list because this is a single, trivial task that can be completed in one step. There's no need to track multiple tasks or steps for such a straightforward request.
</reasoning>
</example>

<example>
User: What does the git status command do?
Assistant: The git status command shows the current state of your working directory and staging area. It displays which changes have been staged, which haven't, and which files aren't being tracked by Git. This is a fundamental command for understanding the current state of your repository before making commits.

<reasoning>
The assistant did not use the todo list because this is an informational request with no actual coding task to complete. The user is simply asking for an explanation, not for the assistant to perform multiple steps or tasks.
</reasoning>
</example>

<example>
User: Can you add a comment to the calculateTotal function to explain what it does?
Assistant: Sure, let me add a comment to the calculateTotal function to explain what it does.
* Uses the Edit tool to add a comment to the calculateTotal function *

<reasoning>
The assistant did not use the todo list because this is a single, straightforward task confined to one location in the code. Adding a comment doesn't require tracking multiple steps or systematic organization.
</reasoning>
</example>

<example>
User: Run npm install for me and tell me what happens.
Assistant: I'll run the npm install command for you.

*Executes: npm install*

The command completed successfully. Here's the output:
[Output of npm install command]

All dependencies have been installed according to your package.json file.

<reasoning>
The assistant did not use the todo list because this is a single command execution with immediate results. There are no multiple steps to track or organize, making the todo list unnecessary for this straightforward task.
</reasoning>
</example>

## Task States and Management

1. **Task States**: Use these states to track progress:
   - pending: Task not yet started
   - in_progress: Currently working on (limit to ONE task at a time)
   - completed: Task finished successfully
   - cancelled: Task no longer needed

2. **Task Management**:
   - Update task status in real-time as you work
   - Mark tasks complete IMMEDIATELY after finishing (don't batch completions)
   - Only have ONE task in_progress at any time
   - Complete current tasks before starting new ones
   - Cancel tasks that become irrelevant

3. **Task Breakdown**:
   - Create specific, actionable items
   - Break complex tasks into smaller, manageable steps
   - Use clear, descriptive task names

When in doubt, use this tool. Being proactive with task management demonstrates attentiveness and ensures you complete all requirements successfully.
`

type findSkillsArgs struct {
	Name string `json:"name,omitempty" jsonschema:"Skill name or search query. Omit to list all skills."`
}

type findSnippetsArgs struct {
	Language string `json:"language,omitempty" jsonschema:"Programming language. Omit to list available languages."`
	Query    string `json:"query,omitempty" jsonschema:"Search query for snippet name/description."`
}

type workflowStatus string

type updateWorkflowStateArgs struct {
	Status      workflowStatus `json:"status" jsonschema:"Overall workflow status: 'working' (actively working - may need iteration) or 'agent-done' (agent's work done - needs goal verification) or 'complete' (goals verified as achieved). Valid values: working, agent-done, complete"`
	Task        string         `json:"task" jsonschema:"Current task being worked on (e.g. 'Writing tests for auth endpoints'). Use empty string to clear. Be specific about what you're doing."`
	AddProgress string         `json:"addProgress" jsonschema:"Add a progress note to track what you've accomplished. This will be appended to the progress array. Use this frequently to document your steps."`
}

type sendMessageArgs struct {
	ToAgent string `json:"toAgent" jsonschema:"The agent who will receive this message. Must be one of the agents in the workflow."`
	Body    string `json:"body" jsonschema:"The content of the message to send."`
}

type deleteUnreadMessagesArgs struct {
	IDs []int `json:"ids" jsonschema:"Message IDs to delete. All IDs must exist and must refer to unread messages. Supports deleting one or more unread messages in a batch."`
}

type projectTodoWriteArgs struct {
	Todos []state.TodoItem `json:"todos" jsonschema:"The updated todo list"`
}

type questionItem struct {
	Question    string   `json:"question" jsonschema:"The question to ask"`
	Choices     []string `json:"choices" jsonschema:"Multiple-choice options for this question"`
	MultiSelect bool     `json:"multiSelect,omitempty" jsonschema:"Allow multiple selections (default: false)"`
}

type askUserQuestionArgs struct {
	Questions []questionItem `json:"questions" jsonschema:"Array of questions to present to the user"`
}

type askUserWorkGateArgs struct {
	Summary string `json:"summary" jsonschema:"A comprehensive summary of the definition for the human to review before approving. Must include: GOAL items, brainstorming decisions, task breakdown, and validation criteria."`
}

func schemaFor[T any]() (*jsonschema.Schema, error) {
	return jsonschema.For[T](nil)
}

func buildToolSchema[T any](toolName string) (*jsonschema.Schema, error) {
	schema, errSchema := schemaFor[T]()
	if errSchema != nil {
		return nil, fmt.Errorf("build %s schema: %w", toolName, errSchema)
	}
	return schema, nil
}

const (
	autoProceedAnswer           = "I defer to your judgement, proceed with your recommendations"
	autoRecordQuestionsAnswer   = "Please record your questions into .sgai/PROJECT_MANAGEMENT.md"
	autoSkipRetrospectiveAnswer = "CRITICAL HUMAN PARTNER OVERRIDE: skip retrospective. Report to coordinator that the retrospective is complete"
	humanToolTimeout            = 72 * time.Hour
)

type askUserQuestionFunc func(context.Context, *state.Coordinator, askUserQuestionArgs) (string, error)

type askUserWorkGateFunc func(context.Context, *state.Coordinator, string) (string, error)

type humanToolCallbacks struct {
	question askUserQuestionFunc
	workGate askUserWorkGateFunc
}

func startMCPHTTPServer(workingDir string, coord *state.Coordinator, dagAgents []string) (serverURL string, closeFn func(), err error) {
	listener, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		return "", nil, fmt.Errorf("failed to listen on random port: %w", errListen)
	}

	humanTools := selectHumanToolCallbacks(workingDir, coord)
	serveMux := http.NewServeMux()
	handler, errHandler := buildMCPHTTPHandler(workingDir, coord, dagAgents, humanTools)
	if errHandler != nil {
		if errClose := listener.Close(); errClose != nil {
			log.Println("failed to close MCP listener after setup error:", errClose)
		}
		return "", nil, fmt.Errorf("build MCP HTTP handler: %w", errHandler)
	}
	serveMux.Handle("/mcp", handler)

	httpServer := new(http.Server)
	httpServer.Handler = serveMux
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("MCP HTTP server error:", err)
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)
	url := fmt.Sprintf("http://127.0.0.1:%d/mcp", addr.Port)

	closeFn = func() {
		if err := httpServer.Close(); err != nil {
			log.Println("failed to close MCP HTTP server:", err)
		}
	}

	return url, closeFn, nil
}

func parseAgentIdentityHeader(r *http.Request) string {
	identity := r.Header.Get(agentIdentityHeader)
	if identity == "" {
		return ""
	}
	name, _, _ := strings.Cut(identity, "|")
	if name == "" {
		return ""
	}
	return name
}

func resolveCallerAgent(headerAgent string, coord *state.Coordinator) string {
	if headerAgent == "" {
		if currentAgent := coord.State().CurrentAgent; currentAgent != "" && currentAgent != "coordinator" {
			return currentAgent
		}
		return ""
	}
	if headerAgent != "coordinator" {
		return headerAgent
	}
	if currentAgent := coord.State().CurrentAgent; currentAgent != "" && currentAgent != "coordinator" {
		return currentAgent
	}
	return "coordinator"
}

func buildMCPHTTPHandler(workingDir string, coord *state.Coordinator, dagAgents []string, humanTools humanToolCallbacks) (http.Handler, error) {
	if _, errBuild := buildMCPServerForAgent(workingDir, coord, dagAgents, humanTools, "coordinator"); errBuild != nil {
		return nil, errBuild
	}
	if _, errBuild := buildMCPServerForAgent(workingDir, coord, dagAgents, humanTools, "builder"); errBuild != nil {
		return nil, errBuild
	}
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		server, errBuild := buildMCPServer(workingDir, r, coord, dagAgents, humanTools)
		if errBuild != nil {
			log.Println("failed to build MCP server:", errBuild)
			return mcp.NewServer(newMCPImplementation("sgai"), nil)
		}
		return server
	}, nil), nil
}

func buildMCPServer(workingDir string, r *http.Request, coord *state.Coordinator, dagAgents []string, humanTools humanToolCallbacks) (*mcp.Server, error) {
	agentName := resolveCallerAgent(parseAgentIdentityHeader(r), coord)
	return buildMCPServerForAgent(workingDir, coord, dagAgents, humanTools, agentName)
}

func buildMCPServerForAgent(workingDir string, coord *state.Coordinator, dagAgents []string, humanTools humanToolCallbacks, agentName string) (*mcp.Server, error) {
	server := mcp.NewServer(newMCPImplementation("sgai"), nil)
	mcpCtx := &mcpContext{workingDir: workingDir, coord: coord, dagAgents: dagAgents, agentName: agentName, humanTools: humanTools}

	if errRegister := registerCommonTools(server, mcpCtx, agentName); errRegister != nil {
		return nil, errRegister
	}

	if agentName == "coordinator" {
		if errRegister := registerCoordinatorTools(server, mcpCtx); errRegister != nil {
			return nil, errRegister
		}
	}

	return server, nil
}

func newMCPImplementation(name string) *mcp.Implementation {
	return &mcp.Implementation{
		Name:       name,
		Title:      "",
		Version:    "",
		WebsiteURL: "",
		Icons:      nil,
	}
}

func newMCPTool(name, description string, inputSchema *jsonschema.Schema) *mcp.Tool {
	return &mcp.Tool{
		Meta:         mcp.Meta{},
		Annotations:  nil,
		Name:         name,
		Description:  description,
		InputSchema:  inputSchema,
		OutputSchema: nil,
		Title:        "",
		Icons:        nil,
	}
}

func newTextContent(text string) *mcp.TextContent {
	return &mcp.TextContent{Text: text, Meta: mcp.Meta{}, Annotations: nil}
}

func newTextCallToolResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Meta:              mcp.Meta{},
		Content:           []mcp.Content{newTextContent(text)},
		StructuredContent: nil,
		IsError:           false,
	}
}

func registerCommonTools(server *mcp.Server, mcpCtx *mcpContext, agentName string) error {
	findSkillsSchema, errSchema := buildToolSchema[findSkillsArgs]("find_skills")
	if errSchema != nil {
		return errSchema
	}
	findSnippetsSchema, errSchema := buildToolSchema[findSnippetsArgs]("find_snippets")
	if errSchema != nil {
		return errSchema
	}
	sendMessageSchema, errSchema := buildToolSchema[sendMessageArgs]("send_message")
	if errSchema != nil {
		return errSchema
	}
	questionSchema, errSchema := buildToolSchema[askUserQuestionArgs]("ask_user_question")
	if errSchema != nil {
		return errSchema
	}
	workGateSchema, errSchema := buildToolSchema[askUserWorkGateArgs]("ask_user_work_gate")
	if errSchema != nil {
		return errSchema
	}
	emptySchema, errSchema := buildToolSchema[struct{}]("empty")
	if errSchema != nil {
		return errSchema
	}

	mcp.AddTool(server, newMCPTool("find_skills", "Search for skills by name or keywords. Returns skill names and descriptions. Use the 'skill' tool to load a skill's full content.", findSkillsSchema), mcpCtx.findSkillsHandler)

	mcp.AddTool(server, newMCPTool("find_snippets", "Find code snippets by language and query.", findSnippetsSchema), mcpCtx.findSnippetsHandler)

	updateWorkflowStateSchema, updateWorkflowStateDescription := buildUpdateWorkflowStateSchema(agentName)
	mcp.AddTool(server, newMCPTool("update_workflow_state", updateWorkflowStateDescription, updateWorkflowStateSchema), mcpCtx.updateWorkflowStateHandler)

	mcp.AddTool(server, newMCPTool("send_message", "Send a message to another agent in the workflow. The message will be stored and delivered when the target agent starts.", sendMessageSchema), mcpCtx.sendMessageHandler)

	mcp.AddTool(server, newMCPTool("check_inbox", "Check for messages sent to the current agent. Returns all unread messages from other agents.", emptySchema), mcpCtx.checkInboxHandler)

	mcp.AddTool(server, newMCPTool("check_outbox", "Check for messages you have already sent. Returns all messages sent by the current agent.", emptySchema), mcpCtx.checkOutboxHandler)

	mcp.AddTool(server, newMCPTool("ask_user_question", "Present one or more multiple-choice questions to the human partner. Depending on the session mode, this tool may wait for the human or return an environment-provided response.", questionSchema), mcpCtx.askUserQuestionHandler)

	mcp.AddTool(server, newMCPTool("ask_user_work_gate", "Present the work gate approval question. Depending on the session mode, this tool may wait for the human or return an environment-provided response.", workGateSchema), mcpCtx.askUserWorkGateHandler)

	return nil
}

func registerCoordinatorTools(server *mcp.Server, mcpCtx *mcpContext) error {
	emptySchema, errSchema := buildToolSchema[struct{}]("empty")
	if errSchema != nil {
		return errSchema
	}
	deleteUnreadMessagesSchema, errSchema := buildToolSchema[deleteUnreadMessagesArgs]("delete_unread_messages")
	if errSchema != nil {
		return errSchema
	}
	projectTodoWriteSchema, errSchema := buildToolSchema[projectTodoWriteArgs]("project_todowrite")
	if errSchema != nil {
		return errSchema
	}

	mcp.AddTool(server, newMCPTool("peek_message_bus", "Check all messages in the system (both pending and read). Returns all messages in reverse chronological order (most recent first). Coordinator-only tool for monitoring inter-agent communication.", emptySchema), mcpCtx.peekMessageBusHandler)

	mcp.AddTool(server, newMCPTool("delete_unread_messages", "Delete one or more unread messages from the message bus by ID. Coordinator-only tool. All provided IDs must refer to unread messages.", deleteUnreadMessagesSchema), mcpCtx.deleteUnreadMessagesHandler)

	mcp.AddTool(server, newMCPTool("project_todowrite", todoWriteDescription, projectTodoWriteSchema), mcpCtx.projectTodoWriteHandler)

	mcp.AddTool(server, newMCPTool("project_todoread", todoReadDescription, emptySchema), mcpCtx.projectTodoReadHandler)

	return nil
}

type mcpContext struct {
	workingDir string
	coord      *state.Coordinator
	dagAgents  []string
	agentName  string
	humanTools humanToolCallbacks
}

type emptyResult struct{}

func (c *mcpContext) findSkillsHandler(_ context.Context, _ *mcp.CallToolRequest, args findSkillsArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := findSkills(c.workingDir, args.Name)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) findSnippetsHandler(_ context.Context, _ *mcp.CallToolRequest, args findSnippetsArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := findSnippets(c.workingDir, args.Language, args.Query)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) updateWorkflowStateHandler(_ context.Context, _ *mcp.CallToolRequest, args updateWorkflowStateArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := updateWorkflowState(c.coord, c.agentName, args)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) sendMessageHandler(_ context.Context, _ *mcp.CallToolRequest, args sendMessageArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := sendMessage(c.workingDir, c.coord, c.dagAgents, c.agentName, args.ToAgent, args.Body)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) checkInboxHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyResult, error) {
	result, err := checkInbox(c.coord, c.agentName)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) checkOutboxHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyResult, error) {
	result, err := checkOutbox(c.coord, c.agentName)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) peekMessageBusHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyResult, error) {
	result, err := peekMessageBus(c.coord)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) deleteUnreadMessagesHandler(_ context.Context, _ *mcp.CallToolRequest, args deleteUnreadMessagesArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := deleteUnreadMessages(c.coord, c.agentName, args.IDs)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) projectTodoWriteHandler(_ context.Context, _ *mcp.CallToolRequest, args projectTodoWriteArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := projectTodoWrite(c.coord, args.Todos)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) projectTodoReadHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyResult, error) {
	result, err := projectTodoRead(c.coord)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) askUserQuestionHandler(ctx context.Context, _ *mcp.CallToolRequest, args askUserQuestionArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := c.askUserQuestionResponder()(ctx, c.coord, args)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) askUserWorkGateHandler(ctx context.Context, _ *mcp.CallToolRequest, args askUserWorkGateArgs) (*mcp.CallToolResult, emptyResult, error) {
	result, err := c.askUserWorkGateResponder()(ctx, c.coord, args.Summary)
	if err != nil {
		return nil, emptyResult{}, err
	}
	return newTextCallToolResult(result), emptyResult{}, nil
}

func (c *mcpContext) askUserQuestionResponder() askUserQuestionFunc {
	if c.humanTools.question != nil {
		return c.humanTools.question
	}
	return askUserQuestion
}

func (c *mcpContext) askUserWorkGateResponder() askUserWorkGateFunc {
	if c.humanTools.workGate != nil {
		return c.humanTools.workGate
	}
	return askUserWorkGate
}

func askUserQuestion(ctx context.Context, coord *state.Coordinator, args askUserQuestionArgs) (string, error) {
	if coord == nil {
		return "Error: workflow coordinator not available.", nil
	}

	switch coord.State().InteractionMode {
	case state.ModeSelfDrive, state.ModeContinuous:
		return askUserQuestionAutoResponse(autoProceedAnswer)(ctx, coord, args)
	}

	return askUserQuestionInteractive(ctx, coord, args)
}

func askUserQuestionInteractive(ctx context.Context, coord *state.Coordinator, args askUserQuestionArgs) (string, error) {
	if coord == nil {
		return "Error: workflow coordinator not available.", nil
	}

	if validationErr := validateAskUserQuestionArgs(args); validationErr != "" {
		return validationErr, nil
	}

	question, humanMessage, questionSummary := buildQuestionRequest(args)
	answer, errWait := waitForHumanResponse(ctx, coord, question, humanMessage, "ask_user_question")
	if errWait != nil {
		return "", fmt.Errorf("waiting for human response: %w", errWait)
	}

	return questionSummary + "\nHuman response: " + answer, nil
}

func askUserQuestionAutoResponse(answer string) askUserQuestionFunc {
	return func(_ context.Context, _ *state.Coordinator, args askUserQuestionArgs) (string, error) {
		if validationErr := validateAskUserQuestionArgs(args); validationErr != "" {
			return validationErr, nil
		}
		return answer, nil
	}
}

func validateAskUserQuestionArgs(args askUserQuestionArgs) string {
	if len(args.Questions) == 0 {
		return `Error: At least one question is required. You must provide questions in this format: {"questions": [{"question": "Your question text?", "choices": ["Choice 1", "Choice 2"], "multiSelect": false}]}`
	}

	for i, q := range args.Questions {
		if len(q.Choices) == 0 {
			return fmt.Sprintf("Error: Question %d has no choices", i+1)
		}
	}

	return ""
}

func buildQuestionRequest(args askUserQuestionArgs) (question *state.MultiChoiceQuestion, humanMessage, questionSummary string) {
	questions := make([]state.QuestionItem, len(args.Questions))
	for i, q := range args.Questions {
		questions[i] = state.QuestionItem{
			Question:    q.Question,
			Choices:     q.Choices,
			MultiSelect: q.MultiSelect,
		}
	}

	question = &state.MultiChoiceQuestion{Questions: questions, IsWorkGate: false}
	humanMessage = args.Questions[0].Question

	var result strings.Builder
	fmt.Fprintf(&result, "Presented %d question(s) to user:\n", len(args.Questions))
	for i, q := range args.Questions {
		fmt.Fprintf(&result, "\nQuestion %d: %s\n", i+1, q.Question)
		fmt.Fprintf(&result, "  Choices: %v\n", q.Choices)
		fmt.Fprintf(&result, "  MultiSelect: %v\n", q.MultiSelect)
	}

	return question, humanMessage, result.String()
}

func askUserWorkGate(ctx context.Context, coord *state.Coordinator, summary string) (string, error) {
	if validationErr := validateAskUserWorkGateSummary(summary); validationErr != "" {
		return validationErr, nil
	}

	if coord == nil {
		return "Error: workflow coordinator not available.", nil
	}

	switch coord.State().InteractionMode {
	case state.ModeSelfDrive, state.ModeContinuous:
		return askUserWorkGateAutoResponse(autoRecordQuestionsAnswer)(ctx, coord, summary)
	}

	return askUserWorkGateInteractive(ctx, coord, summary)
}

func askUserWorkGateInteractive(ctx context.Context, coord *state.Coordinator, summary string) (string, error) {
	if coord == nil {
		return "Error: workflow coordinator not available.", nil
	}

	if validationErr := validateAskUserWorkGateSummary(summary); validationErr != "" {
		return validationErr, nil
	}

	questionText := summary + "\n\n---\n\nIs the definition complete? May I begin implementation?"

	question := &state.MultiChoiceQuestion{
		Questions: []state.QuestionItem{
			{
				Question:    questionText,
				Choices:     []string{workGateApprovalText, "Not ready yet, need more clarification"},
				MultiSelect: false,
			},
		},
		IsWorkGate: true,
	}

	answer, errWait := waitForHumanResponse(ctx, coord, question, questionText, "ask_user_work_gate")
	if errWait != nil {
		return "", fmt.Errorf("waiting for human response: %w", errWait)
	}

	return "Presented work gate question to user:\n\nQuestion: " + questionText + "\n  Choices: [DEFINITION IS COMPLETE, BUILD MAY BEGIN, Not ready yet, need more clarification]\n  MultiSelect: false\n\nHuman response: " + answer, nil
}

func askUserWorkGateAutoResponse(answer string) askUserWorkGateFunc {
	return func(_ context.Context, coord *state.Coordinator, summary string) (string, error) {
		if validationErr := validateAskUserWorkGateSummary(summary); validationErr != "" {
			return validationErr, nil
		}
		if errPromote := promoteAfterWorkGate(coord); errPromote != nil {
			return "", errPromote
		}
		return answer, nil
	}
}

func validateAskUserWorkGateSummary(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return "Error: A summary is required. You must compile a comprehensive summary (GOAL items, brainstorming decisions, task breakdown, validation criteria) before asking for work gate approval."
	}
	return ""
}

func waitForHumanResponse(ctx context.Context, coord *state.Coordinator, question *state.MultiChoiceQuestion, humanMessage, toolName string) (string, error) {
	if coord == nil {
		return "", errors.New("workflow coordinator not available")
	}

	ctxWait, cancel := context.WithTimeout(ctx, humanToolTimeout)
	defer cancel()

	answer, errWait := coord.AskAndWait(ctxWait, question, humanMessage)
	if errWait == nil {
		return answer, nil
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	if errors.Is(errWait, context.DeadlineExceeded) {
		log.Printf("%s timed out at %s after %s", toolName, timestamp, humanToolTimeout)
	} else {
		log.Printf("%s stopped waiting at %s: %v", toolName, timestamp, errWait)
	}

	return "", fmt.Errorf("waiting for %s approval: %w", toolName, errWait)
}

func promoteAfterWorkGate(coord *state.Coordinator) error {
	if coord == nil {
		return nil
	}

	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		if wf.InteractionMode == state.ModeBrainstorming {
			wf.InteractionMode = state.ModeBuilding
		}
	}); errUpdate != nil {
		return fmt.Errorf("failed to save work gate approval: %w", errUpdate)
	}

	return nil
}

func selectHumanToolCallbacks(workingDir string, coord *state.Coordinator) humanToolCallbacks {
	if coord == nil {
		return humanToolCallbacks{question: askUserQuestion, workGate: askUserWorkGate}
	}

	switch coord.State().InteractionMode {
	case state.ModeSelfDrive, state.ModeContinuous:
		return humanToolCallbacks{
			question: askUserQuestionAutoResponse(autoProceedAnswer),
			workGate: askUserWorkGateAutoResponse(autoRecordQuestionsAnswer),
		}
	}

	metadata := readGoalMetadata(workingDir)
	if !retrospectiveEnabled(metadata.Retrospective) {
		return humanToolCallbacks{
			question: askUserQuestionAutoResponse(autoProceedAnswer),
			workGate: askUserWorkGateAutoResponse(autoSkipRetrospectiveAnswer),
		}
	}

	return humanToolCallbacks{question: askUserQuestion, workGate: askUserWorkGate}
}

func readGoalMetadata(workingDir string) GoalMetadata {
	if workingDir == "" {
		var metadata GoalMetadata
		return metadata
	}

	data, errRead := os.ReadFile(filepath.Join(workingDir, "GOAL.md"))
	if errRead != nil {
		var metadata GoalMetadata
		return metadata
	}

	metadata, errParse := parseYAMLFrontmatter(data)
	if errParse != nil {
		var emptyMetadata GoalMetadata
		return emptyMetadata
	}

	return metadata
}

func findSkills(workingDir, name string) (string, error) {
	skillsDir := filepath.Join(workingDir, ".sgai", "skills")

	skillFiles, err := collectSkillFiles(skillsDir)
	if err != nil {
		return "", fmt.Errorf("failed to access skills: %w", err)
	}

	if name == "" {
		return listAllSkills(skillsDir, skillFiles)
	}

	if result, found := findSkillByExactMatch(skillsDir, skillFiles, name); found {
		return result, nil
	}

	if result := findSkillsByPrefix(skillsDir, skillFiles, name); result != "" {
		return result, nil
	}

	if result := findSkillsByBasename(skillsDir, skillFiles, name); result != "" {
		return result, nil
	}

	return findSkillsByFuzzyMatch(skillsDir, skillFiles, name)
}

func collectSkillFiles(skillsDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(skillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking skills directory %s: %w", skillsDir, err)
	}
	return files, nil
}

func skillDisplayName(frontmatter map[string]string, relName string) string {
	if n := frontmatter["name"]; n != "" {
		return n
	}
	return filepath.Base(relName)
}

func skillSteeringMessage(name, desc string) string {
	return fmt.Sprintf(
		"Found skill '%s': %s. Use the 'skill({\"name\":%q})' to load its full content.",
		name, desc, name,
	)
}

func skillRelName(skillsDir, file string) string {
	relName, _ := filepath.Rel(skillsDir, file)
	return strings.TrimSuffix(relName, "/SKILL.md")
}

func skillDesc(frontmatter map[string]string) string {
	desc := frontmatter["description"]
	if desc == "" {
		return "No description"
	}
	return desc
}

func listAllSkills(skillsDir string, skillFiles []string) (string, error) {
	var skills []string
	for _, file := range skillFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		frontmatter := parseFrontmatterMap(content)
		relName := skillRelName(skillsDir, file)
		skills = append(skills, fmt.Sprintf("%s: %s", skillDisplayName(frontmatter, relName), skillDesc(frontmatter)))
	}
	return strings.Join(skills, "\n"), nil
}

func findSkillByExactMatch(skillsDir string, skillFiles []string, name string) (string, bool) {
	for _, file := range skillFiles {
		relName := skillRelName(skillsDir, file)
		if relName != name {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return "", false
		}
		frontmatter := parseFrontmatterMap(content)
		return skillSteeringMessage(skillDisplayName(frontmatter, relName), skillDesc(frontmatter)), true
	}
	return "", false
}

func findSkillsByPrefix(skillsDir string, skillFiles []string, name string) string {
	var matches []string
	for _, file := range skillFiles {
		relName := skillRelName(skillsDir, file)
		if !strings.HasPrefix(relName, name) || relName == name {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		frontmatter := parseFrontmatterMap(content)
		matches = append(matches, fmt.Sprintf("%s: %s", skillDisplayName(frontmatter, relName), skillDesc(frontmatter)))
	}
	return strings.Join(matches, "\n")
}

type skillMatchEntry struct {
	name string
	desc string
}

func findSkillsByBasename(skillsDir string, skillFiles []string, name string) string {
	var matches []skillMatchEntry
	for _, file := range skillFiles {
		relName := skillRelName(skillsDir, file)
		if filepath.Base(relName) != name {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		frontmatter := parseFrontmatterMap(content)
		matches = append(matches, skillMatchEntry{skillDisplayName(frontmatter, relName), skillDesc(frontmatter)})
	}
	if len(matches) == 1 {
		return skillSteeringMessage(matches[0].name, matches[0].desc)
	}
	if len(matches) > 1 {
		var results []string
		for _, m := range matches {
			results = append(results, fmt.Sprintf("%s: %s", m.name, m.desc))
		}
		return strings.Join(results, "\n")
	}
	return ""
}

func findSkillsByFuzzyMatch(skillsDir string, skillFiles []string, name string) (string, error) {
	nameLower := strings.ToLower(name)
	var matches []string
	for _, file := range skillFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		frontmatter := parseFrontmatterMap(content)
		contentLower := strings.ToLower(string(content))
		if strings.Contains(strings.ToLower(frontmatter["name"]), nameLower) ||
			strings.Contains(strings.ToLower(frontmatter["description"]), nameLower) ||
			strings.Contains(contentLower, nameLower) {
			relName := skillRelName(skillsDir, file)
			matches = append(matches, fmt.Sprintf("%s: %s", skillDisplayName(frontmatter, relName), skillDesc(frontmatter)))
		}
	}
	return strings.Join(matches, "\n"), nil
}

// findSnippets searches for code snippets in the .sgai/snippets directory.
// When language is empty, it lists available languages. When query is empty,
// it lists all snippets for the language. Otherwise, it searches for matching snippets.
//

func findSnippets(workingDir, language, query string) (string, error) {
	snippetsDir := filepath.Join(workingDir, ".sgai", "snippets")

	if language == "" {
		return listSnippetLanguages(snippetsDir)
	}

	langDir := filepath.Join(snippetsDir, language)
	entries, err := os.ReadDir(langDir)
	if err != nil {
		return "", nil
	}

	if query == "" {
		return listSnippetsForLanguage(langDir, entries)
	}

	return searchSnippets(langDir, entries, query)
}

func listSnippetLanguages(snippetsDir string) (string, error) {
	entries, err := os.ReadDir(snippetsDir)
	if err != nil {
		return "", nil
	}
	var languages []string
	for _, entry := range entries {
		if entry.IsDir() {
			languages = append(languages, entry.Name())
		}
	}
	return strings.Join(languages, "\n"), nil
}

func listSnippetsForLanguage(langDir string, entries []os.DirEntry) (string, error) {
	var snippets []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(langDir, entry.Name()))
		if err != nil {
			continue
		}
		frontmatter := parseFrontmatterMap(content)
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		desc := frontmatter["description"]
		if desc == "" {
			desc = "No description"
		}
		snippets = append(snippets, fmt.Sprintf("%s: %s", name, desc))
	}
	return strings.Join(snippets, "\n"), nil
}

func searchSnippets(langDir string, entries []os.DirEntry, query string) (string, error) {
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(langDir, entry.Name()))
		if err != nil {
			continue
		}
		if strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())) == query {
			return string(content), nil
		}
	}

	if result := findSnippetsByNameContains(langDir, entries, query); result != "" {
		return result, nil
	}

	return findSnippetsByFuzzyMatch(langDir, entries, query)
}

type snippetMatch struct {
	name    string
	content string
	desc    string
}

func findSnippetsByNameContains(langDir string, entries []os.DirEntry, query string) string {
	var matches []snippetMatch
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(langDir, entry.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !strings.Contains(name, query) || name == query {
			continue
		}
		frontmatter := parseFrontmatterMap(content)
		desc := frontmatter["description"]
		if desc == "" {
			desc = "No description"
		}
		matches = append(matches, snippetMatch{name, string(content), desc})
	}
	if len(matches) == 1 {
		return matches[0].content
	}
	if len(matches) > 1 {
		var results []string
		for _, m := range matches {
			results = append(results, fmt.Sprintf("%s: %s", m.name, m.desc))
		}
		return strings.Join(results, "\n")
	}
	return ""
}

func findSnippetsByFuzzyMatch(langDir string, entries []os.DirEntry, query string) (string, error) {
	queryLower := strings.ToLower(query)
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(langDir, entry.Name()))
		if err != nil {
			continue
		}
		frontmatter := parseFrontmatterMap(content)
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.Contains(strings.ToLower(name), queryLower) ||
			strings.Contains(strings.ToLower(frontmatter["description"]), queryLower) {
			desc := frontmatter["description"]
			if desc == "" {
				desc = "No description"
			}
			matches = append(matches, fmt.Sprintf("%s: %s", name, desc))
		}
	}
	return strings.Join(matches, "\n"), nil
}

func updateWorkflowState(coord *state.Coordinator, callerAgent string, args updateWorkflowStateArgs) (string, error) {
	var response string
	var shouldStartWatchdog bool

	if coord == nil {
		return "Error: workflow coordinator not available.", nil
	}

	errUpdate := coord.UpdateState(func(currentState *state.Workflow) {
		nextStatus := currentState.Status
		statusPolicy := workflowStatusPolicyForAgent(callerAgent)

		if args.Status != "" {
			status := strings.Trim(string(args.Status), "\"'")
			if !slices.Contains(statusPolicy.statuses, status) {
				response = fmt.Sprintf("Error: Invalid status '%s'. Must be one of: %s", status, strings.Join(statusPolicy.statuses, ", "))
				return
			}
			nextStatus = status
		}

		if nextStatus == state.StatusAgentDone || nextStatus == state.StatusComplete {
			pendingCount := countPendingTodos(todosForAgent(currentState, callerAgent))
			if pendingCount > 0 {
				response = fmt.Sprintf("Error: Cannot transition to '%s' with %d pending TODO items. Please complete all TODO items first.", nextStatus, pendingCount)
				return
			}
		}

		if currentState.Progress == nil {
			currentState.Progress = []state.ProgressEntry{}
		}

		if args.Status != "" {
			currentState.Status = nextStatus
			shouldStartWatchdog = nextStatus == state.StatusAgentDone
		}

		currentState.Task = args.Task

		if args.AddProgress != "" {
			entry := state.ProgressEntry{
				Timestamp:   time.Now().Format(time.RFC3339),
				Agent:       callerAgent,
				Description: args.AddProgress,
			}
			currentState.Progress = append(currentState.Progress, entry)
		}

		if (currentState.Status == state.StatusComplete || currentState.Status == state.StatusAgentDone) && currentState.Task != "" {
			currentState.Task = ""
		}

		if response == "" {
			response = "State updated successfully.\n"
			response += fmt.Sprintf("  Status: %s\n", currentState.Status)
			if currentState.Task != "" {
				response += fmt.Sprintf("  Current task: %s\n", currentState.Task)
			}
			if args.AddProgress != "" {
				response += fmt.Sprintf("  Added progress note: %s\n", args.AddProgress)
			}
			response += fmt.Sprintf("  Total progress notes: %d", len(currentState.Progress))
		}
	})

	if errUpdate != nil {
		return "", fmt.Errorf("failed to save state: %w", errUpdate)
	}

	if response != "" && strings.HasPrefix(response, "Error:") {
		return response, nil
	}

	if shouldStartWatchdog {
		coord.StartAgentDoneWatchdog(coord.AgentCancel())
	}

	return response, nil
}

func sendMessage(workingDir string, coord *state.Coordinator, dagAgents []string, callerAgent, toAgent, body string) (string, error) {
	if coord == nil {
		return "Error: Could not read state.json. Has the workflow been initialized?", nil
	}

	targetAgentName := extractAgentFromModelID(toAgent)
	if !slices.Contains(dagAgents, targetAgentName) {
		return fmt.Sprintf("Error: Agent '%s' is not in the workflow. Valid agents are: %s", toAgent, strings.Join(dagAgents, ", ")), nil
	}

	var (
		fromAgent string
		result    string
	)
	recipients := messageRecipientsForAgent(workingDir, toAgent, nil)

	errUpdate := coord.UpdateState(func(currentState *state.Workflow) {
		if currentState.Messages == nil {
			currentState.Messages = []state.Message{}
		}

		fromAgent = callerAgent
		if currentState.CurrentModel != "" {
			fromAgent = currentState.CurrentModel
		}

		createdAt := time.Now().UTC().Format(time.RFC3339)
		for _, recipient := range recipients {
			message := newStateMessage(nextMessageID(currentState.Messages), fromAgent, recipient, body, createdAt)
			currentState.Messages = append(currentState.Messages, message)
		}

		if len(recipients) == 1 {
			result = fmt.Sprintf("Message sent successfully to %s.\nFrom: %s\nTo: %s\nBody: %s", recipients[0], fromAgent, recipients[0], body)
		} else {
			result = fmt.Sprintf("Sent %d messages successfully to %s.\nFrom: %s\nTo: %s\nBody: %s", len(recipients), toAgent, fromAgent, strings.Join(recipients, ", "), body)
		}
		if callerAgent != "coordinator" {
			result += "\n\nIMPORTANT: To receive a response from the target agent, you MUST yield control by calling sgai_update_workflow_state({status: 'agent-done'}). The target agent cannot run until you yield."
		}
	})

	if errUpdate != nil {
		return "", fmt.Errorf("failed to save state: %w", errUpdate)
	}

	return result, nil
}

func newStateMessage(id int, fromAgent, toAgent, body, createdAt string) state.Message {
	return state.Message{
		ID:        id,
		FromAgent: fromAgent,
		ToAgent:   toAgent,
		Body:      body,
		Read:      false,
		ReadAt:    "",
		ReadBy:    "",
		CreatedAt: createdAt,
	}
}

func checkInbox(coord *state.Coordinator, callerAgent string) (string, error) {
	if coord == nil {
		return "Error: Could not read state.json. Has the workflow been initialized?", nil
	}

	snapshot := coord.State()
	currentModel := snapshot.CurrentModel

	var unreadMessages []state.Message
	for i := range snapshot.Messages {
		if messageMatchesRecipient(&snapshot.Messages[i], callerAgent, currentModel) && !snapshot.Messages[i].Read {
			unreadMessages = append(unreadMessages, snapshot.Messages[i])
		}
	}

	if len(unreadMessages) == 0 {
		return "You have no messages.", nil
	}

	timestamp := time.Now().Format(time.RFC3339)
	errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		for i := range wf.Messages {
			if messageMatchesRecipient(&wf.Messages[i], callerAgent, currentModel) && !wf.Messages[i].Read {
				wf.Messages[i].Read = true
				wf.Messages[i].ReadAt = timestamp
				wf.Messages[i].ReadBy = callerAgent
			}
		}
	})
	if errUpdate != nil {
		return "", fmt.Errorf("failed to save state: %w", errUpdate)
	}

	var result strings.Builder
	fmt.Fprintf(&result, "You have %d message(s):\n\n", len(unreadMessages))
	for i := 0; i < len(unreadMessages); i++ {
		msg := unreadMessages[i]
		fmt.Fprintf(&result, "Message %d:\n  From: %s\n  Body: %s\n\n", i+1, msg.FromAgent, msg.Body)
	}

	return strings.TrimSpace(result.String()), nil
}

//nolint:unparam // error is always nil by design - errors are handled by returning user-friendly messages
func checkOutbox(coord *state.Coordinator, callerAgent string) (string, error) {
	if coord == nil {
		return "Error: Could not read state.json. Has the workflow been initialized?", nil
	}

	snapshot := coord.State()
	currentModel := snapshot.CurrentModel

	var unreadMessages []state.Message
	var readMessages []state.Message
	for i := range snapshot.Messages {
		if messageMatchesSender(&snapshot.Messages[i], callerAgent, currentModel) {
			if snapshot.Messages[i].Read {
				readMessages = append(readMessages, snapshot.Messages[i])
			} else {
				unreadMessages = append(unreadMessages, snapshot.Messages[i])
			}
		}
	}

	if len(unreadMessages) == 0 && len(readMessages) == 0 {
		return "You have not sent any messages.", nil
	}

	var result strings.Builder

	if len(unreadMessages) > 0 {
		fmt.Fprintf(&result, "Pending messages (%d):\n", len(unreadMessages))
		for i, msg := range unreadMessages {
			subject := strings.Split(msg.Body, "\n")[0]
			fmt.Fprintf(&result, "  %d. To: %s | Subject: %s\n", i+1, msg.ToAgent, subject)
		}
		result.WriteString("\n")
	}

	if len(readMessages) > 0 {
		fmt.Fprintf(&result, "Delivered messages (%d):\n", len(readMessages))
		for i, msg := range readMessages {
			subject := strings.Split(msg.Body, "\n")[0]
			readStatus := "Unread"
			if msg.ReadAt != "" {
				readStatus = "Read at " + msg.ReadAt
			}
			fmt.Fprintf(&result, "  %d. To: %s | Subject: %s | %s\n", i+1, msg.ToAgent, subject, readStatus)
		}
	}

	return strings.TrimSpace(result.String()), nil
}

//nolint:unparam // error is always nil by design - errors are handled by returning user-friendly messages
func peekMessageBus(coord *state.Coordinator) (string, error) {
	if coord == nil {
		return "Error: Could not read state.json. Has the workflow been initialized?", nil
	}

	snapshot := coord.State()

	if len(snapshot.Messages) == 0 {
		return "No messages in the system.", nil
	}

	var result strings.Builder
	fmt.Fprintf(&result, "Total messages: %d\n\n", len(snapshot.Messages))

	for i := 0; i < len(snapshot.Messages); i++ {
		msg := snapshot.Messages[i]
		fmt.Fprintf(&result, "Message %d (ID: %d):\n", i+1, msg.ID)
		fmt.Fprintf(&result, "  From: %s\n", msg.FromAgent)
		fmt.Fprintf(&result, "  To: %s\n", msg.ToAgent)
		if msg.Read {
			result.WriteString("  Status: read\n")
			if msg.ReadAt != "" {
				fmt.Fprintf(&result, "  Read At: %s\n", msg.ReadAt)
			}
		} else {
			result.WriteString("  Status: pending\n")
		}
		fmt.Fprintf(&result, "  Body: %s\n\n", msg.Body)
	}

	return strings.TrimSpace(result.String()), nil
}

func deleteUnreadMessages(coord *state.Coordinator, agentName string, ids []int) (string, error) {
	if coord == nil {
		return "Error: Could not read state.json. Has the workflow been initialized?", nil
	}

	if agentName != "coordinator" {
		return "Error: delete_unread_messages is a coordinator-only tool", nil
	}

	normalizedIDs := normalizedMessageIDs(ids)
	if len(normalizedIDs) == 0 {
		return "Error: At least one message ID is required", nil
	}

	idSet := make(map[int]struct{}, len(normalizedIDs))
	for _, id := range normalizedIDs {
		idSet[id] = struct{}{}
	}

	var invalidIDs []int
	errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		invalidIDs = invalidUnreadMessageIDs(wf.Messages, normalizedIDs)
		if len(invalidIDs) > 0 {
			return
		}

		wf.Messages = slices.DeleteFunc(wf.Messages, func(msg state.Message) bool {
			_, ok := idSet[msg.ID]
			return ok
		})
	})
	if errUpdate != nil {
		return "", fmt.Errorf("failed to save state: %w", errUpdate)
	}
	if len(invalidIDs) > 0 {
		return fmt.Sprintf("Error: Message IDs %s must all be unread", joinMessageIDs(invalidIDs)), nil
	}

	return "Deleted unread messages: " + joinMessageIDs(normalizedIDs), nil
}

func invalidUnreadMessageIDs(messages []state.Message, ids []int) []int {
	unreadIDs := make(map[int]struct{}, len(messages))
	for _, msg := range messages {
		if !msg.Read {
			unreadIDs[msg.ID] = struct{}{}
		}
	}

	invalidIDs := make([]int, 0)
	for _, id := range ids {
		if _, ok := unreadIDs[id]; !ok {
			invalidIDs = append(invalidIDs, id)
		}
	}

	return invalidIDs
}

func normalizedMessageIDs(ids []int) []int {
	normalizedIDs := slices.Clone(ids)
	slices.Sort(normalizedIDs)
	return slices.Compact(normalizedIDs)
}

func joinMessageIDs(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ", ")
}

func projectTodoWrite(coord *state.Coordinator, todos []state.TodoItem) (string, error) {
	if coord == nil {
		return "Error: workflow coordinator not available.", nil
	}

	if err := coord.UpdateState(func(wf *state.Workflow) {
		wf.ProjectTodos = todos
	}); err != nil {
		return "", fmt.Errorf("failed to save state: %w", err)
	}

	return formatTodoList(todos), nil
}

func formatTodoList(todos []state.TodoItem) string {
	nonCompletedCount := 0
	for _, todo := range todos {
		if todo.Status != "completed" {
			nonCompletedCount++
		}
	}

	var result strings.Builder
	fmt.Fprintf(&result, "%d todos\n", nonCompletedCount)
	for _, todo := range todos {
		symbol := todoStatusSymbol(todo.Status)
		fmt.Fprintf(&result, "→ %s %s (%s)\n", symbol, todo.Content, todo.Priority)
	}

	return strings.TrimSuffix(result.String(), "\n")
}

//nolint:unparam // error is always nil by design - errors are handled by returning "0 todos"
func projectTodoRead(coord *state.Coordinator) (string, error) {
	if coord == nil {
		return "0 todos", nil
	}
	return formatTodoList(coord.State().ProjectTodos), nil
}

func messageMatchesRecipient(msg *state.Message, currentAgent, currentModel string) bool {
	if msg.ToAgent == currentAgent {
		return true
	}
	if currentModel != "" && msg.ToAgent == currentModel {
		return true
	}
	return false
}

func messageMatchesSender(msg *state.Message, currentAgent, currentModel string) bool {
	if msg.FromAgent == currentAgent {
		return true
	}
	if currentModel != "" && msg.FromAgent == currentModel {
		return true
	}
	return false
}

type workflowStatusPolicy struct {
	statuses    []string
	description string
}

func workflowStatusPolicyForAgent(agent string) workflowStatusPolicy {
	policy := workflowStatusPolicy{
		statuses:    []string{state.StatusWorking, state.StatusAgentDone},
		description: "Overall workflow status: 'working' (actively working - may need iteration) or 'agent-done' (agent's work done - needs goal verification). Valid values: working, agent-done",
	}
	if agent == "coordinator" {
		policy.statuses = append(policy.statuses, state.StatusComplete)
		policy.description = "Overall workflow status: 'working' (actively working - may need iteration) or 'agent-done' (agent's work done - needs goal verification) or 'complete' (goals verified as achieved). Valid values: working, agent-done, complete"
	}
	return policy
}

func newStringSchema(description string) *jsonschema.Schema {
	schema := new(jsonschema.Schema)
	schema.Type = "string"
	schema.Description = description
	return schema
}

func newEnumStringSchema(enum []any, description string) *jsonschema.Schema {
	schema := newStringSchema(description)
	schema.Enum = enum
	return schema
}

func newObjectSchema(properties map[string]*jsonschema.Schema, required []string) *jsonschema.Schema {
	schema := new(jsonschema.Schema)
	schema.Type = "object"
	schema.Properties = properties
	schema.Required = required
	return schema
}

func buildUpdateWorkflowStateSchema(currentAgent string) (schema *jsonschema.Schema, description string) {
	description = "Update the workflow state file (.sgai/state.json). Use this tool to track your progress throughout your work. Update regularly after each major step. Examples: Set task when starting work, add progress notes as you complete steps, mark complete when done."
	statusPolicy := workflowStatusPolicyForAgent(currentAgent)
	statusEnum := make([]any, len(statusPolicy.statuses))
	for i, status := range statusPolicy.statuses {
		statusEnum[i] = status
	}

	statusSchema := newEnumStringSchema(statusEnum, statusPolicy.description)
	taskSchema := newStringSchema("Current task being worked on (e.g. 'Writing tests for auth endpoints'). Use empty string to clear. Be specific about what you're doing.")
	addProgressSchema := newStringSchema("Add a progress note to track what you've accomplished. This will be appended to the progress array. Use this frequently to document your steps.")

	schema = newObjectSchema(map[string]*jsonschema.Schema{
		"status":      statusSchema,
		"task":        taskSchema,
		"addProgress": addProgressSchema,
	}, []string{"status", "task", "addProgress"})

	return schema, description
}
