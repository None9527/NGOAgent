package prompttext

// ═══════════════════════════════════════════
// Ephemeral injection messages
// These are injected into the system prompt per-turn based on runtime state.
// They are NOT part of the static framework prompt.
// ═══════════════════════════════════════════

// --- Agentic mode ---

const EphAgenticMode = `🤖 [AGENTIC MODE] Fully autonomous. Complete decision authority.
- Complex tasks: plan first (task_boundary + implementation_plan) → self-review → execute.
- Simple tasks: proceed directly.
- No user approval needed for plans.
- High thoroughness required. Use spawn_agent for 3+ components.
Trust your own judgment on when to plan.`

// --- Task progress reminders ---

const EphActiveTaskReminder = `Remember to update the task as appropriate. The current task is:
task_name:"{{.TaskName}}" status:"{{.Status}}" summary:"{{.Summary}}" mode:{{.Mode}}

Update task_boundary around once every 3-4 tools. Status should describe NEXT STEPS, not previous steps.`

const EphArtifactReminder = `You have created the following artifacts so far:
{{.ArtifactList}}

CRITICAL REMINDER: remember that artifacts should be AS CONCISE AS POSSIBLE.
- plan.md is a DESIGN DOCUMENT (component groups + [MODIFY]/[NEW] file paths), NOT a checklist.
- task.md is a PROGRESS CHECKLIST ([x]/[/]/[ ] with specific file names).
- Do not confuse the two.`

// --- Planning mode ---

const EphPlanningMode = `### 📝 Planning Mode Protocol
**Workflow:**
1. **PLANNING**: task_boundary(planning) → research → task_plan(plan) → notify_user(blocked=true) → STOP
2. **EXECUTION**: task_boundary(execution) → task_plan(task) → implement → update task.md
3. **VERIFICATION**: task_boundary(verification) → test → task_plan(walkthrough)

**Mandatory Rules:**
- First tool call MUST be task_boundary(mode="planning").
- Read relevant SKILL.md/MCP docs BEFORE planning if they match the task.
- NEVER write code (edit/write_file) before plan is approved.
- NO sub-agents during PLANNING. Allowed during EXECUTION only.
- User approvals: "ok/yes/approve" → EXECUTION; "no/reject" → CANCEL; feedback → REVISE.

**plan.md Requirements:**
- Group files by component; use '[NEW/MODIFY/DELETE] [file](file:///path)'.
- Verification section: absolute paths + exact commands (read build/test files first).`

// EphPlanningNoPlanReminder — injected when in planning mode but plan.md not yet created.
// COMPRESSED: format requirements merged into EphPlanningMode. This reminder only adds the
// skill-check rule and the exact notify_user call pattern.
const EphPlanningNoPlanReminder = `Reminder: you are in planning mode but no plan.md exists yet.

Before writing a plan:
1. Check if any of your available Skills or MCP tools match this task. If yes, read its SKILL.md FIRST.
2. Search/list/view files to understand existing code and architecture.

Then create plan.md via task_plan(action=create, type=plan) and call notify_user to pause for approval.`

const EphPlanModifiedReminder = `You have modified plan.md during this task. Before switching to execution mode,
call notify_user(message="计划已更新，请审阅", paths_to_review=["plan.md path"], blocked_on_user=true)
to pause and wait for the user to review and approve the changes.`

// EphEnteringPlanningMode — DEPRECATED. Content is vague and redundant with EphPlanningMode.
// Kept as a constant to avoid compile errors but should NOT be injected by prepare.go.
// TODO: remove after confirming no callers.
const EphEnteringPlanningMode = `Now entering planning mode. Follow the planning workflow in the system prompt.`

const EphExitingPlanningMode = `Now that you are exiting planning mode, you MUST transition to EXECUTION mode.

IMMEDIATE REQUIRED ACTIONS:
1. Call task_boundary(mode="execution") to switch mode.
2. IMMEDIATELY create task.md via task_plan(action=create, type=task) with a progress checklist using [x]/[/]/[ ] markers.
3. Implement changes, updating task.md via task_plan(action=update, type=task) as items complete.
4. Upon completion, switch to VERIFICATION mode via task_boundary(mode="verification").
5. After tests pass, if plan.md was created, create walkthrough.md via task_plan(action=create, type=walkthrough) summarizing changes and test results.

FAILURE TO CREATE task.md IS A CRITICAL ERROR. Walkthrough is only required for planned tasks.`

// --- Context management ---

const EphContextStatus = `Context window usage: {{.Percent}}% ({{.Used}}/{{.Total}} tokens).
{{if ge .Percent 80}}WARNING: Context is running low. Be concise and focused.{{end}}`

const EphCompactionNotice = `Context has been compacted to fit within limits. A summary of the conversation so far has been preserved. You may need to re-read files if you need their exact contents.`

// --- Security / validation ---

const EphEditValidation = `The previous edit_file operation failed with error: {{.Error}}
File: {{.FilePath}}
Please review and fix the edit parameters.`

const EphSecurityNotice = `Tool call "{{.ToolName}}" was denied by security policy.
Reason: {{.Reason}}
You may need to ask the user for permission or use a different approach.`

// --- Skills ---

const EphSkillInstruction = `Skill instruction loaded: {{.SkillName}}

{{.Content}}`

// --- Agentic ---

const EphAgenticSelfReview = `🤖 [AGENTIC MODE] You have created an execution plan. Review it yourself for completeness and correctness. If satisfactory, proceed with execution immediately. If issues found, revise the plan first then execute. Do NOT wait for user approval.`
