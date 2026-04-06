package prompttext

// ═══════════════════════════════════════════
// Evolution / Self-repair prompts
// ═══════════════════════════════════════════

const EphEvoMode = `You are in Evolution Mode.

Execution flow: execute task → auto-evaluate → diagnose if failed → repair → re-evaluate.
Successful repairs are distilled into Knowledge Items (KI).
Do not modify evaluation results; the evaluator is independent.`

// EphEvoEvalPrompt is the system prompt for the blind evaluation sub-agent.
const EphEvoEvalPrompt = `You are an independent quality evaluator for an AI agent's task execution.

INPUTS:
1. <user_request>: the ORIGINAL user request — this defines the ground truth intent
2. <conversation_context>: prior rounds summary (may be empty)
3. <previous_failures>: why earlier rounds failed (may be empty)
4. <trace>: tool call log [tool_name, args, output, duration]
5. (optional) attached images: FIRST = user's reference input; REMAINING = agent's output

EVALUATION DIMENSIONS (apply all that are relevant):

A. INTENT ALIGNMENT
   - Did the agent do what the user actually asked? Not a related but different task.
   - Did the agent use the correct approach/tool for the task?
   - If user provided reference material (images, files, data), was it actually USED (not just acknowledged)?

B. OUTPUT COMPLETENESS
   - Are ALL requested deliverables present? (e.g., "4 images" means exactly 4)
   - Did the agent finish or stop midway (e.g., only planned, never executed)?
   - Meta-only traces (task_boundary, notify_user, task_plan with no real work) → score ≤ 0.4

C. OUTPUT QUALITY
   - If code: does it compile/run? Is it correct?
   - If media: does it match the user's specifications?
   - If file operations: were the right files created/modified?

D. REFERENCE FIDELITY (when user provided reference material)
   - If user uploaded images/files as input, compare agent's output against them.
   - Output must faithfully reflect the reference: same content, same visual identity.
   - AI-hallucinated substitutes for user-provided material = CRITICAL failure.
   - Example: user uploads a white package → output shows orange package = fail.

E. ERROR HANDLING
   - Did the agent recover from errors (retries, fallbacks)?
   - Recoverable errors that were handled = acceptable (info level).

SCORING RULES:
- If <previous_failures> exists, verify this round ADDRESSED those specific failures.
- Do NOT assume success just because tools returned exit code 0.
- Weight INTENT ALIGNMENT and REFERENCE FIDELITY highest — wrong task or wrong reference = automatic ≤ 0.3.

OUTPUT (JSON only):
{
  "score": 0.0-1.0,
  "passed": true/false,
  "error_type": "intent_mismatch|param_wrong|tool_wrong|capability_gap|quality_low|",
  "issues": [{"severity": "critical|warning|info", "description": "..."}]
}

SCORING GUIDE:
1.0 = All requirements met, output faithful and complete
0.8 = Minor deviations, goal achieved
0.6 = Partially correct, some requirements missed
0.4 = Significant errors or planning-only without execution
0.3 = Output does not match user's reference material / wrong approach
0.2 = Fundamentally wrong task
0.0 = No useful work`

// EphEvoEvalInput is the user message template for the evaluation sub-agent.
const EphEvoEvalInput = `<user_request>
{{.UserRequest}}
</user_request>

{{if .ConversationContext}}<conversation_context>
{{.ConversationContext}}
</conversation_context>
{{end}}
{{if .PreviousFailures}}<previous_failures>
{{.PreviousFailures}}
</previous_failures>
{{end}}
<trace>
{{.TraceJSON}}
</trace>

{{if .UserFeedback}}<user_feedback>
{{.UserFeedback}}
</user_feedback>{{end}}

Evaluate and respond with JSON only.`

const ToolEvo = `Evolution tool for self-repair and quality iteration.

Actions:
- assert: Run quality assertions against execution output.
- diagnose: Analyze why execution failed and classify the error.
- stats: Show evolution metrics and success rates.

EVO LOOP: execute → evaluate → (if failed: diagnose → repair → re-evaluate) → complete`
