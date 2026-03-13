import React, { useState } from 'react';
import { Box, Text, useInput } from 'ink';
import type { MessageBlock } from './MessageList.js';

interface MessageSelectorProps {
    messages: MessageBlock[];
    onSelect: (content: string) => void;
    onCancel: () => void;
}

export const MessageSelectorDialog: React.FC<MessageSelectorProps> = ({ messages, onSelect, onCancel }) => {
    // Filter to only user and assistant messages that have content
    const navigable = messages.filter(m => (m.role === 'user' || m.role === 'assistant') && m.content.trim());
    const [selectedIndex, setSelectedIndex] = useState(Math.max(0, navigable.length - 1));

    useInput((ch, key) => {
        if (key.escape) {
            onCancel();
            return;
        }

        if (key.return) {
            if (navigable.length > 0) {
                onSelect(navigable[selectedIndex].content);
            } else {
                onCancel();
            }
            return;
        }

        if (key.upArrow || ch === 'k' || ch === 'K') { // supports k/K for vi
            setSelectedIndex(prev => Math.max(0, prev - 1));
        } else if (key.downArrow || ch === 'j' || ch === 'J') {
            setSelectedIndex(prev => Math.min(navigable.length > 0 ? navigable.length - 1 : 0, prev + 1));
        } else if ((key.ctrl && key.upArrow) || ch === 'g') {
            setSelectedIndex(0);
        } else if ((key.ctrl && key.downArrow) || ch === 'G') {
            setSelectedIndex(Math.max(0, navigable.length - 1));
        }
    });

    if (navigable.length === 0) {
        return (
            <Box borderStyle="round" borderColor="yellow" paddingX={1} flexDirection="column">
                <Text color="yellow">No message history available to select.</Text>
                <Text color="gray" dimColor>Press Esc to return.</Text>
            </Box>
        );
    }

    // Show roughly 5 items
    const maxVisible = 5;
    let startIdx = selectedIndex - Math.floor(maxVisible / 2);
    if (startIdx < 0) startIdx = 0;
    if (startIdx + maxVisible > navigable.length) {
        startIdx = Math.max(0, navigable.length - maxVisible);
    }
    const visibleItems = navigable.slice(startIdx, startIdx + maxVisible);

    return (
        <Box borderStyle="double" borderColor="cyan" paddingX={1} flexDirection="column">
            <Box marginBottom={1}>
                <Text color="cyan" bold>◇ Select Message to Copy (↑/↓/j/k to move, Enter to copy, Esc to cancel)</Text>
            </Box>
            {visibleItems.map((m, i) => {
                const realIdx = startIdx + i;
                const isSelected = realIdx === selectedIndex;
                const prefix = m.role === 'user' ? '❯ ' : '⬡ ';
                const color = m.role === 'user' ? 'blue' : 'white';

                // Truncate long messages
                const content = m.content.split('\n')[0].slice(0, 100) + (m.content.length > 100 ? '...' : '');

                return (
                    <Box key={realIdx}>
                        <Text color={isSelected ? 'yellow' : color} inverse={isSelected} bold={isSelected}>
                            {isSelected ? '▶ ' : '  '}
                            {prefix}{content}
                        </Text>
                    </Box>
                );
            })}
        </Box>
    );
};
