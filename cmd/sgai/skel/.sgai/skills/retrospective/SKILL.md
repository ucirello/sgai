---
name: retrospective
description: Post-completion factory improvement analysis. Guides the retrospective agent through artifact discovery, pattern analysis, suggestion generation, and presenting proposed changes for approval. Use when the retrospective agent starts its post-completion phase. Treat `sgai/` overlay changes and `AGENTS.md` changes as co-equal categories when evidence supports them, prefer `sgai/` for process and tooling fixes, and report separate Skills, Agent Prompts, Snippets, and `AGENTS.md` buckets including empty buckets.
---

# Retrospective Analysis

## Overview

This skill guides the retrospective agent through analyzing a completed session and producing actionable improvements to the factory. The goal is to make the factory better over time by examining what happened, identifying patterns, and proposing concrete changes to skills, agent prompts, reusable code snippets, and AGENTS.md.

**Core principle:** Evidence-based improvement. Every suggestion must be grounded in artifacts from the session, not speculation.

**Reporting principle:** Skills, agent prompts in `sgai/`, reusable snippets in `sgai/snippets/`, and `AGENTS.md` are co-equal reporting buckets. Do not treat `AGENTS.md` as the default sink for every process issue. Prefer `sgai/` overlay changes for process and tooling fixes, reserve the `Snippets` bucket for reusable language-specific code patterns, and reserve `AGENTS.md` for repository-level standing instructions. Every retrospective report must name all four buckets and explicitly say when a bucket has no warranted changes.

## When to Use

- Use when the retrospective agent starts (post-completion phase)
- Use when analyzing session artifacts to find improvement opportunities
- Don't use during normal workflow execution
- Don't use for modifying application source code

## IMPORTANT: Understanding `state.json` Paths

There are TWO different `state.json` files in the system:

1. **Session copy**: `.sgai/retrospectives/<session-id>/state.json` — A snapshot of the workflow state captured at session end. This file MAY NOT always exist (it depends on whether the session completed normally and the copy was made).
2. **Main workflow state**: `.sgai/state.json` — The live workflow state file. This file is ALWAYS present after the factory starts.

**Source selection logic (use this whenever a step says to "read state.json"):**
- First, try to read `.sgai/retrospectives/<session-id>/state.json` (the session copy)
- If it does not exist or is unreadable, fall back to `.sgai/state.json` (always present)
- If the session copy exists but looks partial, stale, or contradictory, also read `.sgai/state.json` and compare them before drawing conclusions
- Treat zero-message snapshots, implausibly low visit counts, or conflicts with `.sgai/PROJECT_MANAGEMENT.md` / session transcripts as stale-snapshot triggers
- Document whether your final analysis used the session copy, the live fallback, or both because of a mismatch

## SGAI_NOTES.md: Early and Persistent Writing

**CRITICAL:** You MUST write to `.sgai/SGAI_NOTES.md` EARLY and REPEATEDLY throughout the retrospective. This file records **internal factory operational notes** — how the factory could operate better, known internal issues, and agent patterns. It is distinct from AGENTS.md, which contains project-level instructions. It must be written incrementally so that partial analysis is preserved even if the retrospective is interrupted.

### SGAI_NOTES.md Format

Always APPEND (not replace) a new dated section to `.sgai/SGAI_NOTES.md`:

```markdown
## Factory Health Notes (YYYY-MM-DD)

### Status
[in-progress | complete]

### Known Issues
- [issue descriptions from this session]

### Agent Patterns
- [patterns observed across agents]

### Efficiency Suggestions
- [suggestions for improving factory efficiency]
```

### When to Write SGAI_NOTES.md

Write to `.sgai/SGAI_NOTES.md` at these specific moments — do NOT wait until the end:

1. **After Step 1a** (reading state.json): Write a "Status: in-progress" note with visit counts and message count
2. **After Step 1.5** (Mandatory Analysis Log): Update the note with per-category observations
3. **After Step 3** (Generate Suggestions): Update the note with the suggestion list
4. **After Step 6** (Apply Approved Changes): Update the note with "Status: complete" and final summary

**If the retrospective is interrupted between steps, the most recently written state persists in SGAI_NOTES.md.**

### How to Read Existing SGAI_NOTES.md

Before writing, always read the current `.sgai/SGAI_NOTES.md` to append without overwriting prior sessions:
```
1. Read .sgai/SGAI_NOTES.md (it may or may not exist)
2. If it exists, append your new dated section
3. If it does not exist, create it with just your new dated section
```

## Process

### Step 1: Artifact Discovery

Read artifacts in THIS ORDER (priority matters — richest signal sources first):

#### 1a. Read session `state.json` FIRST (highest priority)

