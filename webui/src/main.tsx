import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { AppErrorBoundary } from './components/ErrorBoundary'
// Provider hierarchy (outer → inner = foundational → application)
import { ConfigProvider } from './providers/ConfigProvider'
import { SessionProvider } from './providers/SessionProvider'
import { HubProvider } from './providers/HubProvider'
import { ScrollProvider } from './providers/ScrollProvider'
import { StreamProvider } from './providers/StreamProvider'
import { LightboxProvider } from './providers/LightboxProvider'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppErrorBoundary>
      <ConfigProvider>
        <SessionProvider>
          <HubProvider>
            <ScrollProvider>
              <StreamProvider>
                <LightboxProvider>
                  <App />
                </LightboxProvider>
              </StreamProvider>
            </ScrollProvider>
          </HubProvider>
        </SessionProvider>
      </ConfigProvider>
    </AppErrorBoundary>
  </StrictMode>,
)
