import { useState, useEffect, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface BrainArtifact {
  name: string
  size: number
  mod_time: string
}

interface BrainPanelProps {
  sessionId: string
  refreshTrigger?: number
  focusTrigger?: { file: string; ts: number } | null
  onNavigateDetail?: (file: string | null) => void // Allows Hub to know if we are in detail view
}

const API_BASE = ''

export function BrainPanel({ sessionId, refreshTrigger = 0, focusTrigger = null, onNavigateDetail }: BrainPanelProps) {
  const [artifacts, setArtifacts] = useState<BrainArtifact[]>([])
  const [expanded, setExpanded] = useState<Record<string, string | null>>({})
  const [loading, setLoading] = useState<Record<string, boolean>>({})
  // Detail view state
  const [detailFile, setDetailFile] = useState<string | null>(null)
  const [detailContent, setDetailContent] = useState('')
  const [detailLoading, setDetailLoading] = useState(false)

  const loadArtifacts = useCallback(async () => {
    if (!sessionId) return
    try {
      const res = await fetch(`${API_BASE}/api/v1/brain/list?session_id=${encodeURIComponent(sessionId)}`)
      if (!res.ok) return
      const data = await res.json()
      setArtifacts(data.artifacts || [])
    } catch {
      setArtifacts([])
    }
  }, [sessionId])

  useEffect(() => {
    if (sessionId) {
      loadArtifacts()
      setExpanded({})
      setDetailFile(null)
      onNavigateDetail?.(null)
    }
  }, [sessionId, loadArtifacts])

  // Auto-refresh on step_done events
  useEffect(() => {
    if (sessionId && refreshTrigger > 0) {
      loadArtifacts()
    }
  }, [refreshTrigger, sessionId, loadArtifacts])

  const loadContent = async (name: string): Promise<string> => {
    const res = await fetch(
      `${API_BASE}/api/v1/brain/read?session_id=${encodeURIComponent(sessionId)}&name=${encodeURIComponent(name)}`
    )
    if (!res.ok) throw new Error('Failed')
    const data = await res.json()
    return data.content || ''
  }

  const toggleExpand = async (name: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (expanded[name] !== undefined) {
      setExpanded(prev => { const n = { ...prev }; delete n[name]; return n })
      return
    }
    setLoading(prev => ({ ...prev, [name]: true }))
    try {
      const content = await loadContent(name)
      setExpanded(prev => ({ ...prev, [name]: content }))
    } catch {
      setExpanded(prev => ({ ...prev, [name]: '⚠️ 无法读取' }))
    } finally {
      setLoading(prev => ({ ...prev, [name]: false }))
    }
  }

  const openDetail = async (name: string) => {
    setDetailFile(name)
    onNavigateDetail?.(name)
    setDetailLoading(true)
    try {
      if (expanded[name] !== undefined && expanded[name] !== null) {
        setDetailContent(expanded[name]!)
      } else {
        const content = await loadContent(name)
        setDetailContent(content)
      }
    } catch {
      setDetailContent('⚠️ 无法读取文件')
    } finally {
      setDetailLoading(false)
    }
  }

  // Handle external focus triggers
  useEffect(() => {
    if (focusTrigger?.file) {
      openDetail(focusTrigger.file)
    }
  }, [focusTrigger])


  const formatSize = (bytes: number) => bytes < 1024 ? `${bytes}B` : `${(bytes / 1024).toFixed(1)}KB`
  const fileIcon = (name: string) => name.endsWith('.md') ? '📄' : name.endsWith('.json') ? '📋' : '📎'
  const isMarkdown = (name: string) => name.endsWith('.md')
  const isJson = (name: string) => name.endsWith('.json')

  // Fuzzy checkbox renderer: detects [x], [ ], [/] patterns in any text and renders inline checkboxes
  const renderFuzzyCheckboxText = (text: string) => {
    const parts = text.split(/(\[[ xX/]\])/g)
    if (parts.length === 1) return text
    return parts.map((part, i) => {
      const match = part.match(/^\[([ xX/])\]$/)
      if (!match) return part
      const state = match[1]
      const checked = state === 'x' || state === 'X'
      const inProgress = state === '/'
      const bg = checked ? '#22c55e' : inProgress ? '#f59e0b' : 'transparent'
      const border = checked ? '#22c55e' : inProgress ? '#f59e0b' : '#555'
      const symbol = checked ? '✓' : inProgress ? '◐' : ''
      return (
        <span key={i} style={{
          display: 'inline-block', width: 14, height: 14,
          border: `1.5px solid ${border}`, borderRadius: 3,
          background: bg, textAlign: 'center', lineHeight: '13px',
          fontSize: 10, fontWeight: 700, color: checked || inProgress ? '#000' : '#555',
          marginRight: 5, verticalAlign: 'middle', flexShrink: 0,
        }}>{symbol}</span>
      )
    })
  }

  const renderContent = (name: string, content: string, full = false) => {
    if (isMarkdown(name)) {
      // Ensure single newlines become paragraph breaks (markdown requires double newlines)
      const withBreaks = content.replace(/\n/g, '\n\n')
      return (
        <div className={`brain-md-content ${full ? 'px-5 py-4 text-sm' : 'px-3 py-2 text-xs'} text-gray-300 leading-relaxed`}>
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              // Intercept all text/paragraph nodes to fuzzy-render checkboxes
              p: ({ children }) => <p>{typeof children === 'string' ? renderFuzzyCheckboxText(children) : children}</p>,
              li: ({ children }) => <li style={{ display: 'flex', alignItems: 'baseline', gap: 2 }}>{typeof children === 'string' ? renderFuzzyCheckboxText(children) : children}</li>,
            }}
          >
            {withBreaks}
          </ReactMarkdown>
        </div>
      )
    }
    if (isJson(name)) {
      try {
        const formatted = JSON.stringify(JSON.parse(content), null, 2)
        return (
          <pre className={`${full ? 'px-5 py-4 text-xs' : 'px-3 py-2 text-[11px]'} text-emerald-300/90 font-mono whitespace-pre-wrap break-words bg-emerald-950/20 leading-relaxed`}>
            {formatted}
          </pre>
        )
      } catch { /* fallback */ }
    }
    return (
      <pre className={`${full ? 'px-5 py-4 text-xs' : 'px-3 py-2 text-[11px]'} text-gray-400 font-mono whitespace-pre-wrap break-words leading-relaxed`}>
        {content}
      </pre>
    )
  }

  // ═══ Detail View ═══
  if (detailFile) {
    return (
      <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'slideInRight 0.25s cubic-bezier(0.4, 0, 0.2, 1)' }}>
        {/* Detail Header */}
        <div className="flex items-center gap-3 px-5 py-3 border-b border-white/[0.04] bg-white/[0.02]">
          <button
            onClick={() => {
              setDetailFile(null)
              onNavigateDetail?.(null)
            }}
            className="p-1.5 -ml-1 rounded-full hover:bg-white/10 text-gray-400 hover:text-white transition-colors flex items-center gap-1 group"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-hover:-translate-x-0.5 transition-transform">
              <path d="M15 18l-6-6 6-6"/>
            </svg>
            <span className="text-[11px] font-medium tracking-wide">返回</span>
          </button>
          <div className="h-4 w-px bg-white/10 mx-1"></div>
          <span className="text-sm shrink-0 drop-shadow-md">{fileIcon(detailFile)}</span>
          <span className="text-sm font-medium text-gray-200 truncate flex-1 tracking-wide">{detailFile}</span>
        </div>

        {/* Detail Content */}
        <div className="flex-1 overflow-y-auto">
          {detailLoading ? (
            <div className="flex items-center justify-center py-16 text-gray-500 text-sm">加载中…</div>
          ) : (
            renderContent(detailFile, detailContent, true)
          )}
        </div>

        {mdStyles}
      </div>
    )
  }

  // ═══ List View with Accordion ═══
  return (
    <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'fadeIn 0.2s ease-out' }}>
      {/* Tools / Actions Header */}
      <div className="flex items-center justify-between px-5 py-2.5 bg-white/[0.02] border-b border-white/[0.04]">
        <div className="flex items-center gap-2">
          {artifacts.length > 0 && (
            <span className="text-[10px] uppercase tracking-widest text-gray-500 font-semibold">{artifacts.length} Items</span>
          )}
        </div>
        <button onClick={loadArtifacts} className="p-1.5 rounded-full hover:bg-white/10 text-gray-500 hover:text-gray-200 transition-colors group" title="刷新目录">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-active:rotate-180 transition-transform duration-300"><path d="M23 4v6h-6M1 20v-6h6"/><path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
        </button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {artifacts.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-28 relative overflow-hidden">
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[180px] h-[180px] bg-blue-500/10 rounded-full blur-[50px] pointer-events-none" />
            <div className="relative z-10 w-16 h-16 rounded-3xl bg-white/[0.03] border border-white/[0.05] shadow-[0_8px_32px_rgba(0,0,0,0.3)] flex items-center justify-center mb-6 ring-1 ring-white/5">
              <span className="text-3xl opacity-80 drop-shadow-md">📝</span>
            </div>
            <span className="text-[13px] font-semibold tracking-wider text-gray-300 mb-1.5 z-10">Empty Workspace</span>
            <span className="text-[11px] font-medium tracking-wide text-gray-500 z-10">Brain 区没有任何产物文件</span>
          </div>
        ) : (
          <div className="divide-y divide-white/[0.04]">
            {artifacts
              .filter(f => {
                // Hide internal system files from the user
                const n = f.name
                if (n === 'last_notification.json') return false
                if (n.endsWith('.metadata.json')) return false
                if (n.includes('.resolved')) return false
                return true
              })
              .map((file) => {
              const isExpanded = expanded[file.name] !== undefined
              const content = expanded[file.name]
              const isLoading = loading[file.name]
              const dateObj = file.mod_time ? new Date(file.mod_time) : null
              const isToday = dateObj ? dateObj.toDateString() === new Date().toDateString() : false

              return (
                <div key={file.name} className="flex flex-col bg-transparent group">
                  <div
                    className="flex items-center gap-3 px-5 py-3.5 cursor-pointer hover:bg-white/[0.03] transition-colors"
                    onClick={(e) => toggleExpand(file.name, e)}
                  >
                    <div className="w-4 flex justify-center shrink-0">
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"
                           className={`text-gray-500 transition-transform duration-300 ${isExpanded ? 'rotate-90' : ''}`}>
                        <path d="M9 18l6-6-6-6"/>
                      </svg>
                    </div>
                    
                    <span className="text-base shrink-0 drop-shadow-sm group-hover:scale-110 transition-transform">{fileIcon(file.name)}</span>
                    
                    <div className="flex-1 min-w-0 pr-2">
                       <div className="text-[13px] font-medium text-gray-200 truncate group-hover:text-blue-200 transition-colors tracking-wide">{file.name}</div>
                       <div className="flex items-center gap-2 mt-1">
                         <span className="text-[10px] font-mono text-gray-500 tracking-wider bg-white/5 px-1.5 py-0.5 rounded">{formatSize(file.size)}</span>
                         {dateObj && (
                           <span className="text-[10px] text-gray-600">
                             {isToday 
                               ? dateObj.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                               : dateObj.toLocaleDateString([], { month: 'short', day: 'numeric' })
                             }
                           </span>
                         )}
                       </div>
                    </div>

                    <button
                      onClick={(e) => { e.stopPropagation(); openDetail(file.name) }}
                      className="opacity-0 group-hover:opacity-100 p-1.5 rounded-full hover:bg-blue-500/20 text-blue-400 transition-all hover:scale-110 shadow-sm"
                      title="全开检视"
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M21 3H14M21 3V10M21 3L13 11M3 21H10M3 21V14M3 21L11 13"/></svg>
                    </button>
                  </div>
                  
                  {isExpanded && (
                    <div className="bg-black/20 border-t border-white/[0.02]" style={{ animation: 'slideDown 0.2s cubic-bezier(0.4, 0, 0.2, 1)' }}>
                      {isLoading ? (
                        <div className="flex items-center justify-center py-6 text-gray-500">
                           <div className="w-4 h-4 rounded-full border-2 border-gray-600 border-t-gray-300 animate-spin"></div>
                        </div>
                      ) : content ? (
                        <div className="max-h-[300px] overflow-y-auto px-1 relative">
                          {renderContent(file.name, content)}
                          <div className="absolute top-0 right-0 bottom-0 w-3 bg-gradient-to-l from-[#111] to-transparent pointer-events-none"></div>
                        </div>
                      ) : null}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Footer — click SID to copy brain path */}
      <div className="px-3 py-2.5 border-t border-white/5">
        <div
          className="text-[11px] text-gray-500 font-mono break-all cursor-pointer hover:text-gray-300 transition-colors"
          title="点击复制 Brain 目录路径"
          onClick={() => navigator.clipboard.writeText(`~/.ngoagent/brain/${sessionId}`)}
        >
          {sessionId || '—'}
        </div>
      </div>

      {mdStyles}
    </div>
  )
}

const mdStyles = (
  <style>{`
    .brain-md-content h1 { font-size: 1.15em; font-weight: 700; margin: 0.8em 0 0.4em; color: #e5e5e5; border-bottom: 1px solid rgba(255,255,255,0.08); padding-bottom: 0.3em; }
    .brain-md-content h2 { font-size: 1.05em; font-weight: 600; margin: 0.7em 0 0.3em; color: #d4d4d4; }
    .brain-md-content h3 { font-size: 0.95em; font-weight: 600; margin: 0.5em 0 0.2em; color: #a3a3a3; }
    .brain-md-content p { margin: 0.4em 0; }
    .brain-md-content ul, .brain-md-content ol { padding-left: 1.4em; margin: 0.3em 0; }
    .brain-md-content li { margin: 0.15em 0; }
    .brain-md-content li::marker { color: #525252; }
    .brain-md-content code { background: rgba(255,255,255,0.08); padding: 0.1em 0.35em; border-radius: 3px; font-size: 0.85em; color: #e879f9; }
    .brain-md-content pre { background: rgba(0,0,0,0.35); padding: 0.7em; border-radius: 6px; overflow-x: auto; margin: 0.5em 0; }
    .brain-md-content pre code { background: none; padding: 0; color: #d4d4d4; }
    .brain-md-content blockquote { border-left: 3px solid rgba(96,165,250,0.4); padding-left: 0.8em; margin: 0.5em 0; color: #a3a3a3; }
    .brain-md-content table { border-collapse: collapse; width: 100%; margin: 0.5em 0; font-size: 0.85em; }
    .brain-md-content th, .brain-md-content td { border: 1px solid rgba(255,255,255,0.1); padding: 0.35em 0.6em; text-align: left; }
    .brain-md-content th { background: rgba(255,255,255,0.06); font-weight: 600; color: #d4d4d4; }
    .brain-md-content a { color: #60a5fa; text-decoration: none; }
    .brain-md-content a:hover { text-decoration: underline; }
    .brain-md-content hr { border: none; border-top: 1px solid rgba(255,255,255,0.08); margin: 0.8em 0; }
    .brain-md-content input[type="checkbox"] { appearance: none; -webkit-appearance: none; width: 14px; height: 14px; margin-right: 0.4em; border: 1.5px solid #525252; border-radius: 3px; vertical-align: middle; position: relative; top: -1px; cursor: default; flex-shrink: 0; }
    .brain-md-content input[type="checkbox"]:checked { background: #22c55e; border-color: #22c55e; }
    .brain-md-content input[type="checkbox"]:checked::before { content: ""; display: block; width: 4px; height: 8px; border: solid #fff; border-width: 0 2px 2px 0; transform: rotate(45deg); position: absolute; top: 1px; left: 4px; }
    .brain-md-content strong { color: #f5f5f5; }
    .brain-md-content em { color: #a3a3a3; }
  `}</style>
)
