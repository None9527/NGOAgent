import { getApiBase, getAuthToken } from '../chat/api'

const BASE = getApiBase() || import.meta.env.VITE_API_URL || ''

/** Build headers with auth token if available */
function withAuth(extra?: Record<string, string>): Record<string, string> {
    const headers: Record<string, string> = { ...extra }
    const token = getAuthToken()
    if (token) headers['Authorization'] = `Bearer ${token}`
    return headers
}

async function json<T>(path: string, opts?: RequestInit): Promise<T> {
    const mergedHeaders = withAuth(opts?.headers as Record<string, string> | undefined)
    const res = await fetch(`${BASE}${path}`, { ...opts, headers: mergedHeaders })
    if (!res.ok) throw new Error(await res.text())
    return res.json()
}

const J = (body: unknown) => ({
    method: 'POST' as const,
    headers: withAuth({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(body),
})

const PUT = (body: unknown) => ({
    method: 'PUT' as const,
    headers: withAuth({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(body),
})

const DEL = () => ({ method: 'DELETE' as const, headers: withAuth() })

export const api = {
    // ── RunController ──
    chat(chatId: number, message: string, model?: string) {
        return fetch(`${BASE}/api/v1/chat`, {
            method: 'POST',
            headers: withAuth({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ chatId, message, model }),
        })
    },
    stop: (chatId: number) => json<{ stopped: boolean }>(`/api/v1/chat/stop?chatId=${chatId}`, { method: 'POST' }),
    status: (chatId: number) => json<{ status: unknown; runState: string; isActive: boolean }>(`/api/v1/chat/status?chatId=${chatId}`),
    sendMessage: (chatId: number, message: string, channel?: string) => json<{ ok: boolean }>('/api/v1/chat/send', J({ chatId, message, channel })),

    // ── SessionManager ──
    newSession: (chatId: number) => json<{ ok: boolean }>(`/api/v1/session/new?chatId=${chatId}`, { method: 'POST' }),
    models: (chatId: number) => json<{ models: ModelInfo[]; current: string }>(`/api/v1/session/models?chatId=${chatId}`),
    switchModel: (chatId: number, model: string) => json<{ ok: boolean; model: string }>('/api/v1/session/model', PUT({ chatId, model })),
    getDelegation: (chatId: number) => json<{ mode: string }>(`/api/v1/session/delegation?chatId=${chatId}`),
    setDelegation: (chatId: number, mode: string) => json<{ ok: boolean }>('/api/v1/session/delegation', PUT({ chatId, mode })),

    // ── SettingsManager ──
    getSettings: (chatId: number) => json<Settings>(`/api/v1/settings?chatId=${chatId}`),
    updateSettings: (chatId: number, settings: Partial<Settings>) => json<{ ok: boolean }>('/api/v1/settings', PUT({ chatId, ...settings })),

    // ── ContextManager ──
    history: (chatId: number) => json<{ history: HistoryEntry[] }>(`/api/v1/context/history?chatId=${chatId}`),
    appendHistory: (chatId: number, userMsg: string, assistantMsg: string) => json<{ ok: boolean }>('/api/v1/context/history', J({ chatId, userMsg, assistantMsg })),
    clearHistory: (chatId: number) => json<{ ok: boolean }>(`/api/v1/context/history?chatId=${chatId}`, DEL()),
    compact: (chatId: number, instructions?: string) => json<{ tokensBefore: number; tokensAfter: number; saved: number }>('/api/v1/context/compact', J({ chatId, instructions })),
    contextStats: (chatId: number) => json<ContextStats>(`/api/v1/context/stats?chatId=${chatId}`),

    // ── SecurityPolicy ──
    getSecurity: () => json<SecurityInfo>('/api/v1/security'),
    updateSecurity: (opts: Partial<SecurityUpdate>) => json<{ ok: boolean }>('/api/v1/security', PUT(opts)),

    // ── Scheduler ──
    listJobs: () => json<{ jobs: CronJob[] }>('/api/v1/scheduler/jobs'),
    addJob: (name: string, schedule: string, command: string, type?: string, chatId?: number) => json<{ ok: boolean }>('/api/v1/scheduler/jobs', J({ name, schedule, command, type, chatId })),
    removeJob: (name: string) => json<{ ok: boolean }>(`/api/v1/scheduler/jobs/${name}`, DEL()),
    enableJob: (name: string) => json<{ ok: boolean }>(`/api/v1/scheduler/jobs/${name}/enable`, { method: 'POST' }),
    disableJob: (name: string) => json<{ ok: boolean }>(`/api/v1/scheduler/jobs/${name}/disable`, { method: 'POST' }),
    runJob: (name: string) => json<{ ok: boolean }>(`/api/v1/scheduler/jobs/${name}/run`, { method: 'POST' }),

    // ── SessionPersistence ──
    listSessions: (limit = 50, offset = 0) => json<{ sessions: SessionSummary[]; total: number }>(`/api/v1/sessions?limit=${limit}&offset=${offset}`),
    searchSessions: (q: string) => json<{ sessions: SessionSummary[] }>(`/api/v1/sessions/search?q=${encodeURIComponent(q)}`),
    getSession: (id: string) => json<unknown>(`/api/v1/sessions/${id}`),
    deleteSession: (id: string) => json<{ ok: boolean }>(`/api/v1/sessions/${id}`, DEL()),
    renameSession: (id: string, title: string) => json<{ ok: boolean }>(`/api/v1/sessions/${id}/title`, PUT({ title })),
    restoreSession: (chatId: number, id: string) => json<{ ok: boolean }>(`/api/v1/sessions/${id}/restore?chatId=${chatId}`, { method: 'POST' }),
    exportSession: (id: string, format = 'markdown') => fetch(`${BASE}/api/v1/sessions/${id}/export?format=${format}`, { headers: withAuth() }),
    listTags: () => json<{ tags: string[] }>('/api/v1/sessions/tags'),
    tagSession: (id: string, tag: string) => json<{ ok: boolean }>(`/api/v1/sessions/${id}/tag`, J({ tag })),
    listFolders: () => json<{ folders: Folder[] }>('/api/v1/sessions/folders'),
    moveSession: (id: string, folderId: string) => json<{ ok: boolean }>(`/api/v1/sessions/${id}/move`, J({ folderId })),

    // ── ToolManager ──
    listTools: () => json<{ tools: ToolInfo[] }>('/api/v1/tools'),
    getToolDetail: (name: string) => json<unknown>(`/api/v1/tools/${name}`),
    enableTool: (name: string) => json<{ ok: boolean }>(`/api/v1/tools/${name}/enable`, { method: 'POST' }),
    disableTool: (name: string) => json<{ ok: boolean }>(`/api/v1/tools/${name}/disable`, { method: 'POST' }),

    // ── SkillManager ──
    listSkills: () => json<{ skills: SkillInfo[] }>('/api/v1/skills'),
    getSkillDetail: (id: string) => json<unknown>(`/api/v1/skills/${id}`),
    installSkill: (source: string) => json<unknown>('/api/v1/skills/install', J({ source })),
    uninstallSkill: (id: string) => json<{ ok: boolean }>(`/api/v1/skills/${id}`, DEL()),
    enableSkill: (id: string) => json<{ ok: boolean }>(`/api/v1/skills/${id}/enable`, { method: 'POST' }),
    disableSkill: (id: string) => json<{ ok: boolean }>(`/api/v1/skills/${id}/disable`, { method: 'POST' }),
    updateSkillConfig: (id: string, config: Record<string, string>) => json<{ ok: boolean }>(`/api/v1/skills/${id}/config`, PUT({ config })),

    // ── SubagentTracker ──
    listSubagents: (chatId: number) => json<{ subagents: unknown[] }>(`/api/v1/subagents?chatId=${chatId}`),
    stopSubagent: (chatId: number, runId: string) => json<{ ok: boolean }>(`/api/v1/subagents/${runId}/stop?chatId=${chatId}`, { method: 'POST' }),
    stopAllSubagents: (chatId: number) => json<{ stopped: number }>(`/api/v1/subagents/stop-all?chatId=${chatId}`, { method: 'POST' }),
    getSubagentLog: (chatId: number, runId: string, limit = 50) => json<{ logs: unknown[] }>(`/api/v1/subagents/${runId}/logs?chatId=${chatId}&limit=${limit}`),

    // ── MCPManager ──
    listMCP: () => json<{ servers: MCPServer[] }>('/api/v1/mcp/servers'),
    addMCP: (name: string, url: string, config?: Record<string, string>) => json<{ ok: boolean }>('/api/v1/mcp/servers', J({ name, url, config })),
    removeMCP: (name: string) => json<{ ok: boolean }>(`/api/v1/mcp/servers/${name}`, DEL()),
    getMCPTools: (name: string) => json<{ tools: ToolInfo[] }>(`/api/v1/mcp/servers/${name}/tools`),

    // ── PromptTemplateManager ──
    listPrompts: () => json<{ templates: PromptTemplate[] }>('/api/v1/prompts'),
    createPrompt: (name: string, command: string, content: string) => json<unknown>('/api/v1/prompts', J({ name, command, content })),
    getPrompt: (id: string) => json<unknown>(`/api/v1/prompts/${id}`),
    updatePrompt: (id: string, name: string, content: string) => json<{ ok: boolean }>(`/api/v1/prompts/${id}`, PUT({ name, content })),
    deletePrompt: (id: string) => json<{ ok: boolean }>(`/api/v1/prompts/${id}`, DEL()),

    // ── StatsProvider ──
    sessionStats: (chatId: number) => json<unknown>(`/api/v1/stats/session?chatId=${chatId}`),
    globalStats: () => json<unknown>('/api/v1/stats/global'),

    // ── ConfigManager ──
    getConfig: () => json<unknown>('/api/v1/config'),
    getConfigSchema: () => json<unknown>('/api/v1/config/schema'),
    getConfigValue: (path: string) => json<{ value: unknown }>(`/api/v1/config/value?path=${encodeURIComponent(path)}`),
    setConfigValue: (path: string, value: unknown) => json<{ ok: boolean }>('/api/v1/config', PUT({ path, value })),
    resetConfig: (path: string) => json<{ ok: boolean }>(`/api/v1/config?path=${encodeURIComponent(path)}`, DEL()),

    // ── SystemAdmin ──
    systemInfo: () => json<unknown>('/api/v1/system/info'),
    healthCheck: () => json<unknown>('/api/v1/system/health'),
    systemLogs: (limit = 50, level = 'info') => json<{ logs: unknown[] }>(`/api/v1/system/logs?limit=${limit}&level=${level}`),
    restart: () => json<{ ok: boolean }>('/api/v1/system/restart', { method: 'POST' }),
    updateStatus: () => json<unknown>('/api/v1/system/update'),

    // ── DataManager: Files ──
    listFiles: () => json<{ files: FileInfo[] }>('/api/v1/data/files'),
    uploadFile: (file: File) => {
        const form = new FormData()
        form.append('file', file)
        return json<FileInfo>('/api/v1/data/files', { method: 'POST', body: form })
    },
    getFile: (id: string) => json<FileInfo>(`/api/v1/data/files/${id}`),
    deleteFile: (id: string) => json<{ ok: boolean }>(`/api/v1/data/files/${id}`, DEL()),
    getFileContent: (id: string) => fetch(`${BASE}/api/v1/data/files/${id}/content`, { headers: withAuth() }),
    searchFiles: (q: string) => json<{ files: FileInfo[] }>(`/api/v1/data/files/search?q=${encodeURIComponent(q)}`),

    // ── DataManager: Memory ──
    listMemories: () => json<{ memories: Memory[] }>('/api/v1/data/memory'),
    addMemory: (content: string) => json<Memory>('/api/v1/data/memory', J({ content })),
    updateMemory: (id: string, content: string) => json<{ ok: boolean }>(`/api/v1/data/memory/${id}`, PUT({ content })),
    deleteMemory: (id: string) => json<{ ok: boolean }>(`/api/v1/data/memory/${id}`, DEL()),
    queryMemory: (query: string, topK = 10) => json<{ memories: Memory[] }>('/api/v1/data/memory/query', J({ query, topK })),

    // ── DataManager: Knowledge ──
    listKnowledgeBases: () => json<{ knowledgeBases: KnowledgeBase[] }>('/api/v1/data/knowledge'),
    createKnowledgeBase: (name: string, description: string) => json<KnowledgeBase>('/api/v1/data/knowledge', J({ name, description })),
    getKnowledgeBase: (id: string) => json<KnowledgeBase>(`/api/v1/data/knowledge/${id}`),
    updateKnowledgeBase: (id: string, name: string, description: string) => json<{ ok: boolean }>(`/api/v1/data/knowledge/${id}`, PUT({ name, description })),
    deleteKnowledgeBase: (id: string) => json<{ ok: boolean }>(`/api/v1/data/knowledge/${id}`, DEL()),
    addFileToKnowledge: (kbId: string, fileId: string) => json<{ ok: boolean }>(`/api/v1/data/knowledge/${kbId}/files`, J({ fileId })),
    queryKnowledge: (kbId: string, query: string, topK = 10) => json<{ results: unknown[] }>(`/api/v1/data/knowledge/${kbId}/query`, J({ query, topK })),
}

// ── Types ──

export interface ModelInfo { ID: string; Alias: string; Provider: string }
export interface Settings { thinkLevel: string; verbose: boolean; reasoning: string; usageMode: string; activation: string; sendPolicy: string }
export interface HistoryEntry { Role: string; Content: string }
export interface ContextStats { MessageCount: number; TokenCount: number; MaxTokens: number }
export interface SecurityInfo { approvalMode: string; trustedTools: string[]; dangerousTools: string[]; trustedCommands: string[] }
export interface SecurityUpdate { approvalMode?: string; trustTool?: string; untrustTool?: string; trustCommand?: string }
export interface CronJob { Name: string; Schedule: string; Command: string; Enabled: boolean; LastRun: string; NextRun: string }
export interface SessionSummary { ID: string; Title: string; ChatID: number; UpdatedAt: string; MessageCount: number }
export interface Folder { ID: string; Name: string }
export interface ToolInfo { Name: string; Description: string; Enabled: boolean; Category: string }
export interface SkillInfo { ID: string; Name: string; Enabled: boolean; Description: string }
export interface MCPServer { Name: string; URL: string; Status: string; ToolCount: number }
export interface PromptTemplate { ID: string; Name: string; Command: string; Content: string }
export interface FileInfo { ID: string; Name: string; Size: number; MimeType: string; CreatedAt: string }
export interface Memory { ID: string; Content: string; CreatedAt: string }
export interface KnowledgeBase { ID: string; Name: string; Description: string; FileCount: number }
