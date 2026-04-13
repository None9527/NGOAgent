import { useState, useEffect, useRef, useCallback, lazy, Suspense } from 'react'
import { ChatViewer } from './renderers/ChatViewer'
import type { ChatViewerHandle } from './renderers/ChatViewer'
import { InputForm } from './renderers/InputForm'
import { api, getApiBase, getAuthToken } from './chat/api'
import { chatStream, checkActiveRun } from './chat/streamHandler'
import type { ChatMessageData } from './chat/types'
import { useConfig } from './providers/ConfigProvider'
import { useSession } from './providers/SessionProvider'
import { useStream } from './providers/StreamProvider'
import { useHub } from './providers/HubProvider'
import { useUIStore } from './stores/uiStore'
import { useMessageStore } from './stores/messageStore'

// Import new structural components
import { Sidebar } from './components/Sidebar'
import { TopNavbar } from './components/TopNavbar'
import { WelcomeScreen } from './components/WelcomeScreen'
import { ApprovalBanner } from './components/ApprovalBanner'
import { PlanReviewBanner } from './components/PlanReviewBanner'
import { TaskProgressBar } from './components/TaskProgressBar'
// Lazy-load heavy components not needed on initial render
const IntelligenceHub = lazy(() => import('./components/IntelligenceHub/index').then(m => ({ default: m.IntelligenceHub })))
const SettingsPage = lazy(() => import('./components/SettingsPage').then(m => ({ default: m.SettingsPage })))
const ConnectPage = lazy(() => import('./components/ConnectPage').then(m => ({ default: m.ConnectPage })))
import { SubagentDock } from './components/SubagentDock'

