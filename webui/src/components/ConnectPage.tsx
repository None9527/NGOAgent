import React, { useState, useEffect } from 'react'

interface ConnectPageProps {
  onConnected: () => void
}

/**
 * ConnectPage — gating screen shown before the main app.
 * Supports two modes:
 * 1. Direct: user enters full URL (e.g. http://192.168.1.x:19997)
 * 2. Proxy: URL left empty, uses Vite dev proxy (same origin)
 *
 * Auto-connects if token is already saved in localStorage.
 */
export const ConnectPage: React.FC<ConnectPageProps> = ({ onConnected }) => {
  const [url, setUrl] = useState(localStorage.getItem('SERVER_URL') || '')
  const [token, setToken] = useState(localStorage.getItem('AUTH_TOKEN') || '')
  const [error, setError] = useState('')
  const [connecting, setConnecting] = useState(false)
  const [autoTried, setAutoTried] = useState(false)

  // Auto-connect: try saved config, also try proxy mode (empty URL)
  useEffect(() => {
    ;(async () => {
      const savedToken = localStorage.getItem('AUTH_TOKEN')
      if (!savedToken) { setAutoTried(true); return }

      const savedUrl = localStorage.getItem('SERVER_URL') || ''
      // Try saved URL first (could be empty = proxy mode)
      const ok = await tryConnect(savedUrl, savedToken, true)
      if (ok) return

      // If saved URL was non-empty, also try proxy mode as fallback
      if (savedUrl) {
        const ok2 = await tryConnect('', savedToken, true)
        if (ok2) return
      }
      setAutoTried(true)
    })()
  }, [])

  async function tryConnect(serverUrl: string, authToken: string, silent = false): Promise<boolean> {
    if (!silent) setConnecting(true)
    setError('')

    try {
      const base = serverUrl.trim().replace(/\/$/, '')
      const res = await fetch(`${base}/v1/health`, { signal: AbortSignal.timeout(5000) })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)

      // Health passed — verify token
      const authRes = await fetch(`${base}/v1/config`, {
        headers: { 'Authorization': `Bearer ${authToken}` },
        signal: AbortSignal.timeout(5000),
      })
      if (authRes.status === 401) {
        if (!silent) {
          setError('Token 验证失败 — 请检查 auth_token 是否正确')
          setConnecting(false)
        }
        return false
      }
      if (!authRes.ok) throw new Error(`HTTP ${authRes.status}`)

      // Save and proceed
      localStorage.setItem('SERVER_URL', base)
      localStorage.setItem('AUTH_TOKEN', authToken.trim())
      onConnected()
      return true
    } catch (err) {
      if (!silent) {
        const msg = err instanceof Error ? err.message : String(err)
        setError(`连接失败: ${msg}`)
        setConnecting(false)
      }
      return false
    }
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!token.trim()) {
      setError('Token 为必填项')
      return
    }
    // URL can be empty (proxy mode)
    tryConnect(url.trim(), token.trim())
  }

  // While auto-connecting, show nothing (avoid flash)
  if (!autoTried && localStorage.getItem('AUTH_TOKEN')) {
    return (
      <div className="h-[100dvh] w-screen flex items-center justify-center bg-[#0a0a0a]">
        <span className="w-6 h-6 border-2 border-white/20 border-t-white rounded-full animate-spin" />
      </div>
    )
  }

  return (
    <div className="h-[100dvh] w-screen flex items-center justify-center bg-[#0a0a0a] text-gray-200">
      {/* Background ambient */}
      <div className="absolute inset-0 overflow-hidden pointer-events-none">
        <div className="absolute top-1/3 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] bg-blue-500/[0.03] rounded-full blur-[120px]" />
        <div className="absolute bottom-1/4 right-1/4 w-[400px] h-[400px] bg-purple-500/[0.02] rounded-full blur-[100px]" />
      </div>

      <form onSubmit={handleSubmit} className="relative z-10 w-full max-w-md mx-4">
        <div className="bg-[#141414] rounded-3xl border border-white/[0.06] shadow-[0_32px_64px_-20px_rgba(0,0,0,0.8)] p-8 flex flex-col gap-6">
          {/* Brand Header */}
          <div className="text-center pt-2 pb-2">
            <h1 className="text-5xl font-black tracking-[-0.04em] text-white mb-2 select-none">
              NGOAgent
            </h1>
            <p className="text-sm text-gray-500 tracking-wide">Connect to your agent instance</p>
          </div>

          {/* Server URL */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-gray-400 tracking-wide">服务器地址</label>
            <input
              type="text"
              value={url}
              onChange={e => setUrl(e.target.value)}
              placeholder="留空使用代理模式 / http://IP:19997"
              className="bg-white/[0.03] border border-white/[0.08] rounded-xl px-4 py-3 text-sm text-gray-200 font-mono outline-none focus:border-blue-500/40 focus:ring-1 focus:ring-blue-500/20 transition-all placeholder:text-gray-600"
            />
            <p className="text-[11px] text-gray-600">
              同机部署留空即可，远程部署填写服务器 IP
            </p>
          </div>

          {/* Auth Token */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-gray-400 tracking-wide">Auth Token</label>
            <input
              type="password"
              value={token}
              onChange={e => setToken(e.target.value)}
              placeholder="粘贴控制台输出的 64 位 token"
              className="bg-white/[0.03] border border-white/[0.08] rounded-xl px-4 py-3 text-sm text-gray-200 font-mono outline-none focus:border-blue-500/40 focus:ring-1 focus:ring-blue-500/20 transition-all placeholder:text-gray-600"
              autoFocus
            />
            <p className="text-[11px] text-gray-600 leading-relaxed">
              首次启动 NGOAgent 时自动生成并打印在控制台
            </p>
          </div>

          {/* Error */}
          {error && (
            <div className="text-sm text-red-400 bg-red-500/10 border border-red-500/20 rounded-xl px-4 py-3">
              {error}
            </div>
          )}

          {/* Submit */}
          <button
            type="submit"
            disabled={connecting}
            className="w-full bg-blue-600 hover:bg-blue-500 disabled:bg-blue-600/50 disabled:cursor-wait text-white rounded-xl py-3 px-4 font-medium transition-all text-sm tracking-wide hover:shadow-[0_8px_24px_-6px_rgba(59,130,246,0.4)]"
          >
            {connecting ? (
              <span className="flex items-center justify-center gap-2">
                <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                连接中...
              </span>
            ) : '连接'}
          </button>

          <p className="text-[11px] text-gray-600 text-center leading-relaxed">
            确保 NGOAgent 服务正在运行且该地址可从当前网络访问
          </p>
        </div>
      </form>
    </div>
  )
}
