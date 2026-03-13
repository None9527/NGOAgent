import React, { useState, useCallback, useEffect } from 'react';
import { Box, Text, useInput } from 'ink';

export type ViMode = 'INSERT' | 'NORMAL';

export interface ViInputProps {
    value: string;
    onChange: (val: string) => void;
    onSubmit: (val: string) => void;
    onModeChange?: (mode: ViMode) => void;
    placeholder?: string;
    isStreaming?: boolean;
    onCancel?: () => void;
    onArrowUp?: () => void;
    onArrowDown?: () => void;
    onTab?: () => void;
}

export const ViInput: React.FC<ViInputProps> = ({
    value,
    onChange,
    onSubmit,
    onModeChange,
    placeholder = '',
    isStreaming = false,
    onCancel,
    onArrowUp,
    onArrowDown,
    onTab,
}) => {
    const [mode, setMode] = useState<ViMode>('INSERT');
    const [cursor, setCursor] = useState(0);

    // Sync cursor with external value changes
    useEffect(() => {
        if (cursor > value.length) setCursor(value.length);
    }, [value, cursor]);

    // Notify parent of mode changes
    useEffect(() => {
        if (onModeChange) onModeChange(mode);
    }, [mode, onModeChange]);

    useInput((ch, key) => {
        if (isStreaming) {
            if (key.ctrl && ch === 'c' && onCancel) {
                onCancel();
            }
            return;
        }

        // --- GLOBAL KEYS ---
        if (key.escape) {
            setMode('NORMAL');
            setCursor((prev) => Math.max(0, prev - 1)); // Vi normalizes cursor to char, not after char
            return;
        }

        if (key.ctrl && ch === 'c') {
            // Usually exit handled by App, but if empty, process it
            return;
        }

        if (key.upArrow && onArrowUp) { onArrowUp(); return; }
        if (key.downArrow && onArrowDown) { onArrowDown(); return; }
        if (key.tab && onTab) { onTab(); return; }

        // --- NORMAL MODE ---
        if (mode === 'NORMAL') {
            if (key.return) {
                onSubmit(value);
                setCursor(0);
                return;
            }
            if (key.leftArrow || ch === 'h') {
                setCursor((p) => Math.max(0, p - 1));
            } else if (key.rightArrow || ch === 'l') {
                setCursor((p) => Math.min(value.length > 0 ? value.length - 1 : 0, p + 1));
            } else if (ch === 'i') {
                setMode('INSERT');
            } else if (ch === 'a') {
                setMode('INSERT');
                setCursor((p) => Math.min(value.length, p + 1));
            } else if (ch === 'A') {
                setMode('INSERT');
                setCursor(value.length);
            } else if (ch === 'I') {
                setMode('INSERT');
                setCursor(0);
            } else if (ch === '0') {
                setCursor(0);
            } else if (ch === '$') {
                setCursor(value.length > 0 ? value.length - 1 : 0);
            } else if (ch === 'w') {
                // simple word forward
                let newC = cursor;
                while (newC < value.length - 1 && value[newC] !== ' ') newC++; // end of word
                while (newC < value.length - 1 && value[newC] === ' ') newC++; // to next word
                setCursor(newC);
            } else if (ch === 'b') {
                // simple word backward
                let newC = cursor;
                if (newC > 0 && value[newC - 1] === ' ') newC--; // skip space
                while (newC > 0 && value[newC] === ' ') newC--;
                while (newC > 0 && value[newC - 1] !== ' ') newC--;
                setCursor(newC);
            } else if (ch === 'x') {
                // delete char under cursor
                if (value.length > 0) {
                    const nextVal = value.slice(0, cursor) + value.slice(cursor + 1);
                    onChange(nextVal);
                    if (cursor >= nextVal.length) setCursor(Math.max(0, nextVal.length - 1));
                }
            } else if (ch === 'S' || ch === 's') { // ch could be undefined if special key, but 'S' is string
                if (ch === 's') {
                    const nextVal = value.slice(0, cursor) + value.slice(cursor + 1);
                    onChange(nextVal);
                    setMode('INSERT');
                }
            }
            return;
        }

        // --- INSERT MODE ---
        if (mode === 'INSERT') {
            if (key.return) {
                onSubmit(value);
                setCursor(0);
                return;
            }

            if (key.leftArrow) {
                setCursor((p) => Math.max(0, p - 1));
            } else if (key.rightArrow) {
                setCursor((p) => Math.min(value.length, p + 1));
            } else if (key.backspace || key.delete) {
                if (cursor > 0) {
                    onChange(value.slice(0, cursor - 1) + value.slice(cursor));
                    setCursor((p) => p - 1);
                }
            } else if (ch) {
                onChange(value.slice(0, cursor) + ch + value.slice(cursor));
                setCursor((p) => p + ch.length);
            }
            return;
        }
    });

    if (isStreaming) {
        return <Text color="gray">Agent working... (Ctrl+C to interrupt)</Text>;
    }

    if (!value && placeholder) {
        return (
            <Text color="gray">
                <Text inverse={cursor === 0} color={mode === 'NORMAL' ? 'yellow' : 'white'}>{placeholder[0]}</Text>
                {placeholder.slice(1)}
            </Text>
        );
    }

    const before = value.slice(0, cursor);
    const at = value[cursor] || ' ';
    const after = value.slice(cursor + 1);

    return (
        <Text>
            {before}
            {/* The cursor block */}
            <Text inverse={true} color={mode === 'NORMAL' ? 'yellow' : 'white'}>
                {at}
            </Text>
            {after}
        </Text>
    );
};