import { ScrollToBottomFab } from './components/ScrollToBottomFab'

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
  const { sessionId, sessions, setSessionId, pendingScrollToEnd: pendingScrollToEndRef, loadHistory } = session
  const messages = useMessageStore(s => s.messages)
  const { planMode, availableModels, health } = config
  const {
    isStreaming, streamPhase, connectionState, taskProgress,
    streamCallbacks, cancelRef,
    scrollToBottom, resetToBottom,
    enterStreamingMode, exitStreamingMode,
    pendingApprovals, setPendingApprovals,
    planReview, setPlanReview,
    setIsStreaming, setSubagentProgress,
  } = stream


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
  } = useUIStore()

  const [connected, setConnected] = useState(() => {
    try {
      const t = getAuthToken()
      return !!(t && t.trim())
    } catch { return false }
  })
  const inputRef = useRef<HTMLDivElement>(null)
  const chatViewerRef = useRef<ChatViewerHandle>(null)

  // Phase 3: scroll control is fully managed by ScrollProvider.
  // ChatVirtualList registers virtualizer capability; StreamProvider consumes it.

  // Measure floating composer height for dynamic footer spacer.
  // Debounced: only update when height changes ≥20px to prevent
  // scroll jitter from micro-resizes (TaskProgressBar/SubagentDock).
  const composerRef = useRef<HTMLDivElement>(null)
  const [composerHeight, setComposerHeight] = useState(200)
  const composerHeightRef = useRef(200)
  useEffect(() => {
    const el = composerRef.current
    if (!el) return
    let rafId = 0
    const ro = new ResizeObserver(([entry]) => {
      const h = Math.ceil(entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height)
      // Ignore micro-resizes: only update when change ≥ 20px
      if (Math.abs(h - composerHeightRef.current) < 20) return
      composerHeightRef.current = h
      if (rafId) cancelAnimationFrame(rafId)
      rafId = requestAnimationFrame(() => {
        rafId = 0
        setComposerHeight(h)
      })
    })
    ro.observe(el)
    return () => { ro.disconnect(); if (rafId) cancelAnimationFrame(rafId) }
  }, [])

  // Reactive scroll-to-end: fires after React commits state from loadHistory
  useEffect(() => {
    if (pendingScrollToEndRef.current && messages.length > 0) {
      pendingScrollToEndRef.current = false
      resetToBottom()
    }
  }, [messages, pendingScrollToEndRef, resetToBottom])

  // Initialize: load existing sessions + health after connection is established
  useEffect(() => {
    if (!connected) return
    document.documentElement.classList.add('dark')
    ;(async () => {
      try {
        // Lazy token validation
        const configRes = await fetch(`${getApiBase()}/v1/config`, {
          headers: { 'Authorization': `Bearer ${getAuthToken()}` },
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



  // Retry — re-generate the last assistant response
  // Phase 2: uses messageStore.stripFromLastUser() (fixes A3 off-by-one)
  const handleRetry = useCallback(async () => {
    if (!sessionId) return
    try {
      const { last_message } = await api.retry(sessionId)
      if (!last_message) return
      // Strip from last user message onward (inclusive)
      useMessageStore.getState().stripFromLastUser()
      // Re-send through normal chat
      const userMsg: ChatMessageData = {
        uuid: `user-${Date.now()}`,
        timestamp: new Date().toISOString(),
        type: 'user',
        message: { role: 'user', parts: [{ text: last_message }] },
      }
      useMessageStore.getState().add(userMsg)
      setIsStreaming(true)
      setSubagentProgress([])
      enterStreamingMode()
      scrollToBottom('instant')
      const handle = chatStream(last_message, sessionId, planMode, streamCallbacks)
      cancelRef.current = handle.cancel
    } catch (err) {
      console.error('[retry] failed:', err)
    }
  }, [sessionId, planMode, streamCallbacks, setIsStreaming, setSubagentProgress, enterStreamingMode, scrollToBottom, cancelRef])

  // Send message — overrideText allows direct send from banner buttons
  const handleSend = useCallback(async (e?: React.FormEvent, overrideText?: string) => {
    if (e) e.preventDefault()
    const textToSend = (overrideText || inputText).trim()
    if (!textToSend || isStreaming) return

    setInputText('')

    // Lazy session creation: use unified session.newSession() path (D1 fix)
    // This ensures messages/msgIndexRef are properly cleared + sidebar refreshed
    let sid = sessionId
    if (!sid) {
      try {
        sid = await session.newSession()
      } catch (err) {
        console.error('Failed to create session', err)
        return
      }
    }

    // Build message text with file attachments if any
    const uploadedFiles = attachedFiles.filter(f => f.status === 'uploaded' && f.path)
    let finalText = textToSend
    if (uploadedFiles.length > 0) {
      const getRole = (t: string) => {
        if (t.startsWith('image/'))  return 'reference_image'
        if (t.startsWith('audio/'))  return 'reference_audio'
        if (t.startsWith('video/'))  return 'reference_video'
        return 'reference_file'
      }
      const fileEntries = uploadedFiles
        .map(f => `  <file name="${f.name}" path="${f.path}" type="${f.type}" role="${getRole(f.type)}" />`)
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
    useMessageStore.getState().add(userMsg)
    setIsStreaming(true)
    setSubagentProgress([]) // Clear previous sub-agent data
    enterStreamingMode()
    scrollToBottom('instant') // User action: snap immediately, no animation

    const handle = chatStream(finalText, sid, planMode, streamCallbacks)
    cancelRef.current = handle.cancel
  }, [inputText, isStreaming, sessionId, attachedFiles, streamCallbacks, planMode, session,
      setInputText, setAttachedFiles,
      setIsStreaming, setSubagentProgress, enterStreamingMode, scrollToBottom, cancelRef])

  // Gate: show ConnectPage until authenticated
  if (!connected) {
    return <Suspense fallback={<div className="flex h-screen items-center justify-center"><span className="animate-pulse text-white/40">Loading...</span></div>}><ConnectPage onConnected={() => setConnected(true)} /></Suspense>
  }

  const isKernelActive = streamPhase === 'streaming' || streamPhase === 'auto_waking'

  return (
    <div className="app-shell flex h-[100dvh] w-screen overflow-hidden text-gray-200 font-sans selection:bg-cyan-400/25">
      
      <Sidebar 
        isOpen={isSidebarOpen} 
        onToggle={() => setIsSidebarOpen(!isSidebarOpen)}
        sessions={sessions}
        currentSessionId={sessionId}
        onSelectSession={handleSelectSession}
        onNewSession={handleNewSession}
        onDeleteSession={handleDeleteSession}
        onRenameSession={handleRenameSession}
      />

      <main className="app-main flex-1 flex flex-col relative w-full h-full min-w-0">
        
        <TopNavbar 
          onToggleSidebar={() => setIsSidebarOpen(!isSidebarOpen)} 
          modelName={health?.model || ''}
          onToggleHub={hub.toggle}
          isHubOpen={hub.isOpen}
          connectionState={connectionState}
          availableModels={availableModels}
          currentModel={health?.model || ''}
          onModelSelect={handleModelSwitch}
          onOpenSettings={() => setIsSettingsOpen(true)}
          planMode={planMode}
          streamPhase={streamPhase}
          taskProgress={taskProgress}
        />

        {/* Banners absolutely centered over the read-column */}
        <ApprovalBanner pendingApprovals={pendingApprovals} setPendingApprovals={setPendingApprovals} />

        {/* ── Chat Area: @tanstack/virtual self-manages scroll at 100% height ── */}
        <div className="flex-1 min-h-0 w-full relative z-0">
          {messages.length === 0 ? (
            <div className="flex items-center justify-center h-full">
              <WelcomeScreen />
            </div>
          ) : (
            <ChatViewer
              ref={chatViewerRef}
              messages={messages}
              theme="dark"
              sessionId={sessionId}
              onRetry={handleRetry}
              isStreaming={isKernelActive}
              composerHeight={composerHeight}
            />
          )}
          {/* FAB: scroll-to-bottom when user scrolls up */}
          {messages.length > 0 && <ScrollToBottomFab />}
        </div>

        {/* Floating Composer Container (Anchored to main bounds, compensated for 6px custom scrollbar width) */}
        <div ref={composerRef} className="composer-dock absolute bottom-0 left-0 w-full pointer-events-none z-10 pr-[6px]">
          <div className="w-full max-w-4xl mx-auto px-2 md:px-5 pb-2 md:pb-6 pt-10 md:pt-24 pointer-events-auto">
            <TaskProgressBar
              isStreaming={isKernelActive}
              taskProgress={taskProgress}
              isWaitingPlan={planReview !== null}
              isWaitingApproval={pendingApprovals.length > 0}
            />
            <SubagentDock />
            {/* Plan Review Banner — width aligned with InputForm */}
            {planReview && (
              <PlanReviewBanner planReview={planReview} setPlanReview={setPlanReview} onSend={handleSend} />
            )}
            <InputForm
              inputText={inputText}
              inputFieldRef={inputRef as React.RefObject<HTMLDivElement>}
              onInputChange={setInputText}
              onSubmit={handleSend}
              attachedFiles={attachedFiles}
              onFilesChange={setAttachedFiles}
            />
            
            <div className="composer-footnote text-center text-xs mt-2">
              Kernel actions are streamed live. Verify critical outputs before applying them.
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
