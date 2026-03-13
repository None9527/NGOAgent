import React from 'react';
import { Box, Text, useInput } from 'ink';
import { slashCommands } from '../commands/index.js';

/**
 * CC-style help dialog — bordered, Esc to close.
 * Shows all commands + keyboard shortcuts.
 */

interface HelpDialogProps {
    onClose: () => void;
}

export const HelpDialog: React.FC<HelpDialogProps> = ({ onClose }) => {
    useInput((_input, key) => {
        if (key.escape || key.return) {
            onClose();
        }
    });

    const shortcuts = [
        { key: 'Ctrl+C', desc: 'Interrupt / double-tap exit' },
        { key: 'Ctrl+D', desc: 'Exit' },
        { key: 'Ctrl+L', desc: 'Clear screen' },
        { key: '↑ / ↓', desc: 'History navigation' },
        { key: 'Ctrl+Up', desc: 'Message selector (History)' },
        { key: 'Tab', desc: 'Autocomplete (/ commands)' },
        { key: '` start', desc: 'Multi-line input' },
        { key: 'Ctrl+E', desc: 'Toggle permission details' },
        { key: 'Esc', desc: 'Cancel / close dialog / Normal mode' },
    ];

    return (
        <Box
            flexDirection="column"
            borderStyle="round"
            borderColor="cyan"
            paddingX={1}
            paddingY={0}
        >
            {/* Commands */}
            <Box marginBottom={1}>
                <Text color="cyan" bold>◇ Commands</Text>
            </Box>
            {slashCommands.map((cmd) => (
                <Box key={cmd.name}>
                    <Text color="green">{cmd.name.padEnd(18)}</Text>
                    <Text color="gray">{cmd.description}</Text>
                </Box>
            ))}

            {/* Shortcuts */}
            <Box marginTop={1} marginBottom={1}>
                <Text color="cyan" bold>◇ Shortcuts</Text>
            </Box>
            {shortcuts.map((s) => (
                <Box key={s.key}>
                    <Text color="green">{s.key.padEnd(18)}</Text>
                    <Text color="gray">{s.desc}</Text>
                </Box>
            ))}

            <Box marginTop={1}>
                <Text color="gray" dimColor>Press Esc or Enter to close</Text>
            </Box>
        </Box>
    );
};
