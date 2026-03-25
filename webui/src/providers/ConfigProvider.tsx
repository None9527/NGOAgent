/**
 * ConfigProvider — manages planMode, securityMode, availableModels, health.
 * Eliminates ~6 useState + 2 handlers from App.tsx.
 */

import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'
import { api, getApiBase, authFetch } from '../chat/api'
import type { HealthInfo } from '../chat/types'

interface ConfigState {
  planMode: 'auto' | 'plan' | 'agentic'
  securityMode: string
  availableModels: string[]
  health: HealthInfo | null
}

interface ConfigActions {
  togglePlanMode: () => void
  toggleSecurityMode: () => Promise<void>
  switchModel: (modelName: string) => Promise<void>
  /** Initialize from backend — called once after auth */
  initialize: () => Promise<{ planMode: 'auto' | 'plan' | 'agentic'; securityMode: string }>
}

type ConfigContextValue = ConfigState & ConfigActions

const ConfigContext = createContext<ConfigContextValue | null>(null)

export function useConfig(): ConfigContextValue {
  const ctx = useContext(ConfigContext)
  if (!ctx) throw new Error('useConfig must be used within ConfigProvider')
  return ctx
}

export function ConfigProvider({ children }: { children: ReactNode }) {
  const [planMode, setPlanMode] = useState<'auto' | 'plan' | 'agentic'>('auto')
  const [securityMode, setSecurityMode] = useState('allow')
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [health, setHealth] = useState<HealthInfo | null>(null)

  const togglePlanMode = useCallback(() => {
    setPlanMode(prev => {
      if (prev === 'auto') return 'plan'
      if (prev === 'plan') return 'agentic'
      return 'auto'
    })
  }, [])

  const toggleSecurityMode = useCallback(async () => {
    const next = securityMode === 'allow' ? 'ask' : 'allow'
    await authFetch('/api/v1/config', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: 'security.mode', value: next })
    })
    setSecurityMode(next)
  }, [securityMode])

  const switchModel = useCallback(async (modelName: string) => {
    try {
      await api.switchModel(modelName)
      const h = await api.health()
      setHealth(h)
    } catch (err) {
      console.error('Failed to switch model', err)
    }
  }, [])

  const initialize = useCallback(async () => {
    const h = await api.health()
    setHealth(h)

    const modelsData = await api.listModels()
    setAvailableModels(modelsData.models || [])

    // Load config for mode sync
    const configRes = await fetch(`${getApiBase()}/v1/config`, {
      headers: { 'Authorization': `Bearer ${localStorage.getItem('AUTH_TOKEN') || ''}` },
      signal: AbortSignal.timeout(5000),
    })
    if (!configRes.ok) return { planMode: 'auto' as const, securityMode: 'allow' }

    const cfg = await configRes.json()
    const planModeVal = cfg?.agent?.planning_mode
    let resolvedPlan: 'auto' | 'plan' | 'agentic' = 'auto'
    if (planModeVal === 'plan' || planModeVal === true) resolvedPlan = 'plan'
    else if (planModeVal === 'agentic') resolvedPlan = 'agentic'

    const resolvedSecurity = cfg?.security?.mode || 'allow'

    setPlanMode(resolvedPlan)
    setSecurityMode(resolvedSecurity)

    return { planMode: resolvedPlan, securityMode: resolvedSecurity }
  }, [])

  return (
    <ConfigContext.Provider value={{
      planMode, securityMode, availableModels, health,
      togglePlanMode, toggleSecurityMode, switchModel, initialize,
    }}>
      {children}
    </ConfigContext.Provider>
  )
}