- [ ] Read `.sgai/PROJECT_MANAGEMENT.md` frontmatter to find the retrospective session directory path (key: `Retrospective Session: .sgai/retrospectives/<session-id>`)
- [ ] List all files in the session directory
- [ ] **Read session `state.json` FIRST** — This is the single richest signal source. Use the source selection logic above: try `.sgai/retrospectives/<session-id>/state.json` first; if missing, fall back to `.sgai/state.json`; if the snapshot looks stale or contradictory, cross-check the live state too. It contains:
  - Visit counts per agent (how many times each agent ran)
  - Inter-agent message log (every message sent between agents)
  - Agent sequence (order of agent execution)
  - Progress notes from each agent
  - **If BOTH `.sgai/retrospectives/<session-id>/state.json` AND `.sgai/state.json` are missing or unreadable, STOP and report this in your analysis log — do NOT proceed to Step 2 without acknowledging this gap**
- [ ] Compare the session copy against `.sgai/PROJECT_MANAGEMENT.md` and the session JSON timeline. If the snapshot reports materially less activity than the rest of the artifacts, read live `.sgai/state.json`, treat the session copy as stale, and log the mismatch explicitly.
- [ ] **WRITE SGAI_NOTES.md NOW** — After reading state.json, write preliminary findings to `.sgai/SGAI_NOTES.md` with format:
  ```markdown
  ## Factory Health Notes (YYYY-MM-DD)

  ### Status
  in-progress

  ### Known Issues
  - [Any obvious issues seen in state.json so far]

  ### Agent Patterns
  - Visit counts: [agent: N visits, ...]
  - Message count: [N total inter-agent messages]

  ### Efficiency Suggestions
  - [Preliminary thoughts, to be refined]
  ```

#### 1b. Read Goal and Project Management artifacts

- [ ] Read `GOAL.md` copy (what was supposed to happen)
- [ ] Read `PROJECT_MANAGEMENT.md` copy (what actually happened — decisions, issues, backtracks)

#### 1c. Read ALL session JSON files (mandatory — no exceptions)

- [ ] Read **ALL** session JSON files (numbered `NNNN-<agent>-<timestamp>.json`) — these contain the full conversation transcripts
- [ ] You MUST read every single session JSON file in the directory, not just a subset
- [ ] Count the total number of session JSON files and track how many you have read
- [ ] **If there are more than 10 session JSONs, you may skim the later ones, but you MUST still open and read at least the first 500 lines of each one**

**Reading session JSONs:** Process files in numerical order (0001, 0002, ...). Each contains the full transcript for one agent iteration. Focus on:
- What the agent was asked to do
- What problems it encountered
- How long it took (number of tool calls)
- Whether it needed reviewer feedback or corrections
- Any patterns of rework, confusion, or inefficiency

#### 1d. Read logs

- [ ] Read `stdout.log` and `stderr.log` for build/test output signals

#### 1e. Read AGENTS.md

- [ ] Check if `AGENTS.md` exists in the repository root
- [ ] If present: Read the full content and parse it into individual rules/instructions (each bullet point, code block, or section heading constitutes a "rule")
- [ ] If missing: Record `AGENTS.md MISSING` as an artifact finding — this will trigger a creation proposal in Step 2.5
- [ ] Note the file size (line count) for the size evaluation in Step 2.5

**Session directory structure:**
```
.sgai/retrospectives/<session-id>/
├── GOAL.md
├── PROJECT_MANAGEMENT.md
├── state.json                        # Session copy (MAY NOT EXIST — use .sgai/state.json as fallback)
├── stdout.log
├── stderr.log
├── screenshots/
└── NNNN-<agent>-<timestamp>.json   # Session transcripts
```

### Step 1.5: Mandatory Analysis Log

**GATE: You MUST complete this step before proceeding to Step 2. You may NOT skip this step.**

After reading all artifacts in Step 1, write a structured analysis summary to your progress notes using `sgai_update_workflow_state`. This summary MUST contain:

1. **Files read count**: "Read X session JSONs out of Y total" (X must equal Y, or you must explain why not)
2. **Visit count summary**: From the effective state source used for analysis — session copy, live fallback, or both due to a mismatch — which agents ran and how many visits each
3. **Message count summary**: From the same effective state source(s) — total inter-agent messages, notable message patterns, and whether any stale-snapshot mismatch was detected
4. **Per-category observations** (at least 1 observation per category):
   - **Efficiency**: At least 1 observation about visit counts, handoff patterns, or iteration depth
   - **Quality**: At least 1 observation about reviewer feedback, test failures, or backtracks
   - **Knowledge gaps**: At least 1 observation about missing information, repeated mistakes, or tool misuse
   - **Process gaps**: At least 1 observation about missing skills, skill violations, or convention drift
   - **AGENTS.md Health**: At least 1 observation about AGENTS.md existence, rule relevance, contradictions with session behavior, or file size/structure

