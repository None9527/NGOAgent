import React, { useState, useCallback } from 'react';
import { Box, Text, useInput } from 'ink';
import { completeCommand } from '../commands/index.js';
import { ViInput, type ViMode } from './ViInput.js';
import { Spinner } from './Spinner.js';

interface InputAreaProps {
    onSubmit: (text: string) => void;
    isStreaming: boolean;
    onCancel: () => void;
}

export const InputArea: React.FC<InputAreaProps> = ({
    onSubmit,
    isStreaming,
    onCancel,
}) => {
    const [value, setValue] = useState('');
    const [history, setHistory] = useState<string[]>([]);
    const [historyIdx, setHistoryIdx] = useState(-1);
    const [completions, setCompletions] = useState<string[]>([]);
    const [multiLine, setMultiLine] = useState(false);
    const [multiLineBuf, setMultiLineBuf] = useState<string[]>([]);
    const [viMode, setViMode] = useState<ViMode>('INSERT');

    useInput((input, key) => {
        // Ctrl+C to interrupt streaming or multiline (only if handled here as fallback)
        if (key.ctrl && input === 'c') {
            if (isStreaming) {
                onCancel();
            } else if (multiLine) {
                setMultiLine(false);
                setMultiLineBuf([]);
                setValue('');
            }
            return;
        }
    });

    const handleArrowUp = useCallback(() => {
        if (!multiLine && history.length > 0) {
            const newIdx = Math.min(historyIdx + 1, history.length - 1);
            setHistoryIdx(newIdx);
            setValue(history[history.length - 1 - newIdx] || '');
        }
    }, [multiLine, history, historyIdx]);

    const handleArrowDown = useCallback(() => {
        if (!multiLine) {
            const newIdx = Math.max(historyIdx - 1, -1);
            setHistoryIdx(newIdx);
            setValue(newIdx < 0 ? '' : (history[history.length - 1 - newIdx] || ''));
        }
    }, [multiLine, history, historyIdx]);

    const handleTab = useCallback(() => {
        if (value.startsWith('/')) {
            const matches = completeCommand(value);
            if (matches.length === 1) {
                setValue(matches[0] + ' ');
                setCompletions([]);
            } else if (matches.length > 1) {
                setCompletions(matches);
            }
        }
    }, [value]);

    // Clear completions when value changes unless it's a tab match
    React.useEffect(() => {
        if (completions.length > 0) setCompletions([]);
    }, [value]);

    const handleSubmit = useCallback((text: string) => {
        const trimmed = text.trim();

        // Multi-line mode: backtick toggle (CC pattern)
        if (!multiLine && trimmed.startsWith('`') && !trimmed.endsWith('`')) {
            setMultiLine(true);
            setMultiLineBuf([trimmed.slice(1)]);
            setValue('');
            return;
        }
        if (multiLine) {
            if (trimmed.endsWith('`')) {
                const lastLine = trimmed.slice(0, -1);
                const full = [...multiLineBuf, lastLine].join('\n');
                setMultiLine(false);
                setMultiLineBuf([]);
                if (full.trim()) {
                    setHistory((prev) => [...prev, full.trim()]);
                    setHistoryIdx(-1);
                    onSubmit(full.trim());
                }
            } else {
                setMultiLineBuf((prev) => [...prev, text]);
            }
            setValue('');
            return;
        }

        if (!trimmed) return;
        setHistory((prev) => [...prev, trimmed]);
        setHistoryIdx(-1);
        setValue('');
        onSubmit(trimmed);
    }, [onSubmit, multiLine, multiLineBuf]);

    if (isStreaming) {
        return (
            <Box flexDirection="column">
                <Spinner isActive={true} />
                <Box paddingTop={1}>
                    <Text dimColor>esc to interrupt</Text>
                </Box>
            </Box>
        );
    }

    return (
        <Box flexDirection="column">
            {/* Tab completion suggestions */}
            {completions.length > 0 && (
                <Box>
                    <Text color="gray">  {completions.join('  ')}</Text>
                </Box>
            )}

            {/* Multi-line mode indicator */}
            {multiLine && multiLineBuf.length > 0 && (
                <Box flexDirection="column">
                    {multiLineBuf.map((line, i) => (
                        <Text key={i} color="gray">  … {line}</Text>
                    ))}
                </Box>
            )}

            <Box>
                <Text color={viMode === 'NORMAL' ? 'yellow' : 'cyan'} bold>
                    {viMode === 'NORMAL' ? '[N] ' : '❯ '}
                </Text>
                <ViInput
                    value={value}
                    onChange={setValue}
                    onSubmit={handleSubmit}
                    onModeChange={setViMode}
                    placeholder={multiLine ? "End with ` to submit" : "Type your message..."}
                    isStreaming={isStreaming}
                    onCancel={onCancel}
                    onArrowUp={handleArrowUp}
                    onArrowDown={handleArrowDown}
                    onTab={handleTab}
                />
            </Box>
        </Box>
    );
};
