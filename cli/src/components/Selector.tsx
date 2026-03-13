import React from 'react';
import { Box, Text, useInput } from 'ink';

interface SelectorItem {
    label: string;
    value: string;
    description?: string;
    current?: boolean;
}

interface SelectorProps {
    title: string;
    items: SelectorItem[];
    selectedIndex: number;
    onSelect: (value: string) => void;
    onCancel: () => void;
    onMove: (index: number) => void;
}

/**
 * Interactive selector for slash command arguments.
 * Handles Up/Down arrow keys, Enter to confirm, Esc to cancel.
 */
export const Selector: React.FC<SelectorProps> = ({
    title,
    items,
    selectedIndex,
    onSelect,
    onCancel,
    onMove,
}) => {
    useInput((_input, key) => {
        if (key.upArrow) {
            onMove(Math.max(0, selectedIndex - 1));
        } else if (key.downArrow) {
            onMove(Math.min(items.length - 1, selectedIndex + 1));
        } else if (key.return) {
            const item = items[selectedIndex];
            if (item) onSelect(item.value);
        } else if (key.escape) {
            onCancel();
        }
    });

    return (
        <Box flexDirection="column">
            <Text color="cyan" bold>{title}</Text>
            {items.map((item, i) => {
                const isSelected = i === selectedIndex;
                const marker = item.current ? ' ◀' : '';
                return (
                    <Box key={item.value}>
                        <Text color={isSelected ? 'green' : 'white'}>
                            {isSelected ? '❯ ' : '  '}
                            {item.label}{marker}
                        </Text>
                        {item.description && (
                            <Text color="gray"> — {item.description}</Text>
                        )}
                    </Box>
                );
            })}
            <Text color="gray" dimColor>
                ↑↓ navigate  ⏎ select  Esc cancel
            </Text>
        </Box>
    );
};
