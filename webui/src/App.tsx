import { useState, useEffect, useRef, useCallback, lazy, Suspense } from 'react'
import { ChatViewer } from './renderers/ChatViewer'
import { InputForm } from './renderers/InputForm'
import { api, getApiBase } from './chat/api'
import { chatStream, checkActiveRun } from './chat/streamHandler'
import type { ChatMessageData } from './chat/types'
import { useConfig } from './providers/ConfigProvider'
import { useSession } from './providers/SessionProvider'
import { useStream } from './providers/StreamProvider'
import { useHub } from './providers/HubProvider'
import { useUIStore } from './stores/uiStore'

// Import new structural components
import { Sidebar } from './components/Sidebar'
import { TopNavbar } from './components/TopNavbar'
import { WelcomeScreen } from './components/WelcomeScreen'
// Lazy-load heavy components not needed on initial render
const IntelligenceHub = lazy(() => import('./components/IntelligenceHub/index').then(m => ({ default: m.IntelligenceHub })))
const SettingsPage = lazy(() => import('./components/SettingsPage').then(m => ({ default: m.SettingsPage })))
const ConnectPage = lazy(() => import('./components/ConnectPage').then(m => ({ default: m.ConnectPage })))
import { SubagentDock } from './components/SubagentDock'

// Import styles
import './renderers/styles/variables.css'
import './renderers/styles/timeline.css'
import './renderers/styles/components.css'

