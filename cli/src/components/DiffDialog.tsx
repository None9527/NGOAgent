import React, { useState } from 'react';
import { Box, Text, useInput } from 'ink';

export interface FileDiff {
    file: string;
    diffText: string;
}

interface DiffDialogProps {
    diffs: FileDiff[];
    onClose: () => void;
}

/**
 * A full-screen overlay to display file diffs.
 * Uses Left/Right to switch between files.
 */
export const DiffDialog: React.FC<DiffDialogProps> = ({ diffs, onClose }) => {
    const [fileIdx, setFileIdx] = useState(0);

    useInput((ch, key) => {
        if (key.escape || (key.ctrl && ch === 'c')) {
            onClose();
            return;
        }

        if (key.leftArrow || ch === 'h') {
            setFileIdx((p) => Math.max(0, p - 1));
        } else if (key.rightArrow || ch === 'l') {
            setFileIdx((p) => Math.min(diffs.length - 1, p + 1));
        }
    });

    if (!diffs || diffs.length === 0) {
        return (
            <Box borderStyle="round" borderColor="blue" paddingX={1} flexDirection="column">
                <Text color="yellow">No diffs available to display.</Text>
                <Text color="gray" dimColor>Press Esc to close.</Text>
            </Box>
        );
    }

    const currentDiff = diffs[fileIdx];
    const lines = currentDiff.diffText.split('\n');
    // Simple rendering, max 20 lines to prevent overflow
    const previewLines = lines.slice(0, 20);
    const hiddenCount = lines.length - 20;

    return (
        <Box borderStyle="double" borderColor="magenta" paddingX={1} flexDirection="column">
            {/* Header */}
            <Box justifyContent="space-between" marginBottom={1}>
                <Text color="magenta" bold>
                    {` 🔍 Reviewing Changes (${fileIdx + 1}/${diffs.length}) `}
                </Text>
                <Text color="gray" dimColor>
                    ←/→: prev/next file, Esc: close
                </Text>
            </Box>

            {/* File name */}
            <Box marginBottom={1}>
                <Text color="white" bold>File: </Text>
                <Text color="cyan">{currentDiff.file}</Text>
            </Box>

            {/* Diff content */}
            <Box flexDirection="column" marginLeft={2}>
                {previewLines.map((line, i) => {
                    let color = 'white';
                    let bg = undefined;
                    if (line.startsWith('+')) {
                        color = 'green';
                    } else if (line.startsWith('-')) {
                        color = 'red';
                    } else if (line.startsWith('@@')) {
                        color = 'cyan';
                    }
                    return (
                        <Text key={i} color={color as any} backgroundColor={bg}>
                            {line}
                        </Text>
                    );
                })}
                {hiddenCount > 0 && (
                    <Text color="gray" dimColor>... {hiddenCount} more lines not shown</Text>
                )}
            </Box>
        </Box>
    );
};