**If you cannot produce at least 1 observation per category:**
- You MUST re-read the session artifacts more carefully
- Focus on the session JSONs — patterns are buried in the transcripts, not in summary artifacts
- A "clean" session (all goals complete, tests passing) does NOT mean there are no observations. Every session has patterns worth noting.
- If after a second careful read you still have a category with no observations, you may write "No observations found after thorough review" for that specific category — but this should be rare

**Example analysis log:**
```
Analysis Summary:
- Files: Read 15/15 session JSONs, state.json from both `.sgai/retrospectives/<session-id>/state.json` and `.sgai/state.json` because the session copy looked stale, GOAL.md, PROJECT_MANAGEMENT.md, stdout.log, stderr.log
- Visits: coordinator(8), go-developer(3), go-reviewer(2), react-developer(1), project-critic-council(1)
- Messages: 19 inter-agent messages, 3 reviewer feedback rounds
- Efficiency: Backend developer visited 3 times due to reviewer feedback — could skills reduce this?
- Quality: Reviewer caught SQL formatting issues 3 times — suggests missing skill
- Knowledge gaps: Agent asked about migration workflow mid-session — no skill exists for this
- Process gaps: stpa-analyst.md was a 17-line stub that needed emergency expansion in-session
- AGENTS.md Health: AGENTS.md present (53 lines), 2 rules appear stale (no React code was modified but 4 React-specific rules exist), no contradictions detected
```

**After completing the analysis log, UPDATE `.sgai/SGAI_NOTES.md`** with the per-category observations:
```markdown
### Agent Patterns
- [Updated with observations from analysis log]

### Known Issues
- [Updated with quality and process gap observations]
```

### Step 2: Pattern Analysis

Analyze the artifacts for these signal types:

#### Efficiency Signals
- [ ] **Visit counts** — From session `state.json` (or `.sgai/state.json` fallback), how many times was each agent visited? High counts suggest rework or unclear instructions.
- [ ] **Handoff patterns** — Were there unnecessary back-and-forth between agents? Could some handoffs have been avoided?
- [ ] **Iteration depth** — How many iterations did the workflow take? What drove the iteration count?

#### Quality Signals
- [ ] **Reviewer feedback** — What did reviewers (go-reviewer, react-reviewer) find? Are there patterns in what they catch?
- [ ] **Test failures** — Were there build or test failures? What caused them?
- [ ] **Backtracks** — Did any agent need to undo or redo work? Why?

#### Knowledge Gaps
- [ ] **Missing information** — Did agents ask questions that should have been pre-documented in skills or AGENTS.md?
- [ ] **Repeated mistakes** — Did multiple agents make the same kind of error?
- [ ] **Tool misuse** — Did agents use tools incorrectly or inefficiently?

#### Process Gaps
- [ ] **Missing skills** — Were there situations where a skill would have helped but none existed?
- [ ] **Skill violations** — Did agents ignore or misapply existing skills?
- [ ] **Convention drift** — Were there style or convention inconsistencies that AGENTS.md should address?

### Step 2.5: AGENTS.md Health Analysis

**GATE: This step is MANDATORY. You may NOT skip it, even if other analysis steps produced sufficient findings.**

Analyze AGENTS.md health across five dimensions:

#### 2.5a. Existence Check
- [ ] Is AGENTS.md present in the repository root?
- [ ] If MISSING: Flag for creation proposal. During Step 3, you MUST propose creating AGENTS.md pre-populated with patterns observed from the session (style rules, conventions, recurring corrections the human made).

#### 2.5b. Rule Extraction
- [ ] Parse AGENTS.md into individual rules/instructions
- [ ] Categorize each rule (style rule, build instruction, convention, terminology, etc.)
- [ ] Count total rules and note logical groupings

#### 2.5c. Contradiction Scan
- [ ] Cross-reference each AGENTS.md rule against the session transcript behavior
- [ ] **Direct contradictions**: Did the human explicitly ask for something that AGENTS.md forbids? (e.g., AGENTS.md says "no inline comments" but human asked agent to "add a comment here")
- [ ] **Override patterns**: Did agents consistently ignore a rule without the human correcting them? This suggests the rule may be outdated.
- [ ] **Implicit tensions**: Did the human's requests reveal preferences that conflict with existing rules, even if not directly?
- [ ] For each finding, record: the rule, the contradicting evidence (session JSON file + approximate location), and whether the rule should be updated or removed

#### 2.5d. Staleness Detection
- [ ] Identify rules that reference tools, patterns, or technologies not used in the session
- [ ] Identify rules that were never relevant to any agent's work during the session
- [ ] Cross-reference with recent sessions (if `.sgai/SGAI_NOTES.md` exists, check for historical patterns)
- [ ] A rule being "not relevant to this session" alone does NOT make it stale — it must show a pattern of irrelevance or reference something that no longer exists

