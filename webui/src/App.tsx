import { useState, useEffect, useRef, useCallback } from 'react'
import { ChatViewer } from './renderers/ChatViewer'
import { InputForm } from './renderers/InputForm'
import type { FileItem } from './renderers/InputForm'
import { api, getApiBase, authFetch } from './chat/api'
import { chatStream, checkActiveRun, reconnectStream } from './chat/streamHandler'
import { historyToMessages } from './chat/messageMapper'
import type { ChatMessageData, HealthInfo, SessionListItem, ApprovalRequest } from './chat/types'
import { useChatScroll } from './hooks/useChatScroll'

// Import new structural components
import { Sidebar } from './components/Sidebar'
import { TopNavbar } from './components/TopNavbar'
import { WelcomeScreen } from './components/WelcomeScreen'
import { IntelligenceHub } from './components/IntelligenceHub/index'
import { SettingsPage } from './components/SettingsPage'
import { ConnectPage } from './components/ConnectPage'

// Import styles
import './renderers/styles/variables.css'
import './renderers/styles/timeline.css'
import './renderers/styles/components.css'

export default function App() {
  const [inputText, setInputText] = useState('')
  const [messages, setMessages] = useState<ChatMessageData[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const [health, setHealth] = useState<HealthInfo | null>(null)
  
  // Session management
  const [sessionId, setSessionId] = useState('')
  const [sessions, setSessions] = useState<SessionListItem[]>([])
  
  // Model management
  const [availableModels, setAvailableModels] = useState<string[]>([])
  
  // Layout state
  const [isSidebarOpen, setIsSidebarOpen] = useState(true) // Open by default on desktop
  const [isSettingsOpen, setIsSettingsOpen] = useState(false)

  // Spatial Right Hub State
  const [isHubOpen, setIsHubOpen] = useState(false)
  const [hubTab, setHubTab] = useState<'brain' | 'knowledge' | 'cron' | 'skills'>('brain')
  const [brainRefreshTrigger, setBrainRefreshTrigger] = useState(0)
  const [brainFocusTrigger, setBrainFocusTrigger] = useState<{ file: string; ts: number } | null>(null)

  // Mode toggles (synced with config)
  const [planMode, setPlanMode] = useState(false)   // false=Auto, true=Plan
  const [securityMode, setSecurityMode] = useState('allow') // 'allow' | 'ask'

  // File attachments
  const [attachedFiles, setAttachedFiles] = useState<FileItem[]>([])

  // Plan review state
  const [planReview, setPlanReview] = useState<{ message: string; paths: string[] } | null>(null)
  const [planFeedbackInput, setPlanFeedbackInput] = useState('')
  const [showFeedbackInput, setShowFeedbackInput] = useState(false)

  // Task progress state (from progress SSE events)
  const [taskProgress, setTaskProgress] = useState<{
    taskName: string; status: string; summary: string; mode: string
  } | null>(null)
  
  
  const [connected, setConnected] = useState(() => {
    try {
      const t = localStorage.getItem('AUTH_TOKEN')
      return !!(t && t.trim())
    } catch { return false }
  })
  const [pendingApprovals, setPendingApprovals] = useState<ApprovalRequest[]>([])
  const cancelRef = useRef<(() => void) | null>(null)
  const inputRef = useRef<HTMLDivElement>(null)
  // P0 perf: Map<uuid, index> for O(1) message lookups during streaming
  const msgIndexRef = useRef<Map<string, number>>(new Map())
  // Flag: set to true when loading history so the next messages commit triggers resetToBottom
  const pendingScrollToEnd = useRef(false)
  
  // ── Sticky auto-scroll: all logic lives in the hook ──
  const { scrollContainerRef, messagesEndRef, handleScroll, scrollToBottom, resetToBottom } = useChatScroll()

  // Reactive scroll-to-end: fires after React commits state from loadHistory
  useEffect(() => {
    if (pendingScrollToEnd.current && messages.length > 0) {
      pendingScrollToEnd.current = false
      resetToBottom()
    }
  }, [messages, resetToBottom])

  // Helper: refresh session list from backend (throttled)
  const lastRefreshRef = useRef(0)
  const refreshSessions = useCallback(async () => {
    // Throttle: at most once per 3s
    const now = Date.now()
    if (now - lastRefreshRef.current < 3000) return
    lastRefreshRef.current = now
    try {
      const data = await api.listSessions()
      setSessions(data.sessions)
    } catch (err) {
      console.error('Failed to fetch sessions', err)
    }
  }, [])

  // Helper: load history for a single session (uses shared messageMapper)
  const loadHistory = useCallback(async (sid: string) => {
    try {
      const data = await api.getHistory(sid)
      setMessages(() => {
        const msgs = historyToMessages(data.messages, sid)
        // Rebuild index
        const idx = new Map<string, number>()
        msgs.forEach((m, i) => idx.set(m.uuid, i))
        msgIndexRef.current = idx
        return msgs
      })
      // Signal: next messages useEffect will snap to bottom after React commits
      pendingScrollToEnd.current = true
    } catch (err) {
      console.error('Failed to load history', err)
      setMessages([])
    }
  }, [resetToBottom])

  // Initialize: load existing sessions + health after connection is established
  useEffect(() => {
    if (!connected) return
    document.documentElement.classList.add('dark')
    ;(async () => {
      try {
        // Lazy token validation: verify saved token is still valid
        const configRes = await fetch(`${getApiBase()}/v1/config`, {
          headers: { 'Authorization': `Bearer ${localStorage.getItem('AUTH_TOKEN') || ''}` },
          signal: AbortSignal.timeout(5000),
        })
        if (configRes.status === 401) {
          // Token invalid — kick back to ConnectPage
          localStorage.removeItem('AUTH_TOKEN')
          setConnected(false)
          return
        }

        const h = await api.health()
        setHealth(h)
        const data = await api.listSessions()
        setSessions(data.sessions)
        if (data.active) {
          setSessionId(data.active)
        }
        // Load available models
        const modelsData = await api.listModels()
        setAvailableModels(modelsData.models || [])
        // Sync mode toggles from config
        const cfg = await configRes.json()
        setPlanMode(!!cfg?.agent?.planning_mode)
        setSecurityMode(cfg?.security?.mode || 'allow')

        // Auto-reconnect: check if there's an active run for the current session
        const activeSessionId = data.active
        if (activeSessionId) {
          const runStatus = await checkActiveRun(activeSessionId)
          if (runStatus.active && !runStatus.done) {
            console.log('[App] Active run detected, reconnecting SSE stream...')
            setIsStreaming(true)
            const handle = reconnectStream(activeSessionId, 0, {
              onMessage: (msg) => setMessages(prev => [...prev, msg]),
              onUpdate: (uuid, patch) => {
                setMessages(prev => prev.map(m => {
                  if (m.uuid !== uuid) return m
                  if (patch.toolCall && m.toolCall) {
                    return {
                      ...m, ...patch,
                      toolCall: {
                        ...m.toolCall, ...patch.toolCall,
                        title: patch.toolCall.title || m.toolCall.title,
                        rawInput: patch.toolCall.rawInput || m.toolCall.rawInput,
                        content: patch.toolCall.content && patch.toolCall.content.length > 0
                          ? patch.toolCall.content : m.toolCall.content,
                      },
                    }
                  }
                  return { ...m, ...patch }
                }))
                if (patch.toolCall?.status === 'completed' && patch.toolCall?.kind) {
                  const kind = patch.toolCall.kind as string
                  if (['write', 'edit', 'updated_plan'].includes(kind)) {
                    setBrainRefreshTrigger(prev => prev + 1)
                  }
                }
              },
              onToolCall: (msg) => setMessages(prev => [...prev, msg]),
              onApproval: (req) => setPendingApprovals(prev => [...prev, req]),
              onPlanReview: (message, paths) => setPlanReview({ message, paths }),
              onStepDone: () => refreshSessions(),
              onProgress: (taskName, status, summary, mode) => setTaskProgress({ taskName, status, summary, mode }),
              onEnd: () => {
                setIsStreaming(false)
                setTaskProgress(null)
                cancelRef.current = null
                refreshSessions()
              },
              onError: (err) => { setIsStreaming(false); cancelRef.current = null; console.error('Reconnect error:', err) },
            })
            cancelRef.current = handle.cancel
          }
        }
      } catch (err: unknown) {
        // Network error during init — don't kick to ConnectPage, just log
        // (user might be offline temporarily, token is still saved)
        console.error('Init failed after connect:', err)
      }
    })()
  }, [connected])

  const handleNewSession = async () => {
    try {
      const sess = await api.newSession()
      setSessionId(sess.session_id)
      setMessages([])
      await refreshSessions()
      // Auto-hide sidebar on mobile if needed
      if (window.innerWidth < 768) setIsSidebarOpen(false) 
    } catch (err) {
      console.error('Failed to create new session', err)
    }
  }

  const handleSelectSession = async (id: string) => {
    setSessionId(id)
    await loadHistory(id)
    if (window.innerWidth < 768) setIsSidebarOpen(false)
  }

  const handleDeleteSession = async (id: string) => {
    try {
      await api.deleteSession(id)
      await refreshSessions()
      // If deleted the current session, create a new one
      if (id === sessionId) {
        const sess = await api.newSession()
        setSessionId(sess.session_id)
        setMessages([])
      }
    } catch (err) {
      console.error('Failed to delete session', err)
    }
  }

  const handleRenameSession = async (id: string, newTitle: string) => {
    try {
      await api.setSessionTitle(id, newTitle)
      await refreshSessions()
    } catch (err) {
      console.error('Failed to rename session', err)
    }
  }

  const handleModelSwitch = async (modelName: string) => {
    try {
      await api.switchModel(modelName)
      // Refresh health to get updated model
      const h = await api.health()
      setHealth(h)
    } catch (err) {
      console.error('Failed to switch model', err)
      console.error('Failed to switch model:', err)
    }
  }

  const handleSuggestionClick = (suggestionText: string) => {
    setInputText(suggestionText)
    if (inputRef.current) {
      inputRef.current.focus()
    }
  }

  // Send message — overrideText allows direct send from banner buttons
  const handleSend = useCallback(async (e?: React.FormEvent, overrideText?: string) => {
    if (e) e.preventDefault()
    const textToSend = (overrideText || inputText).trim()
    if (!textToSend || isStreaming) return

    setInputText('')

    // Lazy session creation: create on first message
    let sid = sessionId
    if (!sid) {
      try {
        const sess = await api.newSession()
        sid = sess.session_id
        setSessionId(sid)
      } catch (err) {
        console.error('Failed to create session', err)
        return
      }
    }

    // Refresh session list in background
    refreshSessions()

    // Build message text with file attachments if any
    const uploadedFiles = attachedFiles.filter(f => f.status === 'uploaded' && f.path)
    let finalText = textToSend
    if (uploadedFiles.length > 0) {
      const isImage = (t: string) => t.startsWith('image/')
      const fileEntries = uploadedFiles
        .map(f => `  <file name="${f.name}" path="${f.path}" type="${f.type}" role="${isImage(f.type) ? 'reference_image' : 'reference_file'}" />`)
        .join('\n')
      finalText = `<user_attachments>\n${fileEntries}\n</user_attachments>\n\n${textToSend}`
      setAttachedFiles([])
    }

    // Add user message
    const userMsg: ChatMessageData = {
      uuid: `user-${Date.now()}`,
      timestamp: new Date().toISOString(),
      type: 'user',
      message: { role: 'user', parts: [{ text: finalText }] },
    }
    setMessages(prev => [...prev, userMsg])
    setIsStreaming(true)
    scrollToBottom('instant') // User action: snap immediately, no animation

    const handle = chatStream(finalText, sid, {
      onMessage: (msg) => {
        setMessages(prev => {
          msgIndexRef.current.set(msg.uuid, prev.length)
          return [...prev, msg]
        })
      },
      onUpdate: (uuid, patch) => {
        setMessages(prev => {
          const idx = msgIndexRef.current.get(uuid)
          if (idx === undefined) return prev
          const m = prev[idx]
          if (!m) return prev
          const next = [...prev]
          // Deep merge toolCall
          if (patch.toolCall && m.toolCall) {
            next[idx] = {
              ...m, ...patch,
              toolCall: {
                ...m.toolCall,
                ...patch.toolCall,
                title: patch.toolCall.title || m.toolCall.title,
                rawInput: patch.toolCall.rawInput || m.toolCall.rawInput,
                content: patch.toolCall.content && patch.toolCall.content.length > 0
                  ? patch.toolCall.content
                  : m.toolCall.content,
              },
            }
          } else {
            next[idx] = { ...m, ...patch }
          }
          return next
        })
        
        // Reactive Intelligence Hub: Refresh Brain file list when a file/plan operation completes
        if (patch.toolCall?.status === 'completed' && patch.toolCall?.kind) {
          const kind = patch.toolCall.kind as string
          if (['write', 'edit', 'updated_plan'].includes(kind)) {
            setBrainRefreshTrigger(prev => prev + 1)
          }
        }
      },
      onToolCall: (msg) => {
        setMessages(prev => {
          msgIndexRef.current.set(msg.uuid, prev.length)
          return [...prev, msg]
        })
        // Reactive Intelligence Hub: Auto-open Brain tab for file/plan operations
        if (msg.toolCall && ['write', 'edit', 'updated_plan'].includes(msg.toolCall.kind)) {
          setHubTab('brain')
          setIsHubOpen(true)
          // Auto-focus specific artifact when task_plan starts (rawInput available at tool_start)
          if (msg.toolCall.kind === 'updated_plan' && msg.toolCall.rawInput) {
            const planType = (msg.toolCall.rawInput as Record<string, unknown>).type as string
            const fileMap: Record<string, string> = { plan: 'plan.md', task: 'task.md', walkthrough: 'walkthrough.md' }
            const targetFile = fileMap[planType]
            if (targetFile) {
              // Delay to allow tool execution + refresh to populate the file
              setTimeout(() => setBrainFocusTrigger({ file: targetFile, ts: Date.now() }), 800)
            }
          }
        }
      },
      onApproval: (req) => {
          setPendingApprovals(prev => [...prev, req])
        },
      onPlanReview: (message, paths) => {
          setPlanReview({ message, paths })
        },
      onStepDone: () => {
          refreshSessions() // Also refresh sidebar titles on each step
        },
      onProgress: (taskName, status, summary, mode) => {
          setTaskProgress({ taskName, status, summary, mode })
        },
      onEnd: () => {
          setIsStreaming(false)
          setTaskProgress(null)
          cancelRef.current = null
          refreshSessions()
          setTimeout(() => refreshSessions(), 3500)
        },
      onError: (err) => { setIsStreaming(false); cancelRef.current = null; console.error('Stream error:', err) },
    })
    cancelRef.current = handle.cancel
  }, [inputText, isStreaming, sessionId, attachedFiles])

  // Stop — signal backend first, then abort SSE stream
  const handleStop = useCallback(async () => {
    if (!isStreaming) return
    // Signal backend to abort agent loop first
    try { await api.stop() } catch { /* ignore */ }
    // Abort the SSE connection (triggers onError which also sets isStreaming=false)
    cancelRef.current?.()
    cancelRef.current = null
    setIsStreaming(false)
    setTaskProgress(null)
  }, [isStreaming])

  // Gate: show ConnectPage until authenticated
  if (!connected) {
    return <ConnectPage onConnected={() => setConnected(true)} />
  }

  const handleTogglePlanMode = async () => {
    const next = !planMode
    await authFetch('/api/v1/config', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: 'agent.planning_mode', value: next })
    })
    setPlanMode(next)
  }

  const handleToggleSecurityMode = async () => {
    const next = securityMode === 'allow' ? 'ask' : 'allow'
    await authFetch('/api/v1/config', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: 'security.mode', value: next })
    })
    setSecurityMode(next)
  }

  return (
    <div className="flex h-[100dvh] w-screen overflow-hidden bg-transparent text-gray-200 font-sans selection:bg-blue-500/30">
      
      <Sidebar 
        isOpen={isSidebarOpen} 
        onToggle={() => setIsSidebarOpen(!isSidebarOpen)}
        sessions={sessions}
        currentSessionId={sessionId}
        onSelectSession={handleSelectSession}
        onNewSession={handleNewSession}
        onDeleteSession={handleDeleteSession}
        onRenameSession={handleRenameSession}
        onOpenHubTab={(tab: 'brain' | 'knowledge' | 'cron' | 'skills') => {
          setHubTab(tab)
          setIsHubOpen(true)
        }}
        onOpenSettings={() => setIsSettingsOpen(true)}
      />

      <main className="flex-1 flex flex-col relative w-full h-full min-w-0 bg-transparent">
        
        <TopNavbar 
          onToggleSidebar={() => setIsSidebarOpen(!isSidebarOpen)} 
          modelName={health?.model || ''}
          onToggleHub={() => setIsHubOpen(!isHubOpen)}
          isHubOpen={isHubOpen}
          availableModels={availableModels}
          currentModel={health?.model || ''}
          onModelSelect={handleModelSwitch}
          onOpenSettings={() => setIsSettingsOpen(true)}
        />

        {/* Banners absolutely centered over the read-column */}
        {pendingApprovals.length > 0 && (
          <div className="absolute top-14 sm:top-20 left-1/2 -translate-x-1/2 z-50 flex flex-col gap-2 w-full max-w-4xl px-2 sm:px-4 pointer-events-none">
            <div className="flex flex-col gap-2 w-full pointer-events-auto" style={{ animation: 'slideDown 0.3s cubic-bezier(0.4, 0, 0.2, 1)' }}>
              {pendingApprovals.map((req, idx) => (
                <div key={req.approvalId}
                  className={`w-full rounded-2xl border border-amber-500/30 bg-black/60 backdrop-blur-[40px] px-5 py-4 flex flex-col gap-3 shadow-[0_20px_40px_-10px_rgba(0,0,0,0.8)] ${pendingApprovals.length > 1 && idx > 0 ? 'mt-2' : ''}`}
                  style={{ animation: 'fadeInScale 0.25s cubic-bezier(0.4, 0, 0.2, 1)', animationDelay: `${idx * 0.1}s`, animationFillMode: 'backwards' }}>
                <div className="flex items-start gap-4">
                  <span className="text-amber-400 text-xl mt-0.5 opacity-90 leading-none">⚠️</span>
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-semibold tracking-wide text-amber-200">
                      待审批操作 <span className="text-gray-500 mx-2">|</span> <code className="font-mono text-[11px] bg-amber-500/10 text-amber-300/90 px-2 py-0.5 rounded-md border border-amber-500/20">{req.toolName}</code>
                    </div>
                    {req.reason && (
                      <div className="text-[13px] text-amber-100/60 mt-1.5 leading-relaxed">{req.reason}</div>
                    )}
                    {Object.keys(req.args).length > 0 && (
                      <pre className="text-[11px] mt-2 text-gray-400 bg-black/40 rounded-lg px-3 py-2.5 overflow-auto max-h-32 font-mono ring-1 ring-white/5">
                        {JSON.stringify(req.args, null, 2)}
                      </pre>
                    )}
                  </div>
                </div>
                <div className="flex gap-2 justify-end flex-wrap">
                  <button
                    onClick={async () => {
                      await api.approve(req.approvalId, false)
                      setPendingApprovals(prev => prev.filter(r => r.approvalId !== req.approvalId))
                    }}
                    className="px-4 py-1.5 rounded-lg text-sm font-medium bg-red-900/60 hover:bg-red-800/80 text-red-200 border border-red-700/40 transition-all hover:scale-105">
                    拒绝
                  </button>
                  <button
                    onClick={async () => {
                      await api.approve(req.approvalId, true)
                      setPendingApprovals(prev => prev.filter(r => r.approvalId !== req.approvalId))
                    }}
                    className="px-4 py-1.5 rounded-lg text-sm font-medium bg-emerald-900/60 hover:bg-emerald-800/80 text-emerald-200 border border-emerald-700/40 transition-all hover:scale-105">
                    允许执行
                  </button>
                </div>
              </div>
              ))}
              {pendingApprovals.length > 1 && (
                <div className="text-xs text-center text-amber-300/60 mt-1">
                  {pendingApprovals.length} 个待审批操作
                </div>
              )}
            </div>
          </div>
        )}

        {/* Plan Review Banner */}
        {planReview && (
          <div className="absolute left-1/2 -translate-x-1/2 z-40 flex flex-col gap-2 w-full max-w-4xl px-4 pointer-events-none"
            style={{ 
              top: pendingApprovals.length > 0 ? '12rem' : '5rem',
              animation: 'slideDown 0.3s cubic-bezier(0.4, 0, 0.2, 1)'
            }}>
            <div className="w-full rounded-2xl border border-blue-500/30 bg-black/60 backdrop-blur-[40px] px-5 py-4 flex flex-col gap-3 shadow-[0_20px_40px_-10px_rgba(0,0,0,0.8)] pointer-events-auto">
              <div className="flex items-start gap-4">
                <span className="text-blue-400 text-xl mt-0.5 opacity-90 leading-none">📋</span>
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-semibold tracking-wide text-blue-200">计划执行审批</div>
                  <div className="text-[13px] text-blue-100/60 mt-1.5 leading-relaxed">{planReview.message}</div>
                  {planReview.paths.length > 0 && (
                    <div className="text-[11px] text-gray-500 mt-2 font-mono flex flex-wrap gap-1">
                      {planReview.paths.map(p => (
                        <span key={p} className="bg-black/40 px-2 py-0.5 rounded-md border border-white/5">{p.split('/').pop()}</span>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              {showFeedbackInput && (
                <div className="flex gap-2 mt-1">
                  <input
                    type="text"
                    value={planFeedbackInput}
                    onChange={(e) => setPlanFeedbackInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && planFeedbackInput.trim()) {
                        const text = planFeedbackInput.trim()
                        setShowFeedbackInput(false)
                        setPlanReview(null)
                        setPlanFeedbackInput('')
                        handleSend(undefined, text)
                      }
                    }}
                    placeholder="输入修改意见..."
                    className="flex-1 bg-black/30 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-gray-200 placeholder:text-gray-600 focus:outline-none focus:border-blue-500/40"
                    autoFocus
                  />
                  <button
                    onClick={() => {
                      if (planFeedbackInput.trim()) {
                        const text = planFeedbackInput.trim()
                        setShowFeedbackInput(false)
                        setPlanReview(null)
                        setPlanFeedbackInput('')
                        handleSend(undefined, text)
                      }
                    }}
                    className="px-3 py-1.5 rounded-lg text-sm font-medium bg-blue-900/60 hover:bg-blue-800/80 text-blue-200 border border-blue-700/40 transition-colors"
                  >
                    发送
                  </button>
                </div>
              )}

              <div className="flex gap-2 justify-end flex-wrap mt-2">
                <button
                  onClick={() => {
                    setPlanReview(null)
                    setShowFeedbackInput(false)
                    handleSend(undefined, 'rejected')
                  }}
                  className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-red-500/10 hover:bg-red-500/20 text-red-400 border border-red-500/30 transition-all hover:scale-105"
                >
                  拒绝并关闭
                </button>
                <button
                  onClick={() => {
                    setIsHubOpen(true)
                    setHubTab('brain')
                    if (planReview.paths.length > 0) {
                      setBrainFocusTrigger({ file: planReview.paths[0].split('/').pop() || 'plan.md', ts: Date.now() })
                    }
                  }}
                  className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-white/5 hover:bg-white/10 text-gray-300 border border-white/10 transition-all hover:scale-105"
                >
                  检视大纲
                </button>
                <button
                  onClick={() => setShowFeedbackInput(true)}
                  className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-amber-500/10 hover:bg-amber-500/20 text-amber-300 border border-amber-500/30 transition-all hover:scale-105"
                >
                  修改计划
                </button>
                <button
                  onClick={() => {
                    setPlanReview(null)
                    setShowFeedbackInput(false)
                    handleSend(undefined, 'approved')
                  }}
                  className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 transition-all hover:scale-105"
                >
                  批准执行
                </button>
              </div>
            </div>
          </div>
        )}
        {/* ── Scrollable Chat Area ── */}
        {/* The hook's MutationObserver + ResizeObserver watches this container */}
        {/* NOTE: Do NOT add CSS scroll-smooth here — it overrides scrollTop assignments and causes */}
        {/* scroll lag during streaming. The hook uses scrollTo({behavior:'smooth'}) only for explicit user scrolls. */}
        <div ref={scrollContainerRef} onScroll={handleScroll} className="flex-1 w-full overflow-y-auto relative z-0">
          {/* Reading Column Container */}
          <div className="w-full max-w-4xl mx-auto flex flex-col min-h-full pt-10 md:pt-20 px-1 md:px-4 relative">
            {messages.length === 0 ? (
              <div className="flex-1 flex items-center justify-center">
                <WelcomeScreen onSuggestionClick={handleSuggestionClick} />
              </div>
            ) : (
            <div className="w-full flex flex-col relative">
              <ChatViewer messages={messages} theme="dark" sessionId={sessionId} />

              {/* Live Task Progress Card */}
              {taskProgress && (
                <div className="w-full rounded-xl border border-white/[0.06] bg-[#1a1a1a] px-4 py-3 mb-2 animate-in fade-in">
                  <div className="flex items-center gap-2 mb-1.5">
                    <span className={`inline-block w-2 h-2 rounded-full animate-pulse ${
                      taskProgress.mode === 'planning' ? 'bg-blue-400' :
                      taskProgress.mode === 'verification' ? 'bg-emerald-400' : 'bg-amber-400'
                    }`} />
                    <span className="text-sm font-medium text-gray-200">{taskProgress.taskName}</span>
                    <span className={`ml-auto text-[10px] uppercase tracking-wider font-semibold px-1.5 py-0.5 rounded ${
                      taskProgress.mode === 'planning' ? 'bg-blue-900/50 text-blue-300' :
                      taskProgress.mode === 'verification' ? 'bg-emerald-900/50 text-emerald-300' :
                      'bg-amber-900/50 text-amber-300'
                    }`}>{taskProgress.mode}</span>
                  </div>
                  <div className="text-xs text-gray-400">{taskProgress.status}</div>
                  {taskProgress.summary && (
                    <div className="text-[11px] text-gray-500 mt-1 line-clamp-2">{taskProgress.summary}</div>
                  )}
                </div>
              )}
            </div>
            )}
            {/* Bottom spacer: prevents floating composer from covering last message */}
            <div className="h-[200px] md:h-[250px] w-full flex-shrink-0 pointer-events-none" aria-hidden="true" />
            {/* Scroll anchor: hook's observers push scrollTop to reach here */}
            <div ref={messagesEndRef} className="h-px w-full" aria-hidden="true" />
          </div>
        </div>
        {/* Floating Composer Container (Anchored to main bounds, compensated for 6px custom scrollbar width) */}
        <div className="absolute bottom-0 left-0 w-full pointer-events-none z-10 pr-[6px]" 
             style={{ background: 'linear-gradient(to bottom, transparent, rgba(0,0,0,0.5) 40%, rgba(0,0,0,0.95) 100%)' }}>
          <div className="w-full max-w-4xl mx-auto px-1 md:px-4 pb-2 md:pb-8 pt-10 md:pt-24 pointer-events-auto">
            <InputForm
              inputText={inputText}
              onInputChange={setInputText}
              onSubmit={handleSend}
              onCancel={handleStop}
              onCompositionStart={() => {}}
              onCompositionEnd={() => {}}
              onKeyDown={() => {}}
              onToggleEditMode={handleTogglePlanMode}
              onToggleThinking={() => {}}
              onToggleSkipAutoActiveContext={() => {}}
              attachedFiles={attachedFiles}
              onFilesChange={(filesOrUpdater) => {
                if (typeof filesOrUpdater === 'function') {
                  setAttachedFiles(filesOrUpdater)
                } else {
                  setAttachedFiles(filesOrUpdater)
                }
              }}

              onToggleSecurityMode={handleToggleSecurityMode}
              securityModeLabel={securityMode === 'allow' ? 'Allow' : 'Ask'}
              inputFieldRef={inputRef as React.RefObject<HTMLDivElement>}
              isStreaming={isStreaming}
              isWaitingForResponse={isStreaming}
              isComposing={false}
              editModeInfo={{ label: planMode ? 'Plan' : 'Auto', title: planMode ? 'Planning mode' : 'Auto mode', icon: null }}
              thinkingEnabled={true}
              activeFileName={null}
              activeSelection={null}
              skipAutoActiveContext={false}
              contextUsage={null}
              completionIsOpen={false}
              placeholder={isStreaming ? 'Agent is thinking...' : 'Message NGOAgent...'}
            />
            
            <div className="text-center text-xs mt-2 text-gray-600">
              NGOAgent can make mistakes. Consider verifying critical information.
            </div>
          </div>
        </div>
      </main>

      {/* ═══ The Unified Intelligence Hub (Right Pane) ═══ */}
      {isHubOpen && (
        <IntelligenceHub 
          sessionId={sessionId}
          activeTab={hubTab}
          onTabChange={setHubTab}
          onClose={() => setIsHubOpen(false)}
          refreshTrigger={brainRefreshTrigger}
          brainFocusTrigger={brainFocusTrigger}
        />
      )}

      {/* Settings Modal (kept distinct as it's an app-level overlay) */}
      <SettingsPage isOpen={isSettingsOpen} onClose={() => setIsSettingsOpen(false)} />
    </div>
  )
}
