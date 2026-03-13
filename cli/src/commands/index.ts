import type { AgentClient } from '../api/client.js';

/**
 * CC-aligned slash command registry.
 * Mirrors Go TUI commands.go (23 commands).
 */
export interface SlashCommand {
    name: string;
    description: string;
    hasSelector?: boolean;
    options?: { label: string; value: string; description?: string }[];
    execute: (client: AgentClient, args: string, cb: CommandCallbacks) => Promise<string | null>;
}

export interface CommandCallbacks {
    setMode: (mode: string) => void;
    setModel: (model: string) => void;
    clearMessages: () => void;
    exit: () => void;
}

// ─── Formatting helpers (match Go TUI style) ───

const section = (title: string) => `◇ ${title}\n`;
const kv = (label: string, value: string) => `  ${label.padEnd(12)} ${value}`;
const icon = (enabled: boolean) => enabled ? '●' : '○';

// ─── Commands ───

export const slashCommands: SlashCommand[] = [
    {
        name: '/help',
        description: 'Show available commands',
        execute: async () => {
            const cmds = slashCommands.map((c) => `  ${c.name.padEnd(18)} ${c.description}`).join('\n');
            const shortcuts = [
                '  Ctrl+C       Interrupt / double-tap to exit',
                '  Ctrl+D       Exit',
                '  Ctrl+L       Clear screen',
                '  ↑/↓          History navigation',
                '  Tab          Autocomplete',
                '  `start       Multi-line input',
            ].join('\n');
            return `${section('Commands')}\n${cmds}\n\n${section('Shortcuts')}\n${shortcuts}`;
        },
    },
    { name: '/quit', description: 'Exit', execute: async (_c, _a, cb) => { cb.exit(); return null; } },
    { name: '/exit', description: 'Exit (alias)', execute: async (_c, _a, cb) => { cb.exit(); return null; } },

    // ─── History & Context ───
    {
        name: '/clear',
        description: 'Clear conversation',
        execute: async (client, _a, cb) => { await client.clearHistory(); cb.clearMessages(); return null; },
    },
    {
        name: '/new',
        description: 'New session',
        execute: async (client, _a, cb) => { await client.newSession(); cb.clearMessages(); return '🔄 New session started'; },
    },
    {
        name: '/compact',
        description: 'Compact context',
        execute: async (client, args) => {
            const res = await client.compactContext(args);
            return `✓ ${res.message}`;
        },
    },
    {
        name: '/history',
        description: 'Show conversation history',
        execute: async (client) => {
            const res = await client.getHistory();
            if (!res.messages?.length) return 'No messages';
            const lines = res.messages.map((m, i) => {
                const content = m.content.length > 80 ? m.content.slice(0, 77) + '...' : m.content;
                return `  ${i + 1}. [${m.role}] ${content}`;
            });
            return `${section(`History (${res.messages.length})`)}\n${lines.join('\n')}`;
        },
    },

    // ─── Models ───
    {
        name: '/models',
        description: 'List models',
        execute: async (client) => {
            const res = await client.listModels();
            const lines = res.models.map((m) => {
                const cur = (m.id === res.currentModel || m.alias === res.currentModel) ? '▸ ' : '  ';
                const alias = m.alias ? ` (${m.alias})` : '';
                return `  ${cur}${m.id}${alias} [${m.provider}]`;
            });
            return `${section(`Models (${res.models.length})`)}\n${lines.join('\n')}`;
        },
    },
    {
        name: '/model',
        description: 'Switch model',
        hasSelector: true,
        execute: async (client, args, cb) => {
            if (!args) return null;
            await client.switchModel(args);
            cb.setModel(args);
            return `✓ Switched to ${args}`;
        },
    },

    // ─── Mode & Think ───
    {
        name: '/mode',
        description: 'Switch planning mode',
        hasSelector: true,
        options: [
            { label: 'forge', value: 'forge', description: 'Deep planning + execution' },
            { label: 'rush', value: 'rush', description: 'Fast, skip planning' },
            { label: 'auto', value: 'auto', description: 'Auto-detect complexity' },
        ],
        execute: async (client, args, cb) => {
            if (!args) return null;
            await client.updateSettings({ planningMode: args });
            cb.setMode(args);
            return `✓ Mode: ${args}`;
        },
    },
    {
        name: '/think',
        description: 'Set thinking level',
        hasSelector: true,
        options: [
            { label: 'off', value: 'off', description: 'No extended thinking' },
            { label: 'low', value: 'low', description: 'Brief' },
            { label: 'medium', value: 'medium', description: 'Moderate' },
            { label: 'high', value: 'high', description: 'Deep reasoning' },
        ],
        execute: async (client, args) => {
            if (!args) return null;
            await client.updateSettings({ thinkLevel: args });
            return `✓ Think: ${args}`;
        },
    },

    // ─── Settings ───
    {
        name: '/settings',
        description: 'View/edit settings',
        hasSelector: true,
        options: [
            { label: 'think', value: 'think', description: 'off/low/medium/high' },
            { label: 'verbose', value: 'verbose', description: 'true/false' },
            { label: 'reasoning', value: 'reasoning', description: 'native/tag/off' },
            { label: 'usage', value: 'usage', description: 'full/compact/off' },
            { label: 'activation', value: 'activation', description: 'always/mention/command' },
            { label: 'send_policy', value: 'send_policy', description: 'stream/batch' },
        ],
        execute: async (client, args) => {
            if (!args) {
                // Show current settings
                const s = await client.getSettings();
                return [
                    section('Settings'),
                    kv('think', s.thinkLevel),
                    kv('verbose', String(s.verbose)),
                    kv('reasoning', s.reasoning),
                    kv('usage', s.usageMode),
                    kv('activation', s.activation),
                    kv('send_policy', s.sendPolicy),
                    kv('delegation', s.delegation),
                    kv('planning', s.planningMode),
                    '',
                    '  Usage: /settings <key> <value>',
                ].join('\n');
            }
            // /settings key value
            const parts = args.split(/\s+/);
            if (parts.length < 2) return `Usage: /settings ${parts[0]} <value>`;
            const [key, ...rest] = parts;
            const value = rest.join(' ');
            const updates: Record<string, string> = {};
            const keyMap: Record<string, string> = {
                think: 'thinkLevel', verbose: 'verbose', reasoning: 'reasoning',
                usage: 'usageMode', activation: 'activation', send_policy: 'sendPolicy',
            };
            const mapped = keyMap[key];
            if (!mapped) return `Unknown setting: ${key}`;
            updates[mapped] = value;
            const res = await client.updateSettings(updates as any);
            return `✓ ${res.message}`;
        },
    },

    // ─── Status & Stats ───
    {
        name: '/status',
        description: 'Show session status',
        execute: async (client) => {
            const st = await client.getStatus();
            const ctx = await client.getContextStats();
            const pct = ctx.maxTokens > 0 ? Math.round((ctx.tokenCount / ctx.maxTokens) * 100) : 0;
            return [
                section('Status'),
                kv('Model', st.model),
                kv('State', st.runState),
                kv('Messages', String(ctx.messageCount)),
                kv('Context', `${ctx.tokenCount.toLocaleString()} / ${ctx.maxTokens.toLocaleString()} (${pct}%)`),
            ].join('\n');
        },
    },
    {
        name: '/stats',
        description: 'Usage statistics',
        execute: async (client) => {
            const s = await client.getSessionStats();
            const g = await client.getGlobalStats();
            return [
                section('Statistics'),
                '  Session:',
                kv('  Tokens', `${s.totalTokens} (↑${s.inputTokens} ↓${s.outputTokens})`),
                kv('  Tool calls', String(s.toolCalls)),
                kv('  Turns', String(s.turnCount)),
                '  Global:',
                kv('  Sessions', String(g.totalSessions)),
                kv('  Tokens', String(g.totalTokens)),
                kv('  Tool calls', String(g.totalToolCalls)),
            ].join('\n');
        },
    },
    {
        name: '/cost',
        description: 'Token cost estimate',
        execute: async (client) => {
            const ctx = await client.getContextStats();
            const cost = (ctx.tokenCount * 0.003) / 1000;
            const pct = ctx.maxTokens > 0 ? Math.round((ctx.tokenCount / ctx.maxTokens) * 100) : 0;
            return [
                section('Cost'),
                kv('Tokens', ctx.tokenCount.toLocaleString()),
                kv('Context', `${pct}%`),
                kv('Est. cost', `~$${cost.toFixed(4)}`),
            ].join('\n');
        },
    },

    // ─── Tools & Skills ───
    {
        name: '/tools',
        description: 'List tools',
        execute: async (client) => {
            const res = await client.listTools();
            const lines = res.tools.map((t) => {
                const desc = t.description.length > 50 ? t.description.slice(0, 47) + '...' : t.description;
                return `  ${icon(t.enabled)} ${t.name.padEnd(16)} ${desc}`;
            });
            return `${section(`Tools (${res.tools.length})`)}\n${lines.join('\n')}`;
        },
    },
    {
        name: '/skills',
        description: 'List skills',
        execute: async (client) => {
            const res = await client.listSkills();
            const lines = res.skills.map((s) => `  ${icon(s.enabled)} ${s.name}`);
            return `${section(`Skills (${res.skills.length})`)}\n${lines.join('\n')}`;
        },
    },

    // ─── System ───
    {
        name: '/info',
        description: 'System information',
        execute: async (client) => {
            const i = await client.getSystemInfo();
            return [
                section('System Info'),
                kv('Version', i.version),
                kv('Go', i.goVersion),
                kv('Uptime', `${Math.round(i.uptimeMs / 3600000)}h`),
                kv('OS/Arch', `${i.os}/${i.arch}`),
                kv('Models', String(i.models)),
                kv('Tools', String(i.tools)),
                kv('Skills', String(i.skills)),
            ].join('\n');
        },
    },
    {
        name: '/version',
        description: 'Show version',
        execute: async (client) => {
            const h = await client.healthCheck();
            return `NGOClaw v${h.version}`;
        },
    },
    {
        name: '/doctor',
        description: 'Health check',
        execute: async (client) => {
            const h = await client.healthCheck();
            return [
                section('Health Check'),
                kv('Healthy', h.healthy ? '✓' : '✗'),
                kv('Version', h.version),
                kv('Model', h.model),
                kv('Tools', String(h.tools)),
            ].join('\n');
        },
    },

    // ─── Security ───
    {
        name: '/security',
        description: 'Security policy',
        execute: async (client) => {
            const s = await client.getSecurity();
            const modeName: Record<string, string> = {
                eager: 'auto (auto-approve)',
                auto: 'standard (ask)',
                off: 'strict (ask all)',
            };
            return [
                section('Security Policy'),
                kv('Mode', modeName[s.approvalMode] || s.approvalMode),
                kv('Trusted', s.trustedTools?.join(', ') || 'none'),
                kv('Dangerous', s.dangerousTools?.join(', ') || 'none'),
                kv('Trusted Cmds', s.trustedCommands?.join(', ') || 'none'),
            ].join('\n');
        },
    },

    // ─── Cron ───
    {
        name: '/cron',
        description: 'Cron job management',
        execute: async (client, args) => {
            if (!args) {
                const res = await client.listCronJobs();
                if (!res.jobs?.length) return 'No cron jobs';
                const lines = res.jobs.map((j) =>
                    `  ${icon(j.enabled)} ${j.name.padEnd(16)} ${j.schedule}  runs:${j.runCount} fails:${j.failCount}`
                );
                return `${section(`Cron Jobs (${res.jobs.length})`)}\n${lines.join('\n')}`;
            }
            const parts = args.split(/\s+/);
            const sub = parts[0];
            const name = parts[1];
            if (!name && sub !== 'list') return 'Usage: /cron <remove|enable|disable|run> <name>';
            switch (sub) {
                case 'remove': case 'rm': { const r = await client.cronRemove(name); return `✓ ${r.message}`; }
                case 'enable': { const r = await client.cronEnable(name); return `✓ ${r.message}`; }
                case 'disable': { const r = await client.cronDisable(name); return `✓ ${r.message}`; }
                case 'run': { const r = await client.cronRunNow(name); return `✓ ${r.message}`; }
                default: return 'Usage: /cron [remove|enable|disable|run] <name>';
            }
        },
    },

    // ─── Tasks & Diff (placeholder) ───
    { name: '/tasks', description: 'Background tasks', execute: async () => 'No background tasks' },
    { name: '/diff', description: 'File changes', execute: async () => 'Diff viewer coming in Batch 2' },
];

