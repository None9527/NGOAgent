import { authFetch } from '../chat/api'
import { useState, useEffect, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { MdStyles } from './shared/mdStyles'

const API_BASE = ''

interface KIItem {
  id: string
  title: string
  summary: string
  tags: string[]
  sources: string[]
  created_at: string
  updated_at: string
}

interface ArtifactFile {
  name: string
  size: number
  mod_time: string
}

interface ArtifactContent {
  name: string
  content: string
  loading: boolean
}

interface KIManagerProps {
  refreshTrigger?: number
  onNavigateDetail?: (id: string | null) => void
}

export function KIManager({ refreshTrigger = 0, onNavigateDetail }: KIManagerProps) {
  const [items, setItems] = useState<KIItem[]>([])
  const [search, setSearch] = useState('')
  const [selectedKI, setSelectedKI] = useState<KIItem | null>(null)
  const [artifacts, setArtifacts] = useState<ArtifactFile[]>([])
  const [artifactContents, setArtifactContents] = useState<ArtifactContent[]>([])

  const loadItems = useCallback(async () => {
    try {
      const res = await authFetch(`${API_BASE}/api/v1/ki/list`)
      if (!res.ok) return
      const data = await res.json()
      setItems(data.items || [])
    } catch { setItems([]) }
  }, [])

  useEffect(() => {
    loadItems(); setSelectedKI(null); setArtifactContents([]); onNavigateDetail?.(null);
  }, [loadItems])

  // Auto-refresh when refreshTrigger changes (e.g. after KI distillation)
  useEffect(() => {
    if (refreshTrigger > 0) loadItems()
  }, [refreshTrigger, loadItems])

  const openKI = async (item: KIItem) => {
    setSelectedKI(item)
    onNavigateDetail?.(item.id)
    setArtifactContents([])
    try {
      const res = await authFetch(`${API_BASE}/api/v1/ki/artifacts?id=${encodeURIComponent(item.id)}`)
      if (!res.ok) return
      const data = await res.json()
      const files: ArtifactFile[] = data.artifacts || []
      setArtifacts(files)

      // Load ALL artifact contents in parallel and render inline
      const contents: ArtifactContent[] = files.map(f => ({ name: f.name, content: '', loading: true }))
      setArtifactContents(contents)

      await Promise.all(files.map(async (f, i) => {
        try {
          const r = await authFetch(
            `${API_BASE}/api/v1/ki/artifact/read?id=${encodeURIComponent(item.id)}&name=${encodeURIComponent(f.name)}`
          )
          const d = await r.json()
          setArtifactContents(prev => {
            const next = [...prev]
            next[i] = { name: f.name, content: d.content || '', loading: false }
            return next
          })
        } catch {
          setArtifactContents(prev => {
            const next = [...prev]
            next[i] = { name: f.name, content: '⚠️ 无法读取', loading: false }
            return next
          })
        }
      }))
    } catch { setArtifacts([]) }
  }

  const deleteKI = async (id: string) => {
    if (!confirm('确定删除此知识条目？')) return
    await authFetch(`${API_BASE}/api/v1/ki/delete`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id })
    })
    setSelectedKI(null)
    onNavigateDetail?.(null)
    loadItems()
  }

  const formatSize = (b: number) => b < 1024 ? `${b}B` : `${(b / 1024).toFixed(1)}KB`

  // ═══ Level 2: KI Detail — artifacts rendered inline ═══
  if (selectedKI) {
    return (
      <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'slideInRight 0.25s cubic-bezier(0.4, 0, 0.2, 1)' }}>
        <div className="flex items-center gap-3 px-5 py-3 border-b border-white/[0.04] bg-white/[0.02] shrink-0">
          <button
            onClick={() => { setSelectedKI(null); onNavigateDetail?.(null) }}
            className="p-1.5 -ml-1 rounded-full hover:bg-white/10 text-gray-400 hover:text-white transition-colors flex items-center gap-1 group"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-hover:-translate-x-0.5 transition-transform">
              <path d="M15 18l-6-6 6-6"/>
            </svg>
            <span className="text-[11px] font-medium tracking-wide">返回</span>
          </button>
          <div className="h-4 w-px bg-white/10 mx-1"></div>
          <span className="text-sm font-semibold text-gray-200 flex-1 truncate">{selectedKI.title}</span>
          <button onClick={() => deleteKI(selectedKI.id)} className="px-3 py-1.5 rounded-lg text-[11px] font-medium text-red-400/70 hover:text-red-400 hover:bg-red-500/10 transition-colors">
            删除
          </button>
        </div>
        
        <div className="flex-1 overflow-y-auto px-3 sm:px-5 py-4 custom-scrollbar">
          <div className="max-w-4xl mx-auto space-y-5">

            {/* Meta */}
            <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-gray-500">
              <span>📅 更新: {new Date(selectedKI.updated_at).toLocaleDateString()}</span>
              <span>🔗 来源: {(selectedKI.sources || []).length} 个对话</span>
              <span className="font-mono text-gray-600">{selectedKI.id.slice(0, 8)}</span>
            </div>

            {/* Tags */}
            {(selectedKI.tags || []).length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {selectedKI.tags.map(t => (
                  <span key={t} className="px-2 py-0.5 rounded-full bg-blue-900/30 text-blue-400 text-[10px]">{t}</span>
                ))}
              </div>
            )}

            {/* Summary */}
            <div className="bg-white/[0.03] rounded-lg p-4">
              <div className="text-[10px] text-gray-600 mb-1.5 uppercase tracking-wider">摘要</div>
              <div className="text-sm text-gray-300 leading-relaxed">{selectedKI.summary}</div>
            </div>

            {/* All artifacts rendered inline */}
            {artifactContents.map((ac) => (
              <div key={ac.name} className="border border-white/[0.06] rounded-xl overflow-hidden">
                {/* File header */}
                <div className="flex items-center gap-2 px-4 py-2.5 bg-white/[0.03] border-b border-white/[0.06]">
                  <span className="text-sm">{ac.name.endsWith('.md') ? '📄' : ac.name.endsWith('.json') ? '📋' : '📎'}</span>
                  <span className="text-xs font-medium text-gray-300 flex-1">{ac.name}</span>
                  <span className="text-[10px] text-gray-600">
                    {formatSize(artifacts.find(a => a.name === ac.name)?.size || 0)}
                  </span>
                  <button
                    onClick={() => navigator.clipboard.writeText(ac.content)}
                    className="text-[10px] px-2 py-0.5 rounded bg-white/5 text-gray-500 hover:bg-white/10 hover:text-gray-300 transition-colors"
                  >复制</button>
                </div>

                {/* File content */}
                <div className="px-3 sm:px-5 py-3 sm:py-4">
                  {ac.loading ? (
                    <div className="text-gray-500 text-xs text-center py-4">加载中…</div>
                  ) : ac.name.endsWith('.md') ? (
                    <div className="hub-md-content text-sm text-gray-300 leading-relaxed">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{ac.content}</ReactMarkdown>
                    </div>
                  ) : ac.name.endsWith('.json') ? (
                    <pre className="text-xs text-emerald-300/90 font-mono whitespace-pre-wrap break-words leading-relaxed">
                      {(() => { try { return JSON.stringify(JSON.parse(ac.content), null, 2) } catch { return ac.content } })()}
                    </pre>
                  ) : (
                    <pre className="text-xs text-gray-400 font-mono whitespace-pre-wrap break-words leading-relaxed">
                      {ac.content}
                    </pre>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
        <MdStyles />
      </div>
    )
  }

  const filtered = items.filter(i =>
    !search || i.title.toLowerCase().includes(search.toLowerCase()) ||
    i.summary.toLowerCase().includes(search.toLowerCase()) ||
    (i.tags || []).some(t => t.toLowerCase().includes(search.toLowerCase()))
  )

  // ═══ Level 1: KI List ═══
  return (
    <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'fadeIn 0.2s ease-out' }}>
      <div className="shrink-0 bg-white/[0.02] border-b border-white/[0.04]">
        <div className="flex items-center justify-between px-5 py-2.5">
          <div className="flex items-center gap-2">
            <span className="text-[10px] uppercase tracking-widest text-gray-500 font-semibold">{items.length} Items</span>
          </div>
          <button onClick={loadItems} className="p-1.5 rounded-full hover:bg-white/10 text-gray-500 hover:text-gray-200 transition-colors group" title="刷新列表">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-active:rotate-180 transition-transform duration-300"><path d="M23 4v6h-6M1 20v-6h6"/><path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
          </button>
        </div>
        
        {/* Search */}
        <div className="px-4 pb-3">
          <div className="relative group">
            <div className="absolute inset-y-0 left-3 flex items-center pointer-events-none text-gray-500 group-focus-within:text-blue-400 transition-colors">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
            </div>
            <input
              value={search} onChange={e => setSearch(e.target.value)}
              placeholder="Search Knowledge..."
              className="w-full bg-black/40 border border-white/10 rounded-xl pl-9 pr-4 py-2 text-xs text-gray-200 placeholder-gray-600 outline-none focus:border-blue-500/50 focus:bg-black/60 transition-all shadow-inner"
            />
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-4 custom-scrollbar">
        {filtered.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-28 relative overflow-hidden">
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[180px] h-[180px] bg-amber-500/10 rounded-full blur-[50px] pointer-events-none" />
            <div className="relative z-10 w-16 h-16 rounded-3xl bg-white/[0.03] border border-white/[0.05] shadow-[0_8px_32px_rgba(0,0,0,0.3)] flex items-center justify-center mb-6 ring-1 ring-white/5">
              <span className="text-3xl opacity-80 drop-shadow-md">💡</span>
            </div>
            <span className="text-[13px] font-semibold tracking-wider text-gray-300 mb-1.5 z-10">{search ? 'No Matches' : 'Empty Knowledge'}</span>
            <span className="text-[11px] font-medium tracking-wide text-gray-500 z-10">{search ? '未能匹配任何库条目' : '知识库暂未收录内容'}</span>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {filtered.map(item => (
              <button key={item.id} onClick={() => openKI(item)}
                className="text-left p-4 rounded-xl border border-white/[0.04] bg-white/[0.02] hover:bg-white/[0.04] hover:border-white/10 transition-all group group-hover:shadow-[0_4px_20px_-5px_rgba(0,0,0,0.5)]">
                <div className="text-[13px] font-semibold text-gray-200 group-hover:text-blue-300 transition-colors mb-1.5 tracking-wide flex items-center gap-2">
                  <span className="shrink-0 text-[10px] px-1.5 py-0.5 rounded-full bg-blue-500/10 text-blue-400 font-mono border border-blue-500/20">{item.id.slice(0, 5)}</span>
                  <span className="truncate">{item.title}</span>
                </div>
                <div className="text-xs text-gray-500 line-clamp-2 mb-3 leading-relaxed">{item.summary}</div>
                <div className="flex items-center justify-between mt-auto">
                  <div className="flex flex-wrap gap-1.5">
                    {(item.tags || []).slice(0, 3).map(t => (
                      <span key={t} className="px-1.5 py-0.5 rounded-md bg-white/5 border border-white/5 text-[10px] text-gray-400 tracking-wide">{t}</span>
                    ))}
                  </div>
                  <span className="text-[10px] text-gray-600 font-mono tracking-wider">{new Date(item.updated_at).toLocaleDateString()}</span>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
      {/* Use shared Hub markdown styles */}
      <MdStyles />
    </div>
  )
}
