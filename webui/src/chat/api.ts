/**
 * REST API client for NGOAgent backend.
 * Extracted from adapter.ts — pure HTTP calls, no rendering logic.
 */

import type { HealthInfo, SessionInfo, SessionListResponse, HistoryMessage } from './types'

/**
 * Returns the stored auth token from localStorage.
 * Empty string means no authentication.
 */
export function getAuthToken(): string {
  try {
    const token = localStorage.getItem('AUTH_TOKEN')
    if (token && token.trim()) return token.trim()
  } catch { /* ignore */ }
  return ''
}

/** Build auth headers: merges Authorization if token exists */
function authHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...extra }
  const token = getAuthToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  return headers
}

/**
 * Returns the current API base URL.
 * In browser/PWA/APK mode: reads `SERVER_URL` from localStorage.
 * Falls back to '' (relative URL, works with Vite dev proxy).
 */
export function getApiBase(): string {
  try {
    const stored = localStorage.getItem('SERVER_URL')
    if (stored && stored.trim()) return stored.trim().replace(/\/$/, '')
  } catch { /* ignore SSR or private mode */ }
  return '' // relative — Vite proxy handles /v1, /api → localhost:19997
}

/**
 * Drop-in replacement for fetch() that auto-injects Authorization header.
 * Use this instead of raw fetch() in all components.
 */
export function authFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers)
  const token = getAuthToken()
  if (token && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  return fetch(input, { ...init, headers })
}



async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${getApiBase()}${path}`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

async function post<T>(path: string, body: unknown = {}): Promise<T> {
  const res = await fetch(`${getApiBase()}${path}`, {
    method: 'POST',
    headers: authHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

export const api = {
  health: () => get<HealthInfo>('/v1/health'),
  listModels: () => get<{ models: string[]; current: string }>('/v1/models'),
  switchModel: (modelName: string) => post<{ status: string }>('/v1/model/switch', { model: modelName }),
  newSession: (title = '') => post<SessionInfo>('/api/v1/session/new', { title }),
  stop: (sessionId?: string) => post('/v1/stop', { session_id: sessionId || '' }),
  approve: (approvalId: string, approved: boolean) =>
    post('/v1/approve', { approval_id: approvalId, approved }),
  listSessions: () => get<SessionListResponse>('/api/v1/session/list'),
  deleteSession: (id: string) => post<{ status: string }>('/api/v1/session/delete', { id }),
  setSessionTitle: (id: string, title: string) =>
    post<{ status: string }>('/api/v1/session/title', { id, title }),
  getHistory: (sessionId: string) =>
    get<{ messages: HistoryMessage[] }>(`/api/v1/history?session_id=${encodeURIComponent(sessionId)}`),
  clearHistory: () => post<{ status: string }>('/api/v1/history/clear'),
  retry: (sessionId: string) => post<{ status: string; last_message: string }>('/v1/retry', { session_id: sessionId }),
}



/**
 * Build a proxied file URL with token query param.
 * Needed because <img>/<video> tags can't send Authorization headers.
 */
export function fileUrl(path: string): string {
  const cleaned = path.replace(/^file:\/\//, '')
  const token = getAuthToken()
  const base = getApiBase()
  return `${base}/v1/file?path=${encodeURIComponent(cleaned)}&token=${encodeURIComponent(token)}`
}
