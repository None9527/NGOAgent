#!/usr/bin/env node
import React from 'react';
import { render } from 'ink';
import { App } from './components/App.js';

// Parse --server flag (default: http://localhost:8080)
const args = process.argv.slice(2);
let serverAddr = 'http://localhost:19996';

for (let i = 0; i < args.length; i++) {
    if (args[i] === '--server' && args[i + 1]) {
        serverAddr = args[i + 1];
        i++;
    }
}

// Clear screen on startup to remove old commands
console.clear();

// Render the Ink app (inline mode like CC — no alt-screen)
render(React.createElement(App, { serverAddr }));