export default function App() {
  // ── Providers ──
  const config = useConfig()
  const session = useSession()
  const stream = useStream()
  const hub = useHub()
  const { sessionId, sessions, messages, setSessionId, setSessions, setMessages, pendingScrollToEnd, loadHistory } = session
  const { planMode, availableModels, health } = config
  const {
    isStreaming, streamPhase, connectionState, taskProgress, subagentProgress,
    streamCallbacks, cancelRef,
    scrollContainerRef, handleScroll, scrollToBottom, resetToBottom,
    followOutput, handleAtBottomChange, userScrolledUpRef, isStreamingRef,
    enterStreamingMode, exitStreamingMode,
    pendingApprovals, setPendingApprovals,
    planReview, setPlanReview,
    setIsStreaming, setSubagentProgress,
  } = stream
  const subagentStats = subagentProgress.length > 0
    ? { running: subagentProgress.filter(e => e.status === 'running').length, total: subagentProgress.length }
    : null

  // ── UIStore (replaces 6 scattered useState) ──
  const {
    sidebarOpen: isSidebarOpen,
    setSidebarOpen: setIsSidebarOpen,
    settingsOpen: isSettingsOpen,
    setSettingsOpen: setIsSettingsOpen,
    inputText,
    setInputText,
    attachedFiles,
    setAttachedFiles,
    planFeedbackInput,
    setPlanFeedbackInput,
    showFeedbackInput,
    setShowFeedbackInput,
  } = useUIStore()

  const [connected, setConnected] = useState(() => {
    try {
      const t = localStorage.getItem('AUTH_TOKEN')
      return !!(t && t.trim())
    } catch { return false }
  })
  const inputRef = useRef<HTMLDivElement>(null)
  // scrollEl: the mounted DOM node — used as Virtuoso's customScrollParent.
  // Must be state (not ref.current) so Virtuoso re-renders after mount with real element.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null)
  useEffect(() => { setScrollEl(scrollContainerRef.current) }, [scrollContainerRef])

  // Reactive scroll-to-end: fires after React commits state from loadHistory
  useEffect(() => {
    if (pendingScrollToEnd.current && messages.length > 0) {
      pendingScrollToEnd.current = false
      resetToBottom()
    }
  }, [messages, resetToBottom])

  // Initialize: load existing sessions + health after connection is established
  useEffect(() => {
    if (!connected) return
    document.documentElement.classList.add('dark')
    ;(async () => {
      try {
        // Lazy token validation
        const configRes = await fetch(`${getApiBase()}/v1/config`, {
          headers: { 'Authorization': `Bearer ${localStorage.getItem('AUTH_TOKEN') || ''}` },
          signal: AbortSignal.timeout(5000),
        })
        if (configRes.status === 401) {
          localStorage.removeItem('AUTH_TOKEN')
          setConnected(false)
          return
        }

        await config.initialize()
        const { activeSessionId } = await session.initialize()

        if (activeSessionId) {
          await loadHistory(activeSessionId)

          const runStatus = await checkActiveRun(activeSessionId)
          if (runStatus.active && !runStatus.done) {
            console.log('[App] Active run detected, reconnecting SSE (lastSeq=%d)...', runStatus.lastSeq)
            stream.reconnect(activeSessionId, runStatus.lastSeq)
          }
        }
      } catch (err: unknown) {
        console.error('Init failed after connect:', err)
      }
    })()
  }, [connected])

  const handleNewSession = useCallback(async () => {
    try {
      await session.newSession()
      if (window.innerWidth < 768) setIsSidebarOpen(false) 
    } catch (err) {
      console.error('Failed to create new session', err)
    }
  }, [session, setIsSidebarOpen])

  const handleSelectSession = useCallback(async (id: string) => {
    // Disconnect current SSE stream before switching — but isolate the cancel
    // to prevent the old stream's async onEnd from overwriting reconnect state.
    if (cancelRef.current) {
      const oldCancel = cancelRef.current
      // Clear ref BEFORE canceling so async onEnd can't null our new handler
      cancelRef.current = null
      setIsStreaming(false)
      exitStreamingMode()
      setPendingApprovals([])
      setPlanReview(null)
      // Now abort — onEnd will fire asynchronously but cancelRef is already null
      // so onEnd's `cancelRef.current = null` is harmless
      oldCancel()
    }
    setSessionId(id)
    await loadHistory(id)

    // Small delay to let the old stream's async onEnd settle before we reconnect
    await new Promise(r => setTimeout(r, 50))

    // Check if target session has an active run — reconnect if so
    try {
      const runStatus = await checkActiveRun(id)
      if (runStatus.active && !runStatus.done) {
        console.log('[App] Target session has active run, reconnecting SSE (lastSeq=%d)...', runStatus.lastSeq)
        stream.reconnect(id, runStatus.lastSeq)
      }
    } catch (err) {
      console.warn('[App] Failed to check active run on session switch:', err)
    }

    if (window.innerWidth < 768) setIsSidebarOpen(false)
  }, [cancelRef, setIsStreaming, exitStreamingMode, setPendingApprovals, setPlanReview, setSessionId, loadHistory, stream, setIsSidebarOpen])

  const handleDeleteSession = useCallback((id: string) => session.deleteSession(id), [session])
  const handleRenameSession = useCallback((id: string, t: string) => session.renameSession(id, t), [session])

  const handleModelSwitch = useCallback((modelName: string) => config.switchModel(modelName), [config])

  const handleSuggestionClick = useCallback((suggestionText: string) => {
    setInputText(suggestionText)
    if (inputRef.current) {
      inputRef.current.focus()
    }
  }, [setInputText])

  // Retry — re-generate the last assistant response
  // Flow: strip backend history → strip UI messages → re-send through normal chat
  const sendRef = useRef<((e?: React.FormEvent, overrideText?: string) => void) | undefined>(undefined)
  const handleRetry = useCallback(async () => {
    if (!sessionId) return
    try {
      const { last_message } = await api.retry(sessionId)
      if (!last_message) return
      // Strip assistant/tool messages from UI (keep up to last user msg)
      setMessages(prev => {
        const lastUserIdx = [...prev].reverse().findIndex(m => m.type === 'user')
        if (lastUserIdx === -1) return prev
        return prev.slice(0, prev.length - lastUserIdx)
      })
      // Directly re-send: add user message + start stream (bypass handleSend to avoid stale isStreaming closure)
      const userMsg: ChatMessageData = {
        uuid: `user-${Date.now()}`,
        timestamp: new Date().toISOString(),
        type: 'user',
        message: { role: 'user', parts: [{ text: last_message }] },
      }
      setMessages(prev => [...prev, userMsg])
      setIsStreaming(true)
      setSubagentProgress([])
      enterStreamingMode()
      scrollToBottom('instant')
      const handle = chatStream(last_message, sessionId, planMode, streamCallbacks)
      cancelRef.current = handle.cancel
    } catch (err) {
      console.error('[retry] failed:', err)
    }
  }, [sessionId, planMode, streamCallbacks])

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
        // Immediately add to sidebar before refresh
        setSessions(prev => [{
          id: sid, title: '', channel: 'web',
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        }, ...prev])
      } catch (err) {
        console.error('Failed to create session', err)
        return
      }
    }

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
    setSubagentProgress([]) // Clear previous sub-agent data
    enterStreamingMode()
    scrollToBottom('instant') // User action: snap immediately, no animation

    const handle = chatStream(finalText, sid, planMode, streamCallbacks)
    cancelRef.current = handle.cancel
  }, [inputText, isStreaming, sessionId, attachedFiles, streamCallbacks])
  sendRef.current = handleSend

  // Gate: show ConnectPage until authenticated
  if (!connected) {
    return <Suspense fallback={<div className="flex h-screen items-center justify-center"><span className="animate-pulse text-white/40">Loading...</span></div>}><ConnectPage onConnected={() => setConnected(true)} /></Suspense>
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
        onOpenHubTab={(tab: 'brain' | 'knowledge' | 'cron' | 'skills') => hub.openTab(tab)}
        onOpenSettings={() => setIsSettingsOpen(true)}
      />

      <main className="flex-1 flex flex-col relative w-full h-full min-w-0 bg-transparent">
        
        <TopNavbar 
          onToggleSidebar={() => setIsSidebarOpen(!isSidebarOpen)} 
          modelName={health?.model || ''}
          onToggleHub={hub.toggle}
          isHubOpen={hub.isOpen}
          connectionState={connectionState}
          isStreaming={isStreaming}
          subagentStats={subagentStats}
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

        {/* ── Scrollable Chat Area ── */}
        {/* The hook's MutationObserver + ResizeObserver watches this container */}
        {/* NOTE: Do NOT add CSS scroll-smooth here — it overrides scrollTop assignments and causes */}
        {/* scroll lag during streaming. The hook uses scrollTo({behavior:'smooth'}) only for explicit user scrolls. */}
        <div ref={scrollContainerRef} onScroll={handleScroll} className="flex-1 min-h-0 w-full overflow-y-scroll relative z-0" style={{ WebkitOverflowScrolling: 'touch' }}>
          {/* Reading Column Container */}
          <div className="w-full max-w-4xl mx-auto flex flex-col min-h-full pt-10 md:pt-20 px-1 md:px-4 relative">
            {messages.length === 0 ? (
              <div className="flex-1 flex items-center justify-center">
                <WelcomeScreen onSuggestionClick={handleSuggestionClick} />
              </div>
            ) : (
            <div className="w-full flex flex-col relative min-h-full">
              <ChatViewer
                messages={messages}
                theme="dark"
                sessionId={sessionId}
                onRetry={handleRetry}
                customScrollParent={scrollEl}
                isStreaming={streamPhase === 'streaming' || streamPhase === 'auto_waking'}
                followOutput={followOutput}
                onAtBottomChange={handleAtBottomChange}
                userScrolledUpRef={userScrolledUpRef}
                isStreamingRef={isStreamingRef}
              />



            </div>
            )}
          </div>
        </div>
        {/* Floating Composer Container (Anchored to main bounds, compensated for 6px custom scrollbar width) */}
        <div className="absolute bottom-0 left-0 w-full pointer-events-none z-10 pr-[6px]" 
             style={{ background: 'linear-gradient(to bottom, transparent, rgba(0,0,0,0.5) 40%, rgba(0,0,0,0.95) 100%)' }}>
          <div className="w-full max-w-4xl mx-auto px-1 md:px-4 pb-2 md:pb-8 pt-10 md:pt-24 pointer-events-auto">
            {/* Task Progress Banner — fixed above input, appears/disappears with task_boundary events */}
            {taskProgress && (
              <div className="px-1 mb-2">
              <div className="w-full rounded-xl border border-white/[0.08] bg-[#1c1c1c] px-4 py-2.5 flex items-center gap-3 transition-all duration-200">
                <span className={`shrink-0 inline-block w-2 h-2 rounded-full animate-pulse ${
                  taskProgress.mode === 'planning' ? 'bg-blue-400' :
                  taskProgress.mode === 'verification' ? 'bg-emerald-400' : 'bg-amber-400'
                }`} />
                <span className="text-sm font-medium text-gray-200 truncate flex-1">{taskProgress.taskName}</span>
                <span className={`shrink-0 text-[10px] uppercase tracking-wider font-semibold px-1.5 py-0.5 rounded ${
                  taskProgress.mode === 'planning' ? 'bg-blue-900/50 text-blue-300' :
                  taskProgress.mode === 'verification' ? 'bg-emerald-900/50 text-emerald-300' :
                  'bg-amber-900/50 text-amber-300'
                }`}>{taskProgress.mode}</span>
                {taskProgress.status && (
                  <span className="shrink-0 text-[11px] text-gray-500 hidden sm:block truncate max-w-[140px]">{taskProgress.status}</span>
                )}
              </div>
              </div>
            )}
            <SubagentDock />
            {/* Plan Review Banner — width aligned with InputForm */}
            {planReview && (
              <div className="px-1 mb-2">
              <div className="w-full rounded-2xl border border-blue-500/30 bg-black/60 backdrop-blur-[40px] px-5 py-4 flex flex-col gap-3 shadow-[0_20px_40px_-10px_rgba(0,0,0,0.8)]"
                style={{ animation: 'slideDown 0.3s cubic-bezier(0.4, 0, 0.2, 1)' }}>
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
                      hub.openTab('brain')
                      if (planReview.paths.length > 0) {
                        hub.focusFile(planReview.paths[0].split('/').pop() || 'plan.md')
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
            <InputForm
              inputText={inputText}
              inputFieldRef={inputRef as React.RefObject<HTMLDivElement>}
              onInputChange={setInputText}
              onSubmit={handleSend}
              attachedFiles={attachedFiles}
              onFilesChange={setAttachedFiles}
            />
            
            <div className="text-center text-xs mt-2 text-gray-600">
              NGOAgent can make mistakes. Consider verifying critical information.
            </div>
          </div>
        </div>
      </main>

      {/* ═══ The Unified Intelligence Hub (Right Pane) ═══ */}
      {hub.isOpen && (
        <Suspense fallback={null}>
          <IntelligenceHub 
            sessionId={sessionId}
            activeTab={hub.tab}
            onTabChange={hub.setTab}
            onClose={hub.close}
            refreshTrigger={hub.brainRefreshTrigger}
            brainFocusTrigger={hub.brainFocusTrigger}
          />
        </Suspense>
      )}

      {/* Settings Modal (kept distinct as it's an app-level overlay) */}
      <Suspense fallback={null}>
        <SettingsPage isOpen={isSettingsOpen} onClose={() => setIsSettingsOpen(false)} />
      </Suspense>
    </div>
  )
}
