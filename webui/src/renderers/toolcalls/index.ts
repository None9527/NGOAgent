/**
 * Tool call renderers — re-exports all tool components and self-registers them
 * into the toolRegistry. Import this file once in the app entry point and
 * all tools become available to ChatViewer without a switch/case.
 */

// Re-export shared toolcall components and types
export * from './shared/index.js';

// Business ToolCall components
export { ThinkToolCall } from './ThinkToolCall.js';
export { SaveMemoryToolCall } from './SaveMemoryToolCall.js';
export { GenericToolCall } from './GenericToolCall.js';
export { EditToolCall } from './EditToolCall.js';
export { WriteToolCall } from './WriteToolCall.js';
export { SearchToolCall } from './SearchToolCall.js';
export { UpdatedPlanToolCall } from './UpdatedPlanToolCall.js';
export { ShellToolCall } from './ShellToolCall.js';
export { ReadToolCall } from './ReadToolCall.js';
export { WebFetchToolCall } from './WebFetchToolCall.js';
export { SpawnAgentToolCall } from './SpawnAgentToolCall.js';
export { CheckboxDisplay } from './CheckboxDisplay.js';
export type { CheckboxDisplayProps } from './CheckboxDisplay.js';
export { ArtifactHookToolCall } from './ArtifactHookToolCall.js';

// ─── Self-registration into toolRegistry ─────────────────

import {
  registerTool,
  registerFallbackTool,
  registerToolMatcher,
  type ToolCallProps,
} from './toolRegistry.js';
import type { ComponentType } from 'react';

// Import components for registration
import { ThinkToolCall as _Think } from './ThinkToolCall.js';
import { SaveMemoryToolCall as _Memory } from './SaveMemoryToolCall.js';
import { GenericToolCall as _Generic } from './GenericToolCall.js';
import { EditToolCall as _Edit } from './EditToolCall.js';
import { WriteToolCall as _Write } from './WriteToolCall.js';
import { SearchToolCall as _Search } from './SearchToolCall.js';
import { UpdatedPlanToolCall as _Plan } from './UpdatedPlanToolCall.js';
import { ShellToolCall as _Shell } from './ShellToolCall.js';
import { ReadToolCall as _Read } from './ReadToolCall.js';
import { WebFetchToolCall as _WebFetch } from './WebFetchToolCall.js';
import { SpawnAgentToolCall as _Spawn } from './SpawnAgentToolCall.js';
import { ArtifactHookToolCall as _Artifact } from './ArtifactHookToolCall.js';

type ToolComp = ComponentType<ToolCallProps>;

// Exact kind → renderer mappings
const kindMap: Record<string, ToolComp> = {
  read:        _Read as ToolComp,
  write:       _Write as ToolComp,
  edit:        _Edit as ToolComp,
  execute:     _Shell as ToolComp,
  bash:        _Shell as ToolComp,
  command:     _Shell as ToolComp,
  spawn_agent: _Spawn as ToolComp,
  updated_plan: _Plan as ToolComp,
  updatedplan: _Plan as ToolComp,
  todo_write:  _Plan as ToolComp,
  update_todos: _Plan as ToolComp,
  todowrite:   _Plan as ToolComp,
  search:      _Search as ToolComp,
  think:       _Think as ToolComp,
  thinking:    _Think as ToolComp,
  save_memory: _Memory as ToolComp,
  savememory:  _Memory as ToolComp,
  memory:      _Memory as ToolComp,
  fetch:       _WebFetch as ToolComp,
  web_fetch:   _WebFetch as ToolComp,
  webfetch:    _WebFetch as ToolComp,
  web_search:  _WebFetch as ToolComp,
  websearch:   _WebFetch as ToolComp,
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
  { component: _Artifact as ToolComp, priority: 10 },
)

// Fallback for unknown tool kinds
registerFallbackTool({ component: _Generic as ToolComp })
