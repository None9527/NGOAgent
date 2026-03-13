/**
 * HTTP/SSE client for the NGOClaw Agent backend.
 * Drop-in replacement for the old gRPC client.
 * Same interface (AgentClient) — all HTTP under the hood.
 */

import { adaptEvent, type ChatEvent } from './adapter.js';
export type { ChatEvent } from './adapter.js';

const DEFAULT_ADDR = 'http://localhost:19996';


export interface HealthInfo {
    healthy: boolean;
    version: string;
    tools: number;
    model: string;
}

export interface StatusInfo {
    model: string;
    runState: string;
    sessionId: string;
    msgCount: number;
    tokenCount: number;
}

export interface ModelInfo {
    id: string;
    alias: string;
    provider: string;
    description: string;
}

export interface SettingsInfo {
    thinkLevel: string;
    verbose: boolean;
    reasoning: string;
    usageMode: string;
    activation: string;
    sendPolicy: string;
    delegation: string;
    planningMode: string;
}

export interface ToolInfo {
    name: string;
    description: string;
    enabled: boolean;
}

export interface SkillInfo {
    name: string;
    enabled: boolean;
}

export interface SessionStats {
    totalTokens: number;
    inputTokens: number;
    outputTokens: number;
    toolCalls: number;
    turnCount: number;
}

export interface GlobalStats {
    totalSessions: number;
    totalTokens: number;
    totalToolCalls: number;
}

export interface SystemInfo {
    version: string;
    goVersion: string;
    uptimeMs: number;
    os: string;
    arch: string;
    models: number;
    tools: number;
    skills: number;
}

export interface CronJob {
    name: string;
    schedule: string;
    enabled: boolean;
    runCount: number;
    failCount: number;
}

export interface SecurityInfo {
    approvalMode: string;
    trustedTools: string[];
    dangerousTools: string[];
    trustedCommands: string[];
}

export interface HistoryMessage {
    role: string;
    content: string;
}

/**
 * HTTP/SSE client for the NGOClaw Agent backend.
 * Full CC-aligned API surface (same interface as old gRPC client).
 */
export class AgentClient {
    private baseUrl: string;
    private sessionId: string;

    constructor(addr: string = DEFAULT_ADDR) {
        // Normalize: ensure http:// prefix
        this.baseUrl = addr.startsWith('http') ? addr : `http://${addr}`;
        this.sessionId = `cli-${Date.now()}`;
    }

    getSessionId(): string {
        return this.sessionId;
    }

    /** Generic JSON fetch helper */
    private async fetchJSON<T>(path: string, opts?: RequestInit): Promise<T> {
        const res = await fetch(`${this.baseUrl}${path}`, {
            headers: { 'Content-Type': 'application/json' },
            ...opts,
        });
        if (!res.ok) {
            const text = await res.text();
            throw new Error(`HTTP ${res.status}: ${text}`);
        }
        return res.json() as Promise<T>;
    }

    /** POST JSON helper */
    private async postJSON<T>(path: string, body: any): Promise<T> {
        return this.fetchJSON<T>(path, {
            method: 'POST',
            body: JSON.stringify(body),
        });
    }

    // ─── Session ───
    async newSession() {
        const res = await this.postJSON<{ session_id: string; title: string }>('/api/v1/session/new', { title: '' });
        this.sessionId = res.session_id;
        return { ok: true, message: `Session ${res.session_id}` };
    }

    // ─── Health & Status ───
    async healthCheck(): Promise<HealthInfo> {
        const res = await this.fetchJSON<{ status: string; version: string; model: string; tools: number }>('/v1/health');
        return { healthy: res.status === 'ok', version: res.version, model: res.model, tools: res.tools };
    }

    async getStatus(): Promise<StatusInfo> {
        const stats = await this.fetchJSON<{ model: string; history_count: number; token_estimate: number }>('/api/v1/stats');
        return {
            model: stats.model,
            runState: 'idle',
            sessionId: this.sessionId,
            msgCount: stats.history_count,
            tokenCount: stats.token_estimate,
        };
    }

