/**
 * REST API client for NGOAgent backend.
 * Extracted from adapter.ts — pure HTTP calls, no rendering logic.
 */

import type { HealthInfo, SessionInfo, SessionListResponse, HistoryMessage } from './types'

const API_BASE = '' // Vite proxy handles /v1, /api → localhost:19997

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

async function post<T>(path: string, body: unknown = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
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
  stop: () => post('/v1/stop'),
  approve: (approvalId: string, approved: boolean) =>
    post('/v1/approve', { approval_id: approvalId, approved }),
  listSessions: () => get<SessionListResponse>('/api/v1/session/list'),
  deleteSession: (id: string) => post<{ status: string }>('/api/v1/session/delete', { id }),
  setSessionTitle: (id: string, title: string) =>
    post<{ status: string }>('/api/v1/session/title', { id, title }),
  getHistory: (sessionId: string) =>
    get<{ messages: HistoryMessage[] }>(`/api/v1/history?session_id=${encodeURIComponent(sessionId)}`),
  clearHistory: () => post<{ status: string }>('/api/v1/history/clear'),
}

export { API_BASE }
