/**
 * UIContext — App state machine, selector dialogs, diff viewer.
 * Manages: AppState transitions, slash command selector, diff overlay.
 * Does NOT manage message data (ChatContext) or config (ConfigContext).
 */
import React, { createContext, useContext, useState, useCallback } from 'react';

// ═══════════════════════════════════════════
// Types
// ═══════════════════════════════════════════

/**
 * Application state machine (CC-style):
 *   idle      → User typing
 *   streaming → Agent responding
 *   selecting → Slash command selector open
 *   approving → Permission request pending
 *   help      → Help dialog open
 *   history   → Message selector open
 *   diffing   → Diff review open
 */
export type AppState = 'idle' | 'streaming' | 'selecting' | 'approving' | 'help' | 'history' | 'diffing';

export interface SelectorItem {
    label: string;
    value: string;
    description?: string;
    current?: boolean;
}

export interface FileDiff {
    file: string;
    diffText: string;
}

interface UIState {
    appState: AppState;
    // Selector
    selectorTitle: string;
    selectorItems: SelectorItem[];
    selectorIndex: number;
    selectorCmd: string;
    // Diffs
    diffs: FileDiff[];
    // Mode (planning mode display)
    mode: string;
}

interface UIActions {
    setAppState: (state: AppState) => void;
    setMode: (m: string) => void;
    /** Open a selector dialog for a slash command */
    openSelector: (title: string, items: SelectorItem[], cmd: string) => void;
    closeSelector: () => void;
    setSelectorIndex: (idx: number) => void;
    /** Open diff viewer */
    openDiffs: (diffs: FileDiff[]) => void;
}

type UIContextValue = UIState & UIActions;

// ═══════════════════════════════════════════
// Context
// ═══════════════════════════════════════════

const UIContext = createContext<UIContextValue | null>(null);

export function useUI(): UIContextValue {
    const ctx = useContext(UIContext);
    if (!ctx) throw new Error('useUI must be used within UIProvider');
    return ctx;
}

// ═══════════════════════════════════════════
// Provider
// ═══════════════════════════════════════════

interface UIProviderProps {
    children: React.ReactNode;
}

export const UIProvider: React.FC<UIProviderProps> = ({ children }) => {
    const [appState, setAppState] = useState<AppState>('idle');
    const [mode, setMode] = useState('auto');
    const [selectorTitle, setSelectorTitle] = useState('');
    const [selectorItems, setSelectorItems] = useState<SelectorItem[]>([]);
    const [selectorIndex, setSelectorIndex] = useState(0);
    const [selectorCmd, setSelectorCmd] = useState('');
    const [diffs, setDiffs] = useState<FileDiff[]>([]);

    const openSelector = useCallback((title: string, items: SelectorItem[], cmd: string) => {
        setSelectorTitle(title);
        setSelectorItems(items);
        setSelectorIndex(0);
        setSelectorCmd(cmd);
        setAppState('selecting');
    }, []);

    const closeSelector = useCallback(() => {
        setAppState('idle');
    }, []);

    const openDiffs = useCallback((newDiffs: FileDiff[]) => {
        setDiffs(newDiffs);
        setAppState('diffing');
    }, []);

    const value: UIContextValue = {
        appState, selectorTitle, selectorItems, selectorIndex, selectorCmd, diffs, mode,
        setAppState, setMode, openSelector, closeSelector, setSelectorIndex, openDiffs,
    };

    return <UIContext.Provider value={value}>{children}</UIContext.Provider>;
};