    async getContextStats() {
        const stats = await this.fetchJSON<{ model: string; history_count: number; token_estimate: number }>('/api/v1/stats');
        return {
            messageCount: stats.history_count,
            tokenCount: stats.token_estimate,
            maxTokens: 128000,
        };
    }

    // ─── Models ───
    async listModels(): Promise<{ models: ModelInfo[]; currentModel: string }> {
        const res = await this.fetchJSON<{ models: string[]; current: string }>('/v1/models');
        const models: ModelInfo[] = res.models.map((m) => ({
            id: m,
            alias: '',
            provider: '',
            description: '',
        }));
        return { models, currentModel: res.current };
    }

    async switchModel(model: string) {
        await this.postJSON('/v1/model/switch', { model });
        return { ok: true, message: `Switched to ${model}` };
    }

    // ─── Settings ───
    async getSettings(): Promise<SettingsInfo> {
        const cfg = await this.fetchJSON<any>('/v1/config');
        const agent = cfg?.agent || {};
        return {
            thinkLevel: agent.think_level || 'off',
            verbose: agent.verbose || false,
            reasoning: agent.reasoning || 'native',
            usageMode: agent.usage_mode || 'full',
            activation: agent.activation || 'always',
            sendPolicy: agent.send_policy || 'stream',
            delegation: agent.delegation || 'off',
            planningMode: agent.planning_mode ? 'forge' : 'auto',
        };
    }

    async updateSettings(updates: Partial<SettingsInfo>) {
        const keyMap: Record<string, string> = {
            thinkLevel: 'agent.think_level',
            verbose: 'agent.verbose',
            reasoning: 'agent.reasoning',
            usageMode: 'agent.usage_mode',
            activation: 'agent.activation',
            sendPolicy: 'agent.send_policy',
            delegation: 'agent.delegation',
            planningMode: 'agent.planning_mode',
        };
        for (const [key, value] of Object.entries(updates)) {
            const dotKey = keyMap[key];
            if (dotKey) {
                await this.postJSON('/api/v1/config', { key: dotKey, value });
            }
        }
        return { ok: true, message: 'Settings updated' };
    }

    // ─── History ───
    async clearHistory() {
        await this.postJSON('/api/v1/history/clear', {});
        return { ok: true, message: 'History cleared' };
    }

    async getHistory(): Promise<{ messages: HistoryMessage[] }> {
        const res = await this.fetchJSON<{ messages: HistoryMessage[] }>(`/api/v1/history?session_id=${this.sessionId}`);
        return { messages: res.messages || [] };
    }

    async compactContext(instructions: string = '') {
        await this.postJSON('/api/v1/history/compact', { instructions });
        return { ok: true, message: 'Context compacted' };
    }

    // ─── Tools & Skills ───
    async listTools(): Promise<{ tools: ToolInfo[] }> {
        const res = await this.fetchJSON<{ tools: { name: string; enabled: boolean }[] }>('/api/v1/tools');
        const tools: ToolInfo[] = (res.tools || []).map((t) => ({
            name: t.name,
            description: '',
            enabled: t.enabled,
        }));
        return { tools };
    }

    async listSkills(): Promise<{ skills: SkillInfo[] }> {
        const res = await this.fetchJSON<{ skills: { name: string; type: string; status: string; description: string }[] }>('/api/v1/skills');
        const skills: SkillInfo[] = (res.skills || []).map((s) => ({
            name: s.name,
            enabled: s.status !== 'disabled',
        }));
        return { skills };
    }

    // ─── Stats ───
    async getSessionStats(): Promise<SessionStats> {
        const stats = await this.fetchJSON<{ history_count: number; token_estimate: number }>('/api/v1/stats');
        return {
            totalTokens: stats.token_estimate,
            inputTokens: Math.ceil(stats.token_estimate * 0.7),
            outputTokens: Math.ceil(stats.token_estimate * 0.3),
            toolCalls: 0,
            turnCount: stats.history_count,
        };
    }

