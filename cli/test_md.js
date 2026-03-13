import { marked } from 'marked';
import markedTerminal from 'marked-terminal';

marked.use(markedTerminal());
console.log(marked.parse('# Hello World\n\n| A | B |\n|---|---|\n| 1 | 2 |'));
