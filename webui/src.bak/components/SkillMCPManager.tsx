import { authFetch } from '../chat/api'
import { useState, useEffect, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { MdStyles } from './shared/mdStyles'

const API_BASE = ''

interface SkillInfo {
  name: string
  description: string
  path: string
  type: string
  enabled: boolean
  forge_status: string
}

interface MCPServer {
  name: string
  running: boolean
}

interface MCPTool {
  name: string
  description: string
  server: string
}

interface SkillMCPManagerProps {
  onNavigateDetail?: (id: string | null) => void
}

type Tab = 'skills' | 'mcp'

export function SkillMCPManager({ onNavigateDetail }: SkillMCPManagerProps) {
  const [tab, setTab] = useState<Tab>('skills')

  // Skills state
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [skillsLoading, setSkillsLoading] = useState(false)
  const [selectedSkill, setSelectedSkill] = useState<string | null>(null)
  const [skillContent, setSkillContent] = useState<string | null>(null)
  const [contentLoading, setContentLoading] = useState(false)

  // MCP state
  const [servers, setServers] = useState<MCPServer[]>([])
  const [tools, setTools] = useState<MCPTool[]>([])
  const [mcpLoading, setMcpLoading] = useState(false)

  const loadSkills = useCallback(async () => {
    setSkillsLoading(true)
    try {
      const res = await authFetch(`${API_BASE}/api/v1/skills/list`)
      if (!res.ok) return
      const data = await res.json()
      setSkills(data.skills || [])
    } catch { setSkills([]) }
    finally { setSkillsLoading(false) }
  }, [])

  const loadMCP = useCallback(async () => {
    setMcpLoading(true)
    try {
      const [srvRes, toolsRes] = await Promise.all([
        authFetch(`${API_BASE}/api/v1/mcp/servers`),
        authFetch(`${API_BASE}/api/v1/mcp/tools`)
      ])
      if (srvRes.ok) {
        const d = await srvRes.json()
        setServers(d.servers || [])
      }
      if (toolsRes.ok) {
        const d = await toolsRes.json()
        setTools(d.tools || [])
      }
    } catch { setServers([]); setTools([]) }
    finally { setMcpLoading(false) }
  }, [])

  useEffect(() => {
    loadSkills()
    loadMCP()
  }, [loadSkills, loadMCP])

  // Sync Hub detail state (onNavigateDetail excluded from deps — same infinite loop fix as CronManager)
  useEffect(() => {
    const isDetail = tab === 'skills' && selectedSkill !== null
    onNavigateDetail?.(isDetail ? 'drilling' : null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, selectedSkill])

  // Refresh MCP when switching to tab
  useEffect(() => {
    if (tab === 'mcp') loadMCP()
  }, [tab, loadMCP])

  const readSkill = async (name: string) => {
    setSelectedSkill(name)
    setContentLoading(true)
    try {
      const res = await authFetch(`${API_BASE}/api/v1/skills/read?name=${encodeURIComponent(name)}`)
      if (!res.ok) throw new Error()
      const data = await res.json()
      setSkillContent(data.content || '')
    } catch { setSkillContent('无法读取 SKILL.md') }
    finally { setContentLoading(false) }
  }

  const refreshSkills = async () => {
    await authFetch(`${API_BASE}/api/v1/skills/refresh`, { method: 'POST' })
    loadSkills()
  }

  const deleteSkill = async (name: string) => {
    if (!confirm(`确认删除 Skill「${name}」？此操作不可恢复。`)) return
    const res = await authFetch(`${API_BASE}/api/v1/skills/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
    if (res.ok) {
      if (selectedSkill === name) { setSelectedSkill(null); setSkillContent(null) }
      loadSkills()
    }
  }

  const forgeColor: Record<string, string> = {
    draft: 'text-gray-500 bg-gray-500/10 border-gray-500/20',
    forging: 'text-amber-400 bg-amber-500/10 border-amber-500/20',
    forged: 'text-emerald-400 bg-emerald-500/10 border-emerald-500/20',
    degraded: 'text-red-400 bg-red-500/10 border-red-500/20',
    reforging: 'text-blue-400 bg-blue-500/10 border-blue-500/20',
  }

  const typeIcon: Record<string, string> = {
    workflow: '📋', executable: '⚡', hybrid: '🔀',
  }

  // ─── Detail View: Skill Content (SKILL.md) ───
  if (tab === 'skills' && selectedSkill) {
    return (
      <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'slideInRight 0.25s cubic-bezier(0.4, 0, 0.2, 1)' }}>
        <div className="flex items-center gap-3 px-5 py-3 border-b border-white/[0.04] bg-white/[0.02] shrink-0">
          <button onClick={() => { setSelectedSkill(null); setSkillContent(null) }} className="p-1.5 -ml-1 rounded-full hover:bg-white/10 text-gray-400 hover:text-white transition-colors flex items-center gap-1 group">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-hover:-translate-x-0.5 transition-transform"><path d="M15 18l-6-6 6-6"/></svg>
            <span className="text-[11px] font-medium tracking-wide">返回</span>
          </button>
          <div className="h-4 w-px bg-white/10 mx-1"></div>
          <div className="flex flex-col min-w-0 flex-1">
            <span className="text-[10px] text-gray-400 tracking-wide uppercase">SKILL.md</span>
            <span className="text-sm font-medium text-gray-200 truncate tracking-wide leading-tight">{selectedSkill}</span>
          </div>
          <button onClick={() => deleteSkill(selectedSkill)} className="px-3 py-1.5 rounded-lg text-[11px] font-medium text-red-400/70 hover:text-red-400 hover:bg-red-500/10 transition-colors">
            删除
          </button>
        </div>
        
        <div className="flex-1 overflow-y-auto px-3 sm:px-5 py-4 sm:py-5 custom-scrollbar">
          {contentLoading ? (
            <div className="text-center py-12 text-gray-500 text-sm">加载中…</div>
          ) : (
            <div className="hub-md-content text-sm text-gray-300 leading-relaxed max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{skillContent || ''}</ReactMarkdown>
            </div>
          )}
        </div>
        <MdStyles />
      </div>
    )
  }

  // ─── Root View ───
  return (
    <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'fadeIn 0.2s ease-out' }}>
      <div className="shrink-0 bg-white/[0.02] border-b border-white/[0.04]">
        {/* Hub Top Tab Switcher */}
        <div className="flex items-center justify-between px-3 sm:px-5 py-2.5 border-b border-white/[0.02]">
          <div className="flex bg-black/40 p-1 rounded-lg ring-1 ring-white/[0.05]">
            <button onClick={() => setTab('skills')}
              className={`px-4 py-1.5 text-[11px] font-medium rounded-md transition-all duration-300 ${
                tab === 'skills' ? 'bg-white/10 text-white shadow-sm ring-1 ring-black/20' : 'text-gray-500 hover:text-gray-300'
              }`}>
              组件扩展 ({skills.length})
            </button>
            <button onClick={() => setTab('mcp')}
              className={`px-4 py-1.5 text-[11px] font-medium rounded-md transition-all duration-300 ${
                tab === 'mcp' ? 'bg-white/10 text-white shadow-sm ring-1 ring-black/20' : 'text-gray-500 hover:text-gray-300'
              }`}>
              MCP 桥接
            </button>
          </div>
          <div className="flex items-center gap-1.5">
            {tab === 'skills' && (
              <button onClick={refreshSkills} className="px-2.5 py-1.5 rounded-md text-[10px] font-medium bg-blue-500/10 text-blue-400 hover:bg-blue-500/20 transition-colors flex items-center gap-1">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M23 4v6h-6M1 20v-6h6"/><path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
                <span>重新发现</span>
              </button>
            )}
            {tab === 'mcp' && (
              <button onClick={loadMCP} className="p-1.5 rounded-full hover:bg-white/10 text-gray-500 hover:text-gray-200 transition-colors group" title="刷新 MCP">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-active:rotate-180 transition-transform duration-300"><path d="M23 4v6h-6M1 20v-6h6"/><path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
              </button>
            )}
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-4 custom-scrollbar">
        {tab === 'skills' && (
          skillsLoading ? (
            <div className="text-center py-16 text-gray-500 text-sm">加载中…</div>
          ) : skills.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-24 text-gray-600">
              <span className="text-4xl mb-4 drop-shadow-lg opacity-40">🧩</span>
              <span className="text-sm font-medium tracking-wide">暂无扩展组件</span>
              <span className="text-[10px] text-gray-500 mt-2 font-mono">~/.ngoagent/skills/</span>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {skills.map(sk => (
                <button key={sk.name} onClick={() => readSkill(sk.name)}
                  className="flex flex-col text-left p-4 rounded-xl border border-white/[0.04] bg-white/[0.02] hover:bg-white/[0.04] hover:border-white/10 transition-all group group-hover:shadow-[0_4px_20px_-5px_rgba(0,0,0,0.5)]">
                  <div className="flex items-center justify-between mb-2 w-full">
                    <div className="flex items-center gap-2 pr-2 min-w-0">
                      <span className="text-sm shrink-0 drop-shadow-sm">{typeIcon[sk.type] || '📦'}</span>
                      <span className="text-[14px] font-semibold text-gray-200 group-hover:text-blue-300 transition-colors truncate tracking-wide">{sk.name}</span>
                    </div>
                    <span className={`shrink-0 text-[9px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider border ${forgeColor[sk.forge_status] || forgeColor.draft}`}>
                      {sk.forge_status}
                    </span>
                  </div>
                  
                  <div className="text-[11px] text-gray-500 line-clamp-2 leading-relaxed mb-3 pr-2">
                    {sk.description || '无描述'}
                  </div>

                  <div className="flex items-center justify-between mt-auto w-full">
                    <span className="text-[9px] text-gray-600 font-mono tracking-wider truncate bg-black/20 px-1.5 py-0.5 rounded border border-white/[0.02] max-w-[200px]">{sk.path.replace(/.+\/skills\//, '')}</span>
                    <div className="text-gray-500 group-hover:text-blue-400 transition-colors shrink-0">
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M9 18l6-6-6-6"/></svg>
                    </div>
                  </div>
                </button>
              ))}
            </div>
          )
        )}

        {tab === 'mcp' && (
          mcpLoading ? (
            <div className="text-center py-16 text-gray-500 text-sm">加载中…</div>
          ) : servers.length === 0 && tools.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-24 text-gray-600">
              <span className="text-4xl mb-4 drop-shadow-lg opacity-40">🔌</span>
              <span className="text-sm font-medium tracking-wide">未接入 MCP 服务器</span>
              <span className="text-[10px] text-gray-500 mt-2 font-mono">config.yaml → mcp.servers</span>
            </div>
          ) : (
            <div className="flex flex-col gap-6">
              {/* Servers Section */}
              <section>
                <div className="flex items-center gap-2 mb-3">
                  <span className="text-[10px] uppercase tracking-widest text-gray-500 font-semibold px-1">连接状态 ({servers.length})</span>
                  <div className="h-px bg-white/5 flex-1" />
                </div>
                <div className="flex flex-col gap-2">
                  {servers.map(srv => (
                    <div key={srv.name} className="flex items-center justify-between px-4 py-3 bg-white/[0.02] border border-white/[0.04] rounded-xl hover:bg-white/[0.03] transition-colors">
                      <div className="flex flex-col">
                        <span className="text-[13px] font-semibold text-gray-200 tracking-wide">{srv.name}</span>
                        <span className="text-[10px] text-gray-500 mt-0.5 font-mono">mcp.server.{srv.name.toLowerCase()}</span>
                      </div>
                      <div className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full border ${srv.running ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-400' : 'bg-red-500/10 border-red-500/20 text-red-400'}`}>
                        <span className={`w-1.5 h-1.5 rounded-full ${srv.running ? 'bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.6)]' : 'bg-red-400'}`} />
                        <span className="text-[9px] font-bold uppercase tracking-wider">{srv.running ? 'Connected' : 'Offline'}</span>
                      </div>
                    </div>
                  ))}
                </div>
              </section>

              {/* Tools Section */}
              {tools.length > 0 && (
                <section>
                  <div className="flex items-center gap-2 mb-3">
                    <span className="text-[10px] uppercase tracking-widest text-gray-500 font-semibold px-1">可用工具 ({tools.length})</span>
                    <div className="h-px bg-white/5 flex-1" />
                  </div>
                  <div className="flex flex-col gap-2.5">
                    {tools.map(t => (
                      <div key={`${t.server}-${t.name}`} className="flex flex-col p-3.5 bg-white/[0.015] border border-white/[0.03] rounded-xl hover:border-white/10 transition-colors">
                        <div className="flex items-center justify-between mb-2">
                          <span className="text-[13px] font-mono font-semibold text-blue-300/90">{t.name}</span>
                          <span className="text-[9px] px-2 py-0.5 rounded-md bg-purple-500/10 text-purple-300/80 border border-purple-500/20 shadow-sm">{t.server}</span>
                        </div>
                        <p className="text-[11px] text-gray-400 leading-relaxed max-w-full break-words">
                          {t.description || '无描述信息'}
                        </p>
                      </div>
                    ))}
                  </div>
                </section>
              )}
            </div>
          )
        )}
      </div>
    </div>
  )
}


