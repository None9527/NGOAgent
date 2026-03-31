/**
 * Tool call renderers — re-exports all tool components and self-registers them
 * into the toolRegistry. Import this file once in the app entry point and
 * all tools become available to ChatViewer without a switch/case.
 */

// Business ToolCall components — single import for both re-export and registration
import { ThinkToolCall } from './ThinkToolCall.js';
import { SaveMemoryToolCall } from './SaveMemoryToolCall.js';
import { GenericToolCall } from './GenericToolCall.js';
import { EditToolCall } from './EditToolCall.js';
import { WriteToolCall } from './WriteToolCall.js';
import { SearchToolCall } from './SearchToolCall.js';
import { UpdatedPlanToolCall } from './UpdatedPlanToolCall.js';
import { ShellToolCall } from './ShellToolCall.js';
import { ReadToolCall } from './ReadToolCall.js';
import { WebFetchToolCall } from './WebFetchToolCall.js';
import { SpawnAgentToolCall } from './SpawnAgentToolCall.js';
import { CheckboxDisplay } from './CheckboxDisplay.js';
import { ArtifactHookToolCall } from './ArtifactHookToolCall.js';

// Re-exports for external consumers
export { ThinkToolCall, SaveMemoryToolCall, GenericToolCall, EditToolCall, WriteToolCall };
export { SearchToolCall, UpdatedPlanToolCall, ShellToolCall, ReadToolCall, WebFetchToolCall };
export { SpawnAgentToolCall, CheckboxDisplay, ArtifactHookToolCall };
export type { CheckboxDisplayProps } from './CheckboxDisplay.js';
export * from './shared/index.js';

// ─── Self-registration into toolRegistry ─────────────────

import {
  registerTool,
  registerFallbackTool,
  registerToolMatcher,
  type ToolCallProps,
} from './toolRegistry.js';
import type { ComponentType } from 'react';

type ToolComp = ComponentType<ToolCallProps>;

// Exact kind → renderer mappings
const kindMap: Record<string, ToolComp> = {
  read:        ReadToolCall as ToolComp,
  write:       WriteToolCall as ToolComp,
  edit:        EditToolCall as ToolComp,
  execute:     ShellToolCall as ToolComp,
  bash:        ShellToolCall as ToolComp,
  command:     ShellToolCall as ToolComp,
  spawn_agent: SpawnAgentToolCall as ToolComp,
  updated_plan: UpdatedPlanToolCall as ToolComp,
  updatedplan: UpdatedPlanToolCall as ToolComp,
  todo_write:  UpdatedPlanToolCall as ToolComp,
  update_todos: UpdatedPlanToolCall as ToolComp,
  todowrite:   UpdatedPlanToolCall as ToolComp,
  search:      SearchToolCall as ToolComp,
  think:       ThinkToolCall as ToolComp,
  thinking:    ThinkToolCall as ToolComp,
  save_memory: SaveMemoryToolCall as ToolComp,
  savememory:  SaveMemoryToolCall as ToolComp,
  memory:      SaveMemoryToolCall as ToolComp,
  fetch:       WebFetchToolCall as ToolComp,
  web_fetch:   WebFetchToolCall as ToolComp,
  webfetch:    WebFetchToolCall as ToolComp,
  web_search:  WebFetchToolCall as ToolComp,
  websearch:   WebFetchToolCall as ToolComp,
}

for (const [kind, comp] of Object.entries(kindMap)) {
  registerTool(kind, { component: comp })
}

// Wildcard: artifact detection for edit/write tool calls
registerToolMatcher(
  (data) => {
    const k = (data.kind || '').toLowerCase()
    if (k !== 'edit' && k !== 'write') return false
    const title = typeof data.title === 'string' ? data.title.toLowerCase() : ''
    return (
      title.endsWith('task.md') ||
      title.endsWith('implementation_plan.md') ||
      title.endsWith('walkthrough.md')
    )
  },
  { component: ArtifactHookToolCall as ToolComp, priority: 10 },
)

// Fallback for unknown tool kinds
registerFallbackTool({ component: GenericToolCall as ToolComp })
