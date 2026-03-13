/**
 * App.tsx — Provider Shell + thin AppContent component.
 *
 * All state lives in 3 contexts:
 *   - ConfigContext: connection, model, version, stats, approval mode
 *   - ChatContext: history, pending, streaming, event processing
 *   - UIContext: app state machine, selector, diffs, mode
 *
 * AppContent is a pure render layer that reads context and dispatches actions.
 */
import React, { useCallback, useEffect } from 'react';
import { Box, Text, useApp, useInput } from 'ink';
import { ConfigProvider, useConfig, ChatProvider, useChat, UIProvider, useUI } from '../contexts/index.js';
import { Banner } from './Banner.js';
import { StatusBar } from './StatusBar.js';
import { InputArea } from './InputArea.js';
import { MessageList, genId } from './MessageList.js';
import { PermissionRequest } from './PermissionRequest.js';
import { Selector } from './Selector.js';
import { HelpDialog } from './HelpDialog.js';
import { MessageSelectorDialog } from './MessageSelectorDialog.js';
import { DiffDialog } from './DiffDialog.js';
import { slashCommands, findCommand, getCommandArgs, completeCommand } from '../commands/index.js';

interface AppProps {
    serverAddr?: string;
}

/**
 * App: Provider Shell — wraps AppContent in the 3 context providers.
 */
export const App: React.FC<AppProps> = ({ serverAddr = 'localhost:50051' }) => {
    return (
        <ConfigProvider serverAddr={serverAddr}>
            <UIProvider>
                <ChatProvider>
                    <AppContent />
                </ChatProvider>
            </UIProvider>
        </ConfigProvider>
    );
};

/**
 * AppContent: Thin render component — reads context, dispatches actions.
 * ~150 lines vs the original ~480 line monolith.
 */
