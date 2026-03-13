import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// dist is in cli/dist, so ../../gateway/proto/agent_service.proto
const PROTO_PATH = join(__dirname, '../../gateway/proto/agent_service.proto');
const DEFAULT_ADDR = 'localhost:50051';

// Load proto definition
const packageDef = protoLoader.loadSync(PROTO_PATH, {
    keepCase: false,
    longs: Number,
    enums: String,
    defaults: true,
    oneofs: true,
});

const proto = grpc.loadPackageDefinition(packageDef) as any;
const AgentService = proto.ngoclaw.agent.v1.AgentService;

export interface ChatEvent {
    type: string;
    text: string;
    toolName: string;
    toolInput: string;
    toolOutput: string;
    toolApproved: boolean;
    error: string;
    callId: string;
    status: string;
    thinking: string;
    stepType: string;
    stepNumber: number;
    totalSteps: number;
    tokenCount: number;
    modelUsed: string;
    success: boolean;
}

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
 * gRPC client for the NGOClaw Agent backend.
 * Full CC-aligned API surface.
 */
export class AgentClient {
    private client: any;
    private sessionId: string;

    constructor(addr: string = DEFAULT_ADDR) {
        this.client = new AgentService(
            addr,
            grpc.credentials.createInsecure(),
        );
        this.sessionId = `cli-${Date.now()}`;
    }

    getSessionId(): string {
        return this.sessionId;
    }

    /** Generic unary RPC helper */
    private rpc<T>(method: string, req: any): Promise<T> {
        return new Promise((resolve, reject) => {
            this.client[method](req, (err: Error | null, res: any) => {
                if (err) reject(err);
                else resolve(res as T);
            });
        });
    }

    /** Session-scoped RPC helper */
    private sessionRpc<T>(method: string, extra: any = {}): Promise<T> {
        return this.rpc<T>(method, { sessionId: this.sessionId, ...extra });
    }

    // ─── Session ───
    async newSession() { return this.sessionRpc<{ ok: boolean; message: string }>('NewSession', { userId: 0 }); }

    // ─── Health & Status ───
    async healthCheck() { return this.rpc<HealthInfo>('HealthCheck', {}); }
    async getStatus() { return this.sessionRpc<StatusInfo>('GetStatus'); }
    async getContextStats() { return this.sessionRpc<{ messageCount: number; tokenCount: number; maxTokens: number }>('GetContextStats'); }

    // ─── Models ───
    async listModels() { return this.rpc<{ models: ModelInfo[]; currentModel: string }>('ListModels', {}); }
    async switchModel(model: string) { return this.sessionRpc<{ ok: boolean; message: string }>('SwitchModel', { model }); }

    // ─── Settings ───
    async getSettings() { return this.sessionRpc<SettingsInfo>('GetSettings'); }
    async updateSettings(updates: Partial<SettingsInfo>) { return this.sessionRpc<{ ok: boolean; message: string }>('UpdateSettings', updates); }

    // ─── History ───
    async clearHistory() { return this.sessionRpc<{ ok: boolean; message: string }>('ClearHistory'); }
    async getHistory() { return this.sessionRpc<{ messages: HistoryMessage[] }>('GetHistory'); }
    async compactContext(instructions: string = '') { return this.sessionRpc<{ ok: boolean; message: string }>('CompactContext', { instructions }); }

    // ─── Tools & Skills ───
    async listTools() { return this.rpc<{ tools: ToolInfo[] }>('ListTools', {}); }
    async listSkills() { return this.rpc<{ skills: SkillInfo[] }>('ListSkills', {}); }

    // ─── Stats ───
    async getSessionStats() { return this.sessionRpc<SessionStats>('GetSessionStats'); }
    async getGlobalStats() { return this.rpc<GlobalStats>('GetGlobalStats', {}); }

    // ─── System ───
    async getSystemInfo() { return this.rpc<SystemInfo>('GetSystemInfo', {}); }

    // ─── Security ───
    async getSecurity() { return this.rpc<SecurityInfo>('GetSecurity', {}); }
    async setApprovalMode(mode: string) { return this.rpc<{ ok: boolean; message: string }>('SetApprovalMode', { value: mode }); }

    // ─── Cron ───
    async listCronJobs() { return this.rpc<{ jobs: CronJob[] }>('ListCronJobs', {}); }
    async cronRemove(name: string) { return this.rpc<{ ok: boolean; message: string }>('CronRemove', { value: name }); }
    async cronEnable(name: string) { return this.rpc<{ ok: boolean; message: string }>('CronEnable', { value: name }); }
    async cronDisable(name: string) { return this.rpc<{ ok: boolean; message: string }>('CronDisable', { value: name }); }
    async cronRunNow(name: string) { return this.rpc<{ ok: boolean; message: string }>('CronRunNow', { value: name }); }

    // ─── Control ───
    async stopRun() { return this.sessionRpc<{ ok: boolean; message: string }>('StopRun'); }
    async approveToolCall(callId: string, approved: boolean) {
        return this.rpc<{ ok: boolean; message: string }>('ApproveToolCall', { sessionId: this.sessionId, callId, approved });
    }

    /**
     * Start a streaming chat.
     * Returns an event emitter-like stream that yields ChatEvent objects.
     */
    chat(
        message: string,
        onEvent: (event: ChatEvent) => void,
        onEnd: () => void,
        onError: (err: Error) => void,
    ): { cancel: () => void } {
        const call = this.client.Chat({
            message,
            sessionId: this.sessionId,
            model: '',
            systemPrompt: '',
            workspace: process.cwd(),
        });

        call.on('data', (event: ChatEvent) => onEvent(event));
        call.on('end', () => onEnd());
        call.on('error', (err: Error) => onError(err));

        return {
            cancel: () => call.cancel(),
        };
    }
}