#### 2.5e. Size & Structure Evaluation
- [ ] Count total lines in AGENTS.md
- [ ] If over 100 lines: evaluate whether the file has become unwieldy and could benefit from restructuring
- [ ] Identify logical groupings of rules (e.g., Go style rules, React conventions, test instructions, terminology)
- [ ] If 3+ distinct groupings exist and the file exceeds 100 lines: propose splitting into separate files (e.g., `AGENTS-go.md`, `AGENTS-react.md`)
- [ ] For any proposed split: describe which rules go into which file and provide the proposed filenames
- [ ] Identify rules that could be removed entirely (redundant, superseded by skills, or outdated)

### Step 3: Generate Suggestions

For each pattern identified in Step 2 and Step 2.5, produce a concrete suggestion. Each suggestion must have:

1. **Category** — One of: `new-skill`, `modify-skill`, `new-agent-prompt`, `modify-agent-prompt`, `new-snippet`, `modify-snippet`, `update-agents-md`, `create-agents-md`, `restructure-agents-md`
2. **Evidence** — The specific artifact and pattern that motivated it
3. **Proposal** — What to create or change (be specific)
4. **Rationale** — Why this improvement will help future sessions
5. **Diff Preview** — For suggestions that modify existing files, include the unified diff showing what will change (read the file first, then compute the diff). For new files, show the proposed content.
6. **Bucket** — Exactly one of: `Skills`, `Agent Prompts`, `Snippets`, or `AGENTS.md`. Use this to keep the final report explicitly separated by bucket.

Before writing a proposal, decide whether the evidence points to a skill, an agent prompt in `sgai/`, a reusable snippet in `sgai/snippets/`, or `AGENTS.md`. Treat those targets as co-equal categories. If the issue is about factory process, tooling workflow, or agent behavior, prefer a `sgai/` skill or agent prompt change unless the evidence shows it truly belongs in repository-wide standing instructions. If the issue is a reusable language-specific code pattern that multiple future agents could start from, prefer the `Snippets` bucket.

**After generating suggestions, UPDATE `.sgai/SGAI_NOTES.md`** with the suggestion list:
```markdown
### Efficiency Suggestions
- [List of suggestions from Step 3, even before human approval]
```

#### Path Validation Rule

Before presenting any suggestion, verify the target path:
- Target path MUST NOT be under `.sgai/` (except `.sgai/SGAI_NOTES.md`)
- If a suggestion targets `.sgai/`, translate it to the `sgai/` overlay equivalent:
  - `.sgai/agent/foo.md` -> `sgai/agent/foo.md`
  - `.sgai/skills/bar/SKILL.md` -> `sgai/skills/bar/SKILL.md`
- The `.sgai/` directory is the runtime directory rebuilt from skeleton + overlay on every startup — changes there are lost immediately

#### Snippet Bucket Rule

Use the `Snippets` bucket only for reusable code artifacts that future agents should be able to discover through `sgai_find_snippets(language, query)`.

- Store snippet proposals under `sgai/snippets/<language>/`
- Use a stable, queryable filename stem because `sgai_find_snippets` does exact stem lookup first, then substring and description matching
- Include frontmatter `description` so list and fuzzy search results are meaningful
- Prefer snippets for reusable code patterns, helpers, or templates; do not use snippets for prose workflow rules better expressed as a skill or agent prompt

#### Suggestion Categories

**New Skills** (`sgai/skills/<name>/SKILL.md`)
- Agents repeatedly needed guidance that no skill provides
- A process pattern emerged that should be standardized
- Example: "Create a skill for database migration testing — agents spent 3 iterations figuring out the migration workflow"

**Modify Existing Skills** (`sgai/skills/<name>/SKILL.md`)
- An existing skill was unclear or incomplete
- Agents violated a skill due to ambiguity
- Example: "Add a section to go-code-review about SQL formatting — reviewer flagged this 3 times"

**New/Modified Agent Prompts** (`sgai/agent/<name>.md`)
- An agent's behavior needs adjustment
- An agent's permissions were too broad or too narrow
- Example: "Add explicit instruction to go-developer about running make lint before marking done"

**New/Modified Snippets** (`sgai/snippets/<language>/<name>.<ext>`)
- A session exposed a reusable code pattern that future agents should start from instead of rewriting
- The asset is primarily code, not policy or workflow guidance
- The snippet should be discoverable by language plus filename stem or description via `sgai_find_snippets`
- Example: "Add a Go snippet for version-control diff retrieval with jj-first fallback because multiple agents had to recreate it"

**Update AGENTS.md**
- A repository-level standing instruction emerged from reviewer feedback or human clarification
- A business rule was discovered during brainstorming and should persist across the repository
- A convention was established mid-session that belongs in repository-level standing guidance rather than a skill or agent prompt
- Do NOT use this bucket for process or tooling fixes when a skill or agent prompt in `sgai/` is the better fit
- Example: "Add rule: Go error variable names must use err prefix pattern (errClose, errRead)"

**AGENTS.md-specific suggestion types (from Step 2.5):**

