import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { ConfigProvider } from './providers/ConfigProvider'
import { SessionProvider } from './providers/SessionProvider'
import { StreamProvider } from './providers/StreamProvider'
import { HubProvider } from './providers/HubProvider'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider>
      <SessionProvider>
        <HubProvider>
          <StreamProvider>
            <App />
          </StreamProvider>
        </HubProvider>
      </SessionProvider>
    </ConfigProvider>
  </StrictMode>,
)

