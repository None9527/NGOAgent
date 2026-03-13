/**
 * ConfigContext — Server config, client instance, model info, stats.
 * Manages: connection lifecycle, health check, model switching, token stats.
 */
import React, { createContext, useContext, useState, useEffect, useRef, useCallback } from 'react';
import { AgentClient } from '../api/client.js';

// ═══════════════════════════════════════════
// Types
// ═══════════════════════════════════════════

export interface TokenStats {
    inputTokens: number;
    outputTokens: number;
    tokenCount: number;
    messageCount: number;
    maxTokens: number;
    costUsd: number;
}

interface ConfigState {
    // Connection
    client: AgentClient | null;
    ready: boolean;
    connError: string | null;
    // Server info
    version: string;
    model: string;
    tools: number;
    approvalMode: string;
    // Stats
    stats: TokenStats;
}

interface ConfigActions {
    setModel: (m: string) => void;
    setApprovalMode: (m: string) => void;
    updateStats: (partial: Partial<TokenStats>) => void;
    cycleApprovalMode: () => Promise<void>;
}

type ConfigContextValue = ConfigState & ConfigActions;

// ═══════════════════════════════════════════
// Context
// ═══════════════════════════════════════════

const ConfigContext = createContext<ConfigContextValue | null>(null);

export function useConfig(): ConfigContextValue {
    const ctx = useContext(ConfigContext);
    if (!ctx) throw new Error('useConfig must be used within ConfigProvider');
    return ctx;
}

// ═══════════════════════════════════════════
// Provider
// ═══════════════════════════════════════════

const DEFAULT_STATS: TokenStats = {
    inputTokens: 0, outputTokens: 0, tokenCount: 0,
    messageCount: 0, maxTokens: 128000, costUsd: 0,
};

interface ConfigProviderProps {
    serverAddr: string;
    children: React.ReactNode;
}

export const ConfigProvider: React.FC<ConfigProviderProps> = ({ serverAddr, children }) => {
    const clientRef = useRef<AgentClient | null>(null);
    const [ready, setReady] = useState(false);
    const [connError, setConnError] = useState<string | null>(null);
    const [version, setVersion] = useState('?.?.?');
    const [model, setModel] = useState('loading...');
    const [tools, setTools] = useState(0);
    const [approvalMode, setApprovalMode] = useState('auto');
    const [stats, setStats] = useState<TokenStats>(DEFAULT_STATS);

    // Initialize connection
    useEffect(() => {
        const client = new AgentClient(serverAddr);
        clientRef.current = client;

        (async () => {
            try {
                const health = await client.healthCheck();
                setVersion(health.version);
                setTools(health.tools);
                if (health.model) setModel(health.model);

                await client.newSession();

                // Fetch initial state
                try {
                    const info = await client.listModels();
                    const finalModel = info.currentModel || health.model || 'unknown';
                    setModel(finalModel);
                    const st = await client.getContextStats();
                    setStats(prev => ({ ...prev, ...st }));
                    const sec = await client.getSecurity();
                    setApprovalMode(sec.approvalMode || 'auto');
                } catch {
                    // Ignore initial fetch errors
                }

                setReady(true);
            } catch (err: any) {
                setConnError(`Cannot connect to backend: ${err.message}\nEnsure 'ngoclaw serve' is running on ${serverAddr}`);
            }
        })();
    }, [serverAddr]);

    const updateStats = useCallback((partial: Partial<TokenStats>) => {
        setStats(prev => ({ ...prev, ...partial }));
    }, []);

    const cycleApprovalMode = useCallback(async () => {
        const client = clientRef.current;
        if (!client) return;
        const modes = ['auto', 'supervised'];
        const idx = modes.indexOf(approvalMode);
        const nextMode = modes[(idx + 1) % modes.length];
        setApprovalMode(nextMode);
        try {
            await client.setApprovalMode(nextMode);
        } catch {
            setApprovalMode(approvalMode);
        }
    }, [approvalMode]);

    const value: ConfigContextValue = {
        client: clientRef.current,
        ready, connError,
        version, model, tools, approvalMode,
        stats,
        setModel, setApprovalMode, updateStats, cycleApprovalMode,
    };

    return <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>;
};