const AppContent: React.FC = () => {
    const { exit } = useApp();
    const config = useConfig();
    const chat = useChat();
    const ui = useUI();

    // Push banner once ready
    useEffect(() => {
        if (config.ready) {
            chat.pushBanner(config.version, config.model, config.tools);
        }
    }, [config.ready]);

    // ─── Keyboard shortcuts ───
    useInput((input, key) => {
        if (key.ctrl && input === 'c') {
            if (chat.isStreaming) {
                chat.cancelStream();
                ui.setAppState('idle');
                return;
            }
            exit();
        }
        if (key.ctrl && input === 'l') {
            process.stdout.write('\x1b[2J\x1b[H');
            chat.remountStatic();
        }
        if (key.ctrl && input === 'd') exit();
        if (key.ctrl && input === '\\') {
            if (ui.appState === 'idle') handleSubmit('/model');
        }
        if (key.ctrl && input === 'r') {
            if (ui.appState === 'idle') handleSubmit('/history');
        }
        if (key.ctrl && key.upArrow) {
            if (ui.appState === 'idle') ui.setAppState('history');
        }
    });

    // ─── Command callbacks (for slash commands) ───
    const cmdCallbacks = {
        setMode: (m: string) => ui.setMode(m),
        setModel: (m: string) => config.setModel(m),
        clearMessages: () => chat.clearHistory(),
        exit,
    };

    // ─── Selector handlers ───
    const handleSelectorSelect = useCallback(async (value: string) => {
        ui.closeSelector();
        if (!config.client) return;

        const cmd = findCommand(ui.selectorCmd);
        if (cmd) {
            try {
                const result = await cmd.execute(config.client, value, cmdCallbacks);
                if (result) chat.pushHistory('assistant', result);
            } catch (err: any) {
                chat.pushHistory('error', err.message);
            }
        }
    }, [ui.selectorCmd, config.client, cmdCallbacks]);

    // ─── Main submit handler ───
    const handleSubmit = useCallback(async (text: string) => {
        if (!config.client || ui.appState !== 'idle') return;

        const trimmed = text.trim();

        // Test diff overlay
        if (trimmed === '/testdiff') {
            ui.openDiffs([
                { file: 'src/main.ts', diffText: '@@ -1,2 +1,3 @@\n+import * as fs from "fs";\n console.log("Init");\n-const a = 1;' },
                { file: 'package.json', diffText: '@@ -10,3 +10,4 @@\n   "dependencies": {\n+    "ink": "^4.0.0",\n     "react": "^18.0.0"\n   }' }
            ]);
            return;
        }

        // Slash command?
        if (text.startsWith('/')) {
            const cmd = findCommand(text);
            const args = getCommandArgs(text);

            if (cmd?.name === '/help') { ui.setAppState('help'); return; }

            if (cmd) {
                if (cmd.hasSelector && !args) {
                    let items = cmd.options || [];
                    if (cmd.name === '/model') {
                        try {
                            const res = await config.client.listModels();
                            items = res.models.map((m: any) => ({
                                label: m.id, value: m.id,
                                description: m.provider, current: m.id === res.currentModel,
                            }));
                        } catch { items = []; }
                    }
                    if (cmd.name === '/mode') {
                        items = items.map(it => ({ ...it, current: it.value === ui.mode }));
                    }
                    ui.openSelector(cmd.description, items, cmd.name);
                    return;
                }
                try {
                    const result = await cmd.execute(config.client, args, cmdCallbacks);
                    if (result) chat.pushHistory('assistant', result);
                } catch (err: any) {
                    chat.pushHistory('error', err.message);
                }
                return;
            }
        }

        // Regular chat message
        ui.setAppState('streaming');
        chat.startChat(text);
    }, [config.client, ui.appState, ui.mode, cmdCallbacks]);

    // Sync streaming state back to UI
    useEffect(() => {
        if (!chat.isStreaming && ui.appState === 'streaming') {
            ui.setAppState('idle');
        }
        if (chat.permReq && ui.appState !== 'approving') {
            ui.setAppState('approving');
        }
    }, [chat.isStreaming, chat.permReq, ui.appState]);

    // ─── Render ───

    if (config.connError) {
        return (
            <Box flexDirection="column" padding={1}>
                <Text color="red" bold>❌ {config.connError}</Text>
            </Box>
        );
    }
    if (!config.ready) {
        return (
            <Box padding={1}>
                <Text color="gray">⏳ Connecting to server...</Text>
            </Box>
        );
    }

    const isFullScreen = ui.appState === 'diffing' || ui.appState === 'history';

    return (
        <Box flexDirection="column" height={isFullScreen ? process.stdout.rows - 1 : undefined}>
            {/* Messages */}
            <Box flexGrow={1} overflowY="hidden" flexDirection="column">
                <MessageList history={chat.history} pending={chat.pending} staticRemountKey={chat.staticRemountKey} />
            </Box>

            {/* Help dialog */}
            {ui.appState === 'help' && <HelpDialog onClose={() => ui.setAppState('idle')} />}

            {/* Permission UI */}
            {ui.appState === 'approving' && chat.permReq && (
                <PermissionRequest
                    toolName={chat.permReq.toolName}
                    toolInput={chat.permReq.toolInput}
                    reason={chat.permReq.reason}
                    approvalMode={config.approvalMode}
                    onCycleMode={config.cycleApprovalMode}
                    onDecision={async (choice) => {
                        const approved = choice === 'allow_once' || choice === 'always_allow';
                        await chat.resolveApproval(approved);
                        ui.setAppState('streaming');
                    }}
                />
            )}

            {/* Selector UI */}
            {ui.appState === 'selecting' && (
                <Selector
                    title={ui.selectorTitle}
                    items={ui.selectorItems}
                    selectedIndex={ui.selectorIndex}
                    onSelect={handleSelectorSelect}
                    onCancel={() => ui.closeSelector()}
                    onMove={ui.setSelectorIndex}
                />
            )}

            {/* Message Selector */}
            {ui.appState === 'history' && (
                <MessageSelectorDialog
                    messages={[...chat.history, ...chat.pending]}
                    onSelect={(text) => { ui.setAppState('idle'); handleSubmit(text); }}
                    onCancel={() => ui.setAppState('idle')}
                />
            )}

            {/* Diff UI */}
            {ui.appState === 'diffing' && (
                <DiffDialog diffs={ui.diffs} onClose={() => ui.setAppState('idle')} />
            )}

            {/* Status Bar */}
            <StatusBar
                model={config.model}
                mode={ui.mode}
                isStreaming={chat.isStreaming}
                inputTokens={config.stats.inputTokens}
                outputTokens={config.stats.outputTokens}
                contextUsedPct={config.stats.maxTokens > 0 ? Math.round(config.stats.tokenCount / config.stats.maxTokens * 100) : 0}
                costUsd={config.stats.costUsd}
            />

            {/* Input */}
            <InputArea
                onSubmit={handleSubmit}
                isStreaming={ui.appState !== 'idle'}
                onCancel={() => {
                    chat.cancelStream();
                    ui.setAppState('idle');
                }}
            />

            <Text dimColor>  ? for shortcuts  esc for vi mode</Text>
        </Box>
    );
};
