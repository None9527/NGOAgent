/**
 * ErrorBoundary — React error boundaries for isolated failure handling.
 *
 * AppErrorBoundary:     Root-level fallback (prevents full white screen).
 * MessageErrorBoundary: Per-message isolation (one message crash ≠ full chat crash).
 */

import { Component, type ReactNode, type ErrorInfo } from 'react'
import { ErrorBoundary, type FallbackProps } from 'react-error-boundary'

// ─── App-level Error Boundary ──────────────────────────────

function AppFallback({ error, resetErrorBoundary }: FallbackProps) {
  const err = error instanceof Error ? error : new Error(String(error))
  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      height: '100vh',
      gap: '16px',
      fontFamily: 'system-ui, sans-serif',
      background: 'var(--color-bg-primary, #0f1117)',
      color: 'var(--color-text-primary, #e2e8f0)',
    }}>
      <div style={{ fontSize: '2rem' }}>⚠️</div>
      <h2 style={{ margin: 0, fontSize: '1.25rem' }}>Application Error</h2>
      <pre style={{
        maxWidth: '600px',
        padding: '12px',
        background: 'rgba(255,0,0,0.1)',
        borderRadius: '6px',
        fontSize: '0.8rem',
        overflow: 'auto',
        color: '#fc8181',
      }}>
        {err.message}
      </pre>
      <button
        onClick={resetErrorBoundary}
        style={{
          padding: '8px 20px',
          background: 'var(--color-accent, #6366f1)',
          color: '#fff',
          border: 'none',
          borderRadius: '6px',
          cursor: 'pointer',
          fontSize: '0.9rem',
        }}
      >
        Reload
      </button>
    </div>
  )
}

export function AppErrorBoundary({ children }: { children: ReactNode }) {
  return (
    <ErrorBoundary
      FallbackComponent={AppFallback}
      onError={(error: unknown, info: ErrorInfo) =>
        console.error('[AppErrorBoundary]', error, info)
      }
    >
      {children}
    </ErrorBoundary>
  )
}

// ─── Message-level Error Boundary ──────────────────────────

function MessageFallback({ error }: FallbackProps) {
  const msg = error instanceof Error ? error.message : String(error)
  return (
    <div style={{
      padding: '8px 12px',
      margin: '4px 0',
      border: '1px solid rgba(252, 129, 129, 0.3)',
      borderRadius: '6px',
      background: 'rgba(252, 129, 129, 0.05)',
      color: '#fc8181',
      fontSize: '0.8rem',
      fontFamily: 'monospace',
    }}>
      ⚠️ Message render error: {msg}
    </div>
  )
}

export function MessageErrorBoundary({ children }: { children: ReactNode }) {
  return (
    <ErrorBoundary
      FallbackComponent={MessageFallback}
      onError={(error: unknown) =>
        console.warn('[MessageErrorBoundary]', error)
      }
    >
      {children}
    </ErrorBoundary>
  )
}

// ─── Legacy class-based fallback (for non-hook contexts) ──

interface ClassBoundaryState { hasError: boolean; error: Error | null }

export class ClassErrorBoundary extends Component<
  { children: ReactNode; fallback?: ReactNode },
  ClassBoundaryState
> {
  state: ClassBoundaryState = { hasError: false, error: null }

  static getDerivedStateFromError(error: Error): ClassBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ClassErrorBoundary]', error, info)
  }

  render() {
    if (this.state.hasError) {
      return this.props.fallback ?? (
        <div style={{ color: '#fc8181', padding: '8px', fontSize: '0.8rem' }}>
          ⚠️ {this.state.error?.message}
        </div>
      )
    }
    return this.props.children
  }
}
