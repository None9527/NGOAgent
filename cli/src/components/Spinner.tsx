import React, { useState, useEffect } from 'react';
import { Text } from 'ink';

/** CC-style random animation text with random prefix symbols */
const ANIMATIONS = [
    'Thinking…', 'Pondering…', 'Percolating…', 'Crystallizing…',
    'Illuminating…', 'Synthesizing…', 'Undulating…', 'Unfurling…',
    'Incubating…', 'Spiraling…',
];

const PREFIXES = ['●', '✶', '✢', '·', '◆', '○', '◇', '★'];

interface SpinnerProps {
    isActive: boolean;
}

export const Spinner: React.FC<SpinnerProps> = ({ isActive }) => {
    const [text, setText] = useState(ANIMATIONS[0]);
    const [prefix, setPrefix] = useState(PREFIXES[0]);

    useEffect(() => {
        if (!isActive) return;
        const pick = () => {
            setText(ANIMATIONS[Math.floor(Math.random() * ANIMATIONS.length)]);
            setPrefix(PREFIXES[Math.floor(Math.random() * PREFIXES.length)]);
        };
        pick();
        const timer = setInterval(pick, 2500);
        return () => clearInterval(timer);
    }, [isActive]);

    if (!isActive) return null;

    return (
        <Text>
            <Text color="cyan">{prefix} </Text>
            <Text color="gray">{text}</Text>
        </Text>
    );
};