/** Find command by name */
export function findCommand(input: string): SlashCommand | undefined {
    const cmd = input.split(' ')[0]?.toLowerCase();
    return slashCommands.find((c) => c.name === cmd);
}

/** Get args after command name */
export function getCommandArgs(input: string): string {
    const idx = input.indexOf(' ');
    return idx > 0 ? input.slice(idx + 1).trim() : '';
}

/** Tab completion */
export function completeCommand(partial: string): string[] {
    if (!partial.startsWith('/')) return [];
    return slashCommands.filter((c) => c.name.startsWith(partial.toLowerCase())).map((c) => c.name);
}

/** Sub-arg completion */
export function completeSubArgs(cmd: string, partial: string): string[] {
    const candidates: Record<string, string[]> = {
        '/settings': ['think', 'verbose', 'reasoning', 'usage', 'activation', 'send_policy'],
        '/security': ['mode', 'trust', 'distrust', 'trust_cmd'],
        '/think': ['off', 'low', 'medium', 'high'],
        '/mode': ['forge', 'rush', 'auto'],
        '/compact': ['hard', 'soft'],
        '/cron': ['remove', 'enable', 'disable', 'run'],
    };
    const list = candidates[cmd] || [];
    return list.filter((c) => c.startsWith(partial.toLowerCase()));
}
