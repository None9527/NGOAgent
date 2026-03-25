import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { AppErrorBoundary } from './components/ErrorBoundary'
// Provider hierarchy (outer → inner = foundational → application)
// Layer 1: Pure config — no side effects
import { ConfigProvider } from './providers/ConfigProvider'
// Layer 2: Session management — depends on config
import { SessionProvider } from './providers/SessionProvider'
// Layer 3: Intelligence Hub — depends on session
import { HubProvider } from './providers/HubProvider'
// Layer 4: Stream transport — depends on session (legacy, migrating to ConnectionProvider in Phase 7)
import { StreamProvider } from './providers/StreamProvider'
// Layer 5: Connection observation — wraps WS state
import { ConnectionProvider } from './providers/ConnectionProvider'
// Layer 6: Unified chat context — aggregates all services for renderers
import { ChatContextProvider } from './providers/ChatContextProvider'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppErrorBoundary>
      <ConfigProvider>
        <SessionProvider>
          <HubProvider>
            <StreamProvider>
              <ConnectionProvider>
                <ChatContextProvider>
                  <App />
                </ChatContextProvider>
              </ConnectionProvider>
            </StreamProvider>
          </HubProvider>
        </SessionProvider>
      </ConfigProvider>
    </AppErrorBoundary>
  </StrictMode>,
)
