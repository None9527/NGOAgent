import React, { useMemo } from 'react';
import { Box, Text } from 'ink';
import { marked } from 'marked';
import { markedTerminal } from 'marked-terminal';

import chalk from 'chalk';

// Configure marked-terminal with a refined color palette (no red+green clash)
marked.use(markedTerminal({
    // Headings: bold cyan
    heading: chalk.bold.cyan,
    // Inline code: magenta on dark bg
    codespan: chalk.magenta,
    // Code blocks: gray border
    code: chalk.gray,
    // Block quotes: dim italic
    blockquote: chalk.gray.italic,
    // Links: underline blue
    href: chalk.underline.blueBright,
    // Bold: white bold
    strong: chalk.bold.white,
    // Italic: italic gray
    em: chalk.italic.yellowBright,
    // List bullets: cyan
    listitem: chalk.white,
    // Tables: white
    table: chalk.white,
    // Horizontal rule
    hr: chalk.gray,
    // First heading (h1)
    firstHeading: chalk.bold.cyanBright,
    // Paragraphs
    paragraph: chalk.white,
    // Strikethrough 
    del: chalk.dim.strikethrough,
}) as any);

interface MarkdownProps {
    children: string;
}

/**
 * A simple Markdown renderer for Ink.
 * Uses `marked-terminal` to parse and render to ANSI strings, then outputs via Ink Text.
 * This ensures all markdown features (tables, quotes, headings) are properly formatted in terminal.
 */
export const Markdown: React.FC<MarkdownProps> = ({ children }) => {
    // We intentionally don't try to parse very small partials that stream in
    const rendered = useMemo(() => {
        try {
            // marked returns a string with ANSI codes when marked-terminal is used
            const parsed = marked.parse(children) as string;
            // markedTerminal usually adds an extra newline at the end of block elements
            return parsed.replace(/\n$/, '');
        } catch {
            return children;
        }
    }, [children]);

    return (
        <Box flexDirection="column" marginBottom={1}>
            <Text>{rendered}</Text>
        </Box>
    );
};