| Trigger | Suggestion Type | What to Propose |
|---------|----------------|-----------------|
| AGENTS.md missing | `create-agents-md` | Create AGENTS.md pre-populated with observed session patterns. Extract style rules, conventions, and recurring human corrections into a structured initial file. |
| Contradiction found | `update-agents-md` | Either update the contradicted rule to match the human's new preference, or remove it if clearly outdated. Always cite the session evidence. |
| Stale rule found | `update-agents-md` | Propose removal of the stale rule with rationale explaining why it's no longer relevant. |
| File too large | `restructure-agents-md` | Propose splitting AGENTS.md into multiple files. Specify which rules go where and provide proposed filenames. |
| Override pattern found | `update-agents-md` | Propose either relaxing the rule or removing it, based on the pattern of agents/humans consistently ignoring it. |

### Step 4: Prioritize and Group

- [ ] Sort suggestions by impact (high/medium/low)
- [ ] Group suggestions into exactly 4 category buckets:
  - **Skills** — Categories `new-skill` and `modify-skill`
  - **Agent Prompts** — Categories `new-agent-prompt` and `modify-agent-prompt`
  - **Snippets** — Categories `new-snippet` and `modify-snippet`
  - **AGENTS.md** — Categories `update-agents-md`, `create-agents-md`, and `restructure-agents-md`
- [ ] Build a bucket summary that names all 4 buckets, even when one or more buckets have zero warranted changes
- [ ] Explicitly mark empty buckets as `no warranted changes` instead of omitting them
- [ ] Discard suggestions that are too vague or not actionable
- [ ] Limit to the top 10 most impactful suggestions (quality over quantity)

**Impact assessment:**
| Impact Level | Criteria |
|-------------|----------|
| **High** | Would have saved 3+ iterations or prevented a major backtrack |
| **Medium** | Would have improved clarity or reduced one review round |
| **Low** | Nice-to-have, minor improvement |

### No Suggestions Case

**PREREQUISITES — You may NOT invoke this case unless ALL of the following are true:**

1. You have read the session `state.json` (via `.sgai/retrospectives/<session-id>/state.json`, or the `.sgai/state.json` fallback) and recorded visit counts and message counts in your analysis log
2. You have read at least 3 session JSON files (or all of them if fewer than 3 exist)
3. You have completed the Step 1.5 Mandatory Analysis Log with observations in all 5 signal categories (efficiency, quality, knowledge gaps, process gaps, AGENTS.md health)
4. You have completed Steps 2-4 (Pattern Analysis, AGENTS.md Health Analysis, Generate Suggestions, Prioritize and Group)

**If ALL prerequisites are met** and you genuinely have zero actionable suggestions after thorough analysis, send `RETRO_COMPLETE` and exit:

```
sgai_send_message({
  toAgent: "coordinator",
  body: "RETRO_COMPLETE: No actionable improvements identified for this session. Bucket summary: Skills - no warranted changes; Agent Prompts - no warranted changes; Snippets - no warranted changes; AGENTS.md - no warranted changes. Analysis summary: Read X/Y session JSONs, session state.json (from [path used]) showed Z agent visits and W messages. Per-category findings: [brief summary of each category observation from Step 1.5]."
})
sgai_update_workflow_state({ status: "agent-done", task: "", addProgress: "No actionable suggestions found after thorough analysis. Sent RETRO_COMPLETE." })
// STOP HERE. Make NO more tool calls. Your turn is OVER.
// This means: no check_inbox, no check_outbox, no file reads, no file writes, no bash, NOTHING.
// Extra tool calls cause system deadlock requiring manual SIGTERM.
```

**CRITICAL:** The RETRO_COMPLETE message MUST include your analysis summary (files read, visit counts, per-category observations) as proof that you actually completed the analysis. A bare "No actionable improvements identified" without evidence is NOT acceptable.

### Step 5: Present Changes for Approval

**MANDATORY YIELD PROTOCOL:** After every `sgai_send_message()` call in this step, you MUST:
1. Immediately call `sgai_update_workflow_state({status: "agent-done"})`
2. STOP making tool calls — your turn is over
3. Do NOT call `check_inbox` or `check_outbox` — the coordinator cannot respond until you yield

**MANDATORY:** You MUST send at least one `RETRO_QUESTION:` message to the coordinator during your run. This is NOT optional. If you found zero suggestions, follow the "No Suggestions Case" above instead.

Present proposed changes to the human partner by sending `RETRO_QUESTION:` messages to the coordinator, grouped by category. Before sending the first approval request, prepare a bucket summary that lists Skills, Agent Prompts, Snippets, and `AGENTS.md`, including any buckets with `no warranted changes`. For each non-empty category bucket (Skills, Agent Prompts, Snippets, `AGENTS.md`), send ONE message containing ALL proposals in that category, and include the full bucket summary in the message so empty buckets are still reported explicitly.

#### Presentation Format