    async getGlobalStats(): Promise<GlobalStats> {
        // TODO: implement in backend
        return { totalSessions: 0, totalTokens: 0, totalToolCalls: 0 };
    }

    // ─── System ───
    async getSystemInfo(): Promise<SystemInfo> {
        return this.fetchJSON<SystemInfo>('/api/v1/system');
    }

    // ─── Security ───
    async getSecurity(): Promise<SecurityInfo> {
        const res = await this.fetchJSON<{ mode: string; block_list: string[]; safe_commands: string[] }>('/api/v1/security');
        return {
            approvalMode: res.mode,
            trustedTools: [],
            dangerousTools: res.block_list || [],
            trustedCommands: res.safe_commands || [],
        };
    }

    async setApprovalMode(mode: string) {
        await this.postJSON('/api/v1/config', { key: 'security.mode', value: mode });
        return { ok: true, message: `Approval mode: ${mode}` };
    }

    // ─── Cron (stub — not yet implemented in backend) ───
    async listCronJobs(): Promise<{ jobs: CronJob[] }> {
        return { jobs: [] };
    }
    async cronRemove(_name: string) { return { ok: false, message: 'cron not implemented' }; }
    async cronEnable(_name: string) { return { ok: false, message: 'cron not implemented' }; }
    async cronDisable(_name: string) { return { ok: false, message: 'cron not implemented' }; }
    async cronRunNow(_name: string) { return { ok: false, message: 'cron not implemented' }; }

    // ─── Control ───
    async stopRun() {
        await this.postJSON('/v1/stop', {});
        return { ok: true, message: 'Run stopped' };
    }

    async approveToolCall(callId: string, approved: boolean) {
        await this.postJSON('/v1/approve', { approval_id: callId, approved });
        return { ok: true, message: approved ? 'Approved' : 'Denied' };
    }

    /**
     * Start a streaming chat via SSE.
     * Returns a cancel handle. Events are adapted to match the ChatEvent interface.
     */
    chat(
        message: string,
        onEvent: (event: ChatEvent) => void,
        onEnd: () => void,
        onError: (err: Error) => void,
    ): { cancel: () => void } {
        const controller = new AbortController();

        const run = async () => {
            try {
                const res = await fetch(`${this.baseUrl}/v1/chat`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        message,
                        session_id: this.sessionId,
                        stream: true,
                    }),
                    signal: controller.signal,
                });

                if (!res.ok) {
                    const text = await res.text();
                    onError(new Error(`HTTP ${res.status}: ${text}`));
                    return;
                }

                // Check if response is JSON (slash command)
                const contentType = res.headers.get('content-type') || '';
                if (contentType.includes('application/json')) {
                    const json = await res.json() as { result?: string };
                    if (json.result) {
                        onEvent(adaptEvent({ type: 'text_delta', content: json.result }));
                        onEvent(adaptEvent({ type: 'done' }));
                    }
                    onEnd();
                    return;
                }

                // SSE streaming
                const reader = res.body?.getReader();
                if (!reader) {
                    onError(new Error('No response body'));
                    return;
                }

                const decoder = new TextDecoder();
                let buffer = '';

                let sseRecvCount = 0;
                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;

                    buffer += decoder.decode(value, { stream: true });
                    const lines = buffer.split('\n');
                    buffer = lines.pop() || '';

                    for (const line of lines) {
                        if (!line.startsWith('data: ')) continue;
                        const payload = line.slice(6).trim();

                        if (payload === '[DONE]') {
                            onEvent(adaptEvent({ type: 'done' }));
                            continue;
                        }

                        try {
                            const raw = JSON.parse(payload);
                            onEvent(adaptEvent(raw));
                        } catch {
                            // Skip malformed SSE lines
                        }
                    }
                }

                onEnd();
            } catch (err: any) {
                if (err.name === 'AbortError') {
                    onEnd();
                } else {
                    onError(err);
                }
            }
        };

        run();

        return {
            cancel: () => controller.abort(),
        };
    }
}

