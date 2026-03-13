import React, { useState } from 'react';
import { Box, Text, useInput } from 'ink';

/**
 * 2-state permission request — 2 options:
 *   y = Allow (tool executes)
 *   n = Deny  (tool is denied, agent gets USER_DENIED)
 *   Ctrl+E = Toggle detail view
 */

export type PermissionChoice = 'allow_once' | 'always_allow' | 'deny' | 'always_deny';

interface PermissionRequestProps {
    toolName: string;
    toolInput: string;
    reason: string;
    approvalMode: string;
    onCycleMode: () => void;
    onDecision: (choice: PermissionChoice) => void;
}

export const PermissionRequest: React.FC<PermissionRequestProps> = ({
    toolName,
    toolInput,
    reason,
    approvalMode,
    onCycleMode,
    onDecision,
}) => {
    const [decided, setDecided] = useState(false);
    const [showDetail, setShowDetail] = useState(false);

    useInput((input, key) => {
        if (decided) return;

        // Ctrl+E toggle details
        if (key.ctrl && input === 'e') {
            setShowDetail((prev) => !prev);
            return;
        }

        // Shift+Tab cycle modes
        if (key.tab && key.shift) {
            onCycleMode();
            return;
        }

        const lower = input.toLowerCase();

        if (lower === 'y') { setDecided(true); onDecision('allow_once'); }
        else if (lower === 'n' || key.escape) { setDecided(true); onDecision('deny'); }
    });

    // Shell command rendering
    const isShell = toolName.toLowerCase().includes('bash') || toolName.toLowerCase().includes('command');
    const preview = showDetail
        ? toolInput
        : toolInput.length > 120 ? toolInput.slice(0, 117) + '...' : toolInput;

    return (
        <Box
            flexDirection="column"
            borderStyle="round"
            borderColor="yellow"
            paddingX={1}
        >
            {/* Header */}
            <Box marginBottom={1} justifyContent="space-between">
                <Text color="yellow" bold>⚠ Permission Required</Text>
                <Box>
                    <Text color="gray">Mode: </Text>
                    <Text color="magenta" bold>{approvalMode}</Text>
                    <Text color="gray" dimColor> (⇧Tab to cycle)  Ctrl+E details</Text>
                </Box>
            </Box>

            {/* Tool info */}
            <Box>
                <Text color="white" bold>Tool: </Text>
                <Text color="cyan" bold>{toolName}</Text>
            </Box>

            {reason && (
                <Box>
                    <Text color="white" bold>Why:  </Text>
                    <Text color="gray">{reason}</Text>
                </Box>
            )}

            {/* Command/args preview */}
            <Box marginTop={1} flexDirection="column">
                {isShell ? (
                    <Text color="white">
                        <Text color="green">$ </Text>
                        {preview}
                    </Text>
                ) : (
                    <Text color="gray">{preview}</Text>
                )}
            </Box>

            {/* Action buttons */}
            <Box marginTop={1}>
                <Text>
                    <Text color="green" bold>y</Text><Text color="gray"> Allow  </Text>
                    <Text color="red" bold>n</Text><Text color="gray"> Deny  </Text>
                    <Text color="gray" dimColor>Esc = Deny</Text>
                </Text>
            </Box>
        </Box>
    );
};