For each non-empty category, send a single `RETRO_QUESTION` with this structure:

```
sgai_send_message({
  toAgent: "coordinator",
  body: "RETRO_QUESTION [MULTI-SELECT]: **Skills Changes** (N proposals)\n\nBucket Summary:\n- Skills: N proposal(s)\n- Agent Prompts: [M proposal(s) OR no warranted changes]\n- Snippets: [P proposal(s) OR no warranted changes]\n- AGENTS.md: [K proposal(s) OR no warranted changes]\n\n### 1. [Title of first proposal]\nEvidence: [1-line evidence from session artifacts]\n```diff\n--- a/[file path]\n+++ b/[file path]\n@@ ... @@\n[unified diff content]\n```\nRationale: [why this helps future sessions]\n\n### 2. [Title of second proposal]\nEvidence: [1-line evidence]\n[full proposed file content for new files, or diff for modifications]\nRationale: [why this helps]\n\nSelect which to approve (multi-select):\n- 1. [Title of first proposal]\n- 2. [Title of second proposal]"
})
// Then yield immediately
sgai_update_workflow_state({ status: "agent-done", task: "Waiting for human response via coordinator", addProgress: "Sent Skills category RETRO_QUESTION to coordinator" })
// STOP HERE. Make NO more tool calls. Do NOT check inbox or outbox. Your turn is OVER.
// This means: no check_inbox, no check_outbox, no file reads, no file writes, no bash, NOTHING.
// Extra tool calls cause system deadlock requiring manual SIGTERM.
```

#### Full Example

```
sgai_send_message({
  toAgent: "coordinator",
  body: "RETRO_QUESTION [MULTI-SELECT]: **Skills Changes** (2 proposals)\n\nBucket Summary:\n- Skills: 2 proposal(s)\n- Agent Prompts: no warranted changes\n- Snippets: 1 proposal(s)\n- AGENTS.md: 1 proposal(s)\n\n### 1. Add SQL formatting section to go-code-review\nEvidence: Reviewer flagged SQL formatting 3 times in session\n```diff\n--- a/sgai/skills/go-code-review/SKILL.md\n+++ b/sgai/skills/go-code-review/SKILL.md\n@@ -45,6 +45,12 @@\n+## SQL Formatting\n+- Align VALUES with INSERT columns\n+- Each column on its own line\n```\nRationale: Prevents repeated reviewer catches\n\n### 2. Create db-migration-testing skill\n[full proposed file content]\nRationale: Standardizes migration testing workflow\n\nSelect which to approve (multi-select):\n- 1. Add SQL formatting section to go-code-review\n- 2. Create db-migration-testing skill"
})
sgai_update_workflow_state({ status: "agent-done", task: "Waiting for human response via coordinator", addProgress: "Sent Skills RETRO_QUESTION to coordinator" })
// STOP HERE. Your turn is OVER.
// This means: no check_inbox, no check_outbox, no file reads, no file writes, no bash, NOTHING.
// Extra tool calls cause system deadlock requiring manual SIGTERM.
```

#### Processing Responses

When the coordinator relays the human's response (which numbered items were selected):
- Track which individual changes were approved vs rejected
- Proceed to the next non-empty category with another `RETRO_QUESTION [MULTI-SELECT]:`
- After all categories have been presented, proceed to Step 6

#### Key Presentation Rules

1. **One message per category** — Never send individual proposals one-at-a-time
2. **Include `[MULTI-SELECT]` marker** — So the coordinator knows to use `multiSelect: true` when relaying
3. **Include the full bucket summary every time** — Every approval message must name Skills, Agent Prompts, Snippets, and `AGENTS.md`, explicitly marking any empty bucket as `no warranted changes`
4. **Show diffs for ALL file modifications** — When suggesting changes to existing files, you MUST read the file first and show the unified diff. For new files, show the full proposed content.
5. **Respect rejections** — If user deselects a proposal, do NOT re-present it
6. **Respect "skip all"** — If user selects nothing in a category, that entire category is skipped
7. **Yield after every send** — The IRON LAW applies here without exception

### Step 6: Apply Approved Changes

Apply only the individually-approved changes. Skip any rejected or unselected changes entirely.

#### Overlay Directory Workflow

The `sgai/` directory is an **overlay** — files placed there wholly replace their skeleton defaults. This has critical implications for how you apply changes:

**For MODIFYING an existing skill, agent prompt, or snippet:**
1. READ the current version from `.sgai/` (the live runtime directory — skeleton + overlay merged)
2. Copy the ENTIRE file content into memory
3. Apply your modifications to the copy
4. Write the COMPLETE modified file to `sgai/` (the overlay directory)

**For CREATING a new skill, agent prompt, or snippet:**
1. Write the entire new file directly to `sgai/`

**CRITICAL:** Partial edits are NOT possible via the overlay. Every file in `sgai/` must be a complete, self-contained version of the file it overrides. If you write only your changes without the rest of the file content, the original content will be lost.

#### Checklist

For each approved change:

- [ ] For modifications: READ the current file from `.sgai/` first, then write the COMPLETE modified version to `sgai/`
- [ ] For new files: Write the entire file directly to `sgai/`
- [ ] Write the change to the correct location (`sgai/` overlay or `AGENTS.md`)
- [ ] For new skills: Create proper `SKILL.md` with YAML frontmatter (name, description)
- [ ] For AGENTS.md changes: Append to the appropriate section, don't overwrite existing content
- [ ] For agent prompt modifications: Use the `sgai/agent/` overlay directory
- [ ] Verify each written file is syntactically valid

#### Writing Skills

New skills must follow the Agent Skills spec:
```markdown
---
name: <skill-name>
description: <when to use this skill>
---

