import React from 'react';
import { Box, Text } from 'ink';

interface BannerProps {
    version: string;
    model: string;
    tools: number;
    cwd?: string;
    recentActivity?: string[];
}

/** Pad or truncate a string to exactly `width` characters */
function pad(s: string, width: number): string {
    if (s.length >= width) return s.slice(0, width);
    return s + ' '.repeat(width - s.length);
}

/** Center a string within `width` characters */
function center(s: string, width: number): string {
    if (s.length >= width) return s.slice(0, width);
    const left = Math.floor((width - s.length) / 2);
    const right = width - s.length - left;
    return ' '.repeat(left) + s + ' '.repeat(right);
}

/**
 * CC-style welcome banner — fully manual border rendering.
 * Every character position is computed from terminal width.
 */
export const Banner: React.FC<BannerProps> = ({
    version,
    model,
    tools,
    cwd,
    recentActivity = [],
}) => {
    const displayCwd = cwd || process.cwd().replace(process.env.HOME || '', '~');
    const shortModel = model.split('/').pop() || model;
    const provider = model.includes('qwen') ? 'DashScope' : 'API Usage Billing';
    const termW = process.stdout.columns || 80;

    // Layout: │ <border=1> <padding=1> <leftCol> <pad=1> │ <pad=1> <rightCol> <padding=1> │
    // inner = termW - 2 (borders)
    const innerW = termW - 2;
    const leftW = Math.floor(innerW * 0.4);
    const rightW = innerW - leftW - 1; // -1 for the center │

    // Build the title bar
    const title = ` NGOClaw v${version} `;
    const topDashLen = Math.max(0, termW - 2 - 4 - title.length); // 2=corners, 4="─── "
    const topLine = `╭─── ${title}${'─'.repeat(topDashLen)}╮`;
    const botLine = `╰${'─'.repeat(termW - 2)}╯`;

    // Left column lines (centered)
    const leftLines = [
        center('Welcome back!', leftW),
        center('', leftW),
        center('╱╲  ╱╲', leftW),
        center('╱  ╲╱  ╲', leftW),
        center('╱────────╲', leftW),
        center(`${shortModel} · ${provider}`, leftW),
        center(displayCwd, leftW),
    ];

    // Right column lines (left-aligned)
    const rightLines = [
        pad('Tips for getting started', rightW),
        pad('Type a message or use /help for commands', rightW),
        pad('─'.repeat(Math.min(rightW, rightW)), rightW),
        pad('Recent activity', rightW),
    ];

    if (recentActivity.length > 0) {
        for (const item of recentActivity.slice(0, 3)) {
            rightLines.push(pad(item, rightW));
        }
    } else {
        rightLines.push(pad('No recent activity', rightW));
    }

    // Pad both columns to same height
    const maxRows = Math.max(leftLines.length, rightLines.length);
    while (leftLines.length < maxRows) leftLines.push(' '.repeat(leftW));
    while (rightLines.length < maxRows) rightLines.push(' '.repeat(rightW));

    return (
        <Box flexDirection="column">
            <Text dimColor>{topLine}</Text>
            {leftLines.map((leftStr, i) => (
                <Text key={i}>
                    <Text dimColor>│</Text>
                    <Text>{leftStr}</Text>
                    <Text dimColor>│</Text>
                    <Text>{rightLines[i]}</Text>
                    <Text dimColor>│</Text>
                </Text>
            ))}
            <Text dimColor>{botLine}</Text>
        </Box>
    );
};
