import React, { useState, useMemo } from 'react';
import { Box, Text, useInput } from 'ink';
import { slashCommands } from '../commands/index.js';

/**
 * CC-style autocomplete popup — shows when typing `/`.
 * ↑/↓ to navigate, Tab/Enter to accept, Esc to dismiss.
 */

interface AutocompleteProps {
    input: string;
    onAccept: (value: string) => void;
    onDismiss: () => void;
    visible: boolean;
}

export const Autocomplete: React.FC<AutocompleteProps> = ({
    input,
    onAccept,
    onDismiss,
    visible,
}) => {
    const [selectedIndex, setSelectedIndex] = useState(0);

    const matches = useMemo(() => {
        if (!input.startsWith('/')) return [];
        const partial = input.toLowerCase();
        return slashCommands.filter((c) => c.name.startsWith(partial));
    }, [input]);

    // Reset index when matches change
    useMemo(() => {
        setSelectedIndex(0);
    }, [matches.length]);

    useInput((ch, key) => {
        if (!visible || matches.length === 0) return;

        if (key.upArrow) {
            setSelectedIndex((prev) => (prev > 0 ? prev - 1 : matches.length - 1));
        } else if (key.downArrow) {
            setSelectedIndex((prev) => (prev < matches.length - 1 ? prev + 1 : 0));
        } else if (key.tab || key.return) {
            onAccept(matches[selectedIndex].name + ' ');
        } else if (key.escape) {
            onDismiss();
        }
    });

    if (!visible || matches.length === 0) return null;

    // Show max 8 items
    const maxVisible = 8;
    const startIdx = Math.max(0, Math.min(selectedIndex - 3, matches.length - maxVisible));
    const visibleItems = matches.slice(startIdx, startIdx + maxVisible);

    return (
        <Box
            flexDirection="column"
            borderStyle="round"
            borderColor="gray"
            paddingX={1}
            marginLeft={2}
        >
            {visibleItems.map((cmd, i) => {
                const realIdx = startIdx + i;
                const isSelected = realIdx === selectedIndex;
                return (
                    <Box key={cmd.name}>
                        <Text
                            color={isSelected ? 'cyan' : 'white'}
                            bold={isSelected}
                            inverse={isSelected}
                        >
                            {' '}{cmd.name.padEnd(16)}{' '}
                        </Text>
                        <Text color="gray"> {cmd.description}</Text>
                    </Box>
                );
            })}
            {matches.length > maxVisible && (
                <Text color="gray" dimColor>
                    {' '}...{matches.length - maxVisible} more
                </Text>
            )}
        </Box>
    );
};