# <Skill Title>

## Overview
[What this skill is for]

## When to Use
[Trigger conditions]

## Process
[Step-by-step guide]
```

#### Writing to AGENTS.md

When adding to AGENTS.md:
1. Read the current AGENTS.md first
2. Find the appropriate section (or create one)
3. Add new content without removing existing content
4. Use the same formatting style as existing entries

### Step 7: Completion

- [ ] **FINAL SGAI_NOTES.md UPDATE**: Write a "Status: complete" update to `.sgai/SGAI_NOTES.md` with:
  - Total approved changes applied
  - Summary of each approved change
  - Any issues encountered during application
- [ ] Verify all approved changes were written successfully
- [ ] Summarize what was changed in the workflow state progress log
- [ ] Set status to `agent-done`

## Rules

1. **Evidence-based only** — Every suggestion must reference specific artifacts. No speculation.
2. **User approval required** — Never write changes without explicit human approval via `RETRO_QUESTION:` messages to the coordinator.
3. **sgai/ and AGENTS.md only** — Only modify files in the `sgai/` overlay directory and `AGENTS.md`. Nothing else.
4. **Read everything** — Read ALL session artifacts before producing suggestions. Incomplete analysis produces bad suggestions.
5. **Quality over quantity** — Limit to 10 suggestions max. Better to have 3 great suggestions than 10 mediocre ones.
6. **Graceful exit** — If the user rejects everything during the approval process, mark done without making changes. That is a valid outcome. But you MUST have sent at least one `RETRO_QUESTION:` message to the coordinator before exiting (or a `RETRO_COMPLETE` in the no-suggestions case).
7. **No source code** — You do not modify Go, TypeScript, test files, or any application code. Period.
8. **No `.sgai/` suggestions** — Never suggest changes targeting `.sgai/` paths (except `.sgai/SGAI_NOTES.md`). Always translate to `sgai/` overlay equivalents. The `.sgai/` directory is rebuilt from skeleton + overlay on every startup — changes there are lost.
9. **Do not default to AGENTS.md** — Treat Skills, Agent Prompts, Snippets, and `AGENTS.md` as co-equal categories. Prefer `sgai/` skill or agent-prompt changes for process and tooling fixes, prefer snippets for reusable language-specific code patterns, and use `AGENTS.md` only when the evidence clearly calls for a repository-level standing rule.
10. **Report empty buckets explicitly** — Every retrospective report must name all 4 buckets and say `no warranted changes` for empty ones instead of omitting them.
11. **Mandatory analysis log** — You MUST complete Step 1.5 before proceeding to Step 2. Skipping the analysis log is a skill violation.
12. **AGENTS.md analysis is mandatory** — Every retrospective MUST include Step 2.5 (AGENTS.md Health Analysis). Even if AGENTS.md looks fine, you must document that assessment. "AGENTS.md looks fine" is not acceptable without evidence of reading it. AGENTS.md analysis does NOT imply AGENTS.md changes are warranted.
13. **SGAI_NOTES.md is incremental** — Write to `.sgai/SGAI_NOTES.md` early and often. Do NOT wait until the end. Preliminary findings must be written after Step 1a, updated after Step 1.5, updated after Step 3, and finalized after Step 6.

## Red Flags - STOP

- "I'll just put this in AGENTS.md"
- "I don't need to mention empty buckets because there are no proposals there"
- "Skills and agent prompts are basically the same bucket"
- "A reusable code example can just live inside a skill diff"
- "Process guidance belongs in AGENTS.md by default"

## Rationalization Table

| Excuse | Reality |
|--------|---------|
| "I found one AGENTS.md idea, so that should be the main recommendation" | AGENTS.md is only one bucket. Skills and agent prompts are co-equal categories and may be the better fit for the evidence. |
| "Process and tooling fixes are repo-wide, so they belong in AGENTS.md" | Default those fixes to `sgai/` skills or agent prompts. Use `AGENTS.md` only for repository-level standing instructions. |
| "A reusable code pattern should just be described in prose" | Reusable code belongs in the `Snippets` bucket when future agents should discover it through `sgai_find_snippets(language, query)`. |
| "No proposals in a bucket means I can skip mentioning it" | Empty buckets must still be reported as `no warranted changes`. |
| "Mandatory AGENTS.md analysis means mandatory AGENTS.md edits" | Analysis is mandatory; edits are evidence-driven and may be zero. |

### Common Rationalizations to REJECT
- "Step 2.5 is about AGENTS.md, so I should probably propose an AGENTS.md edit" — NO. AGENTS.md analysis is mandatory, but AGENTS.md changes still require evidence. Empty AGENTS.md bucket is a valid outcome.
- "This process fix is global, so AGENTS.md is simpler" — NO. If it is a factory-process or tooling fix, prefer a skill or agent prompt in `sgai/` unless it is truly a repository-level standing instruction.
- "This reusable code pattern can stay inside a skill example" — NO. If future agents should search for and reuse the code directly, propose a snippet in `sgai/snippets/<language>/`.
- "Only Skills had proposals, so I can omit the other buckets" — NO. Every retrospective report must still name Agent Prompts and `AGENTS.md`, and explicitly mark empty buckets as `no warranted changes`.
- "I'll suggest modifying `.sgai/agent/foo.md` directly" — NO. Always target `sgai/agent/foo.md` (overlay).
- "I'll suggest changes to `.sgai/skills/bar/SKILL.md`" — NO. Target `sgai/skills/bar/SKILL.md` instead.
- "The `.sgai/` path is where the file currently lives" — Irrelevant. You READ from `.sgai/`, but SUGGEST and WRITE to `sgai/`.
- "Everything looks clean, no need to dig deeper" — NO. Clean-looking sessions often have the most interesting buried patterns. Every session has patterns worth noting, even successful ones.
- "The session was successful so there's nothing to improve" — NO. Success does not mean there are no improvement opportunities. Dig into the transcripts.
- "I've read enough to conclude there are no suggestions" — NO, unless you have met ALL prerequisites for the No Suggestions Case (session `state.json` read via fallback logic, 3+ session JSONs read, analysis log complete with all 5 categories).
- "I'll write SGAI_NOTES.md at the end" — NO. Write it EARLY (after Step 1a) and update it throughout. The whole point is that partial analysis is preserved if the retrospective is interrupted.
- "AGENTS.md looks fine, I'll skip the health analysis" — NO. Step 2.5 is mandatory. You must analyze all 5 dimensions and document your findings even if AGENTS.md appears healthy.

## Checklist

Before marking done, verify:

- [ ] Read session `state.json` FIRST (tried `.sgai/retrospectives/<session-id>/state.json`, fell back to `.sgai/state.json` if needed, and cross-checked live state when the snapshot looked stale) and recorded visit counts + message counts
- [ ] Wrote preliminary findings to `.sgai/SGAI_NOTES.md` immediately after Step 1a
- [ ] Read ALL session JSON files (count: X out of Y total)
- [ ] Completed Step 1.5 Mandatory Analysis Log with observations in all 5 categories
- [ ] Updated `.sgai/SGAI_NOTES.md` after Step 1.5 with per-category observations
- [ ] Read all session artifacts (GOAL.md, PM, session `state.json` via fallback logic, session JSONs, stdout.log, stderr.log)
- [ ] Read AGENTS.md (or noted its absence) during Step 1e
- [ ] Completed Step 2.5 (AGENTS.md Health Analysis) with all 5 dimensions checked
- [ ] Included at least one AGENTS.md Health observation in the Step 1.5 analysis log
- [ ] Identified patterns from at least 2 signal categories (efficiency, quality, knowledge, process)
- [ ] Produced concrete suggestions with evidence, diffs, and rationale
- [ ] Updated `.sgai/SGAI_NOTES.md` after Step 3 with suggestion list
- [ ] Grouped suggestions into category buckets (Skills, Agent Prompts, Snippets, AGENTS.md)
- [ ] Reported all 4 buckets explicitly, including any `no warranted changes` buckets
- [ ] Preferred `sgai/` skill or agent-prompt suggestions for process/tooling fixes, and snippet suggestions for reusable code patterns, unless evidence required `AGENTS.md`
- [ ] Sent at least one `RETRO_QUESTION [MULTI-SELECT]:` message per non-empty category to the coordinator (or `RETRO_COMPLETE` if zero suggestions)
- [ ] Applied only individually-approved changes; skipped all rejected changes
- [ ] Applied changes to correct locations (sgai/ overlay or AGENTS.md)
- [ ] Updated `.sgai/SGAI_NOTES.md` with "Status: complete" after Step 6
- [ ] Set workflow state to agent-done
- [ ] After EVERY sgai_send_message() call, immediately called sgai_update_workflow_state({status: "agent-done"}) and stopped
- [ ] Never called check_inbox or check_outbox between sending a message and yielding
