import React from 'react';
import { Text } from 'ink';

export interface StatusBarProps {
    model: string;
    mode: string;
    isStreaming: boolean;
    inputTokens: number;
    outputTokens: number;
    contextUsedPct: number;
    costUsd: number;
    duration?: number;
}

function fmtTok(n: number): string {
    if (n <= 0) return '0';
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000) return (n / 1_000).toFixed(1) + 'k';
    return String(n);
}

function fmtCost(usd: number): string {
    if (usd <= 0) return '';
    return usd > 0.5 ? `~$${usd.toFixed(2)}` : `~$${usd.toFixed(4)}`;
}

/**
 * CC-style status bar. Single line, right-aligned info.
 * Format: ---- in:3.8k out:39 ... Auto /qwen3.5-plus -
 */
export const StatusBar: React.FC<StatusBarProps> = ({
    model, mode, isStreaming, inputTokens, outputTokens,
    contextUsedPct, costUsd, duration,
}) => {
    const parts: string[] = [];
    const cost = fmtCost(costUsd);
    if (cost) parts.push(cost);
    if (inputTokens > 0 || outputTokens > 0) {
        parts.push('in:' + fmtTok(inputTokens) + ' out:' + fmtTok(outputTokens));
    }
    if (duration && duration > 0) parts.push(duration.toFixed(1) + 's');
    if (contextUsedPct > 0) parts.push(contextUsedPct + '%');
    const modeLabel = mode.charAt(0).toUpperCase() + mode.slice(1);
    const shortModel = model.split('/').pop() || model;
    parts.push('... ' + modeLabel + ' /' + shortModel);

    const right = ' ' + parts.join('  ') + ' -';
    const termW = process.stdout.columns || 80;
    const dashes = Math.max(0, termW - right.length - 2);

    return <Text dimColor>{'-'.repeat(dashes) + right}</Text>;
};
