import { authFetch } from '../chat/api'
import { useState, useEffect, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { MdStyles } from './shared/mdStyles'

const API_BASE = ''

interface CronJob {
  name: string
  schedule: string
  prompt: string
  enabled: boolean
  run_count: number
  fail_count: number
  last_run: string | null
  created_at: string
  updated_at: string
}

interface LogEntry {
  file: string
  time: string
  size: number
  success: boolean
}

interface CronManagerProps {
  onNavigateDetail?: (id: string | null) => void
}

type Tab = 'jobs' | 'logs'

export function CronManager({ onNavigateDetail }: CronManagerProps) {
  const [tab, setTab] = useState<Tab>('jobs')

  // ─── Jobs state ───
  const [jobs, setJobs] = useState<CronJob[]>([])
  const [loading, setLoading] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState('')
  const [newSchedule, setNewSchedule] = useState('5m')
  const [newPrompt, setNewPrompt] = useState('')
  const [creating, setCreating] = useState(false)

  // ─── Logs state ───
  const [selectedJob, setSelectedJob] = useState<string | null>(null)
  const [logEntries, setLogEntries] = useState<LogEntry[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [logContent, setLogContent] = useState<string | null>(null)
  const [logFile, setLogFile] = useState<string | null>(null)
  const [logContentLoading, setLogContentLoading] = useState(false)

  const loadJobs = useCallback(async () => {
    setLoading(true)
    try {
      const res = await authFetch(`${API_BASE}/api/v1/cron/list`)
      if (!res.ok) return
      const data = await res.json()
      setJobs(data.jobs || [])
    } catch { setJobs([]) }
    finally { setLoading(false) }
  }, [])

  useEffect(() => {
    loadJobs()
  }, [loadJobs])

  // Sync Hub detail state (onNavigateDetail is excluded from deps to prevent infinite loop —
  // it's an inline arrow in the parent that creates a new ref every render)
  useEffect(() => {
    const isDetail = (tab === 'logs' && (selectedJob || logFile))
    onNavigateDetail?.(isDetail ? 'drilling' : null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, selectedJob, logFile])

  const loadLogs = useCallback(async (jobName: string) => {
    setSelectedJob(jobName)
    setLogsLoading(true)
    setLogContent(null)
    setLogFile(null)
    try {
      const res = await authFetch(`${API_BASE}/api/v1/cron/logs?name=${encodeURIComponent(jobName)}`)
      if (!res.ok) throw new Error('fail')
      const data = await res.json()
      setLogEntries(data.logs || [])
    } catch { setLogEntries([]) }
    finally { setLogsLoading(false) }
  }, [])

  const readLog = async (jobName: string, file: string) => {
    setLogFile(file)
    setLogContentLoading(true)
    try {
      const res = await authFetch(`${API_BASE}/api/v1/cron/log/read?name=${encodeURIComponent(jobName)}&file=${encodeURIComponent(file)}`)
      if (!res.ok) throw new Error('fail')
      const data = await res.json()
      setLogContent(data.content || '')
    } catch { setLogContent('无法读取日志') }
    finally { setLogContentLoading(false) }
  }

  const createJob = async () => {
    if (!newName || !newSchedule || !newPrompt) return
    setCreating(true)
    try {
      const res = await authFetch(`${API_BASE}/api/v1/cron/create`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName, schedule: newSchedule, prompt: newPrompt })
      })
      if (!res.ok) { const t = await res.text(); alert(t); return }
      setNewName(''); setNewSchedule('5m'); setNewPrompt('')
      setShowCreate(false)
      loadJobs()
    } catch (e: any) { alert(e.message) }
    finally { setCreating(false) }
  }

  const deleteJob = async (name: string) => {
    if (!confirm(`删除定时任务 "${name}" 及其所有日志？`)) return
    await authFetch(`${API_BASE}/api/v1/cron/delete`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    })
    loadJobs()
  }

  const toggleJob = async (name: string, enabled: boolean) => {
    const endpoint = enabled ? 'disable' : 'enable'
    await authFetch(`${API_BASE}/api/v1/cron/${endpoint}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    })
    loadJobs()
  }

  const runNow = async (name: string) => {
    await authFetch(`${API_BASE}/api/v1/cron/run`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    })
    loadJobs()
  }

  const formatTime = (t: string | null) => {
    if (!t) return '—'
    try {
      const d = new Date(t)
      return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch { return t }
  }

  // ─── Detail View: Log Content ───
  if (tab === 'logs' && logFile && selectedJob) {
    return (
      <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'slideInRight 0.25s cubic-bezier(0.4, 0, 0.2, 1)' }}>
        <div className="flex items-center gap-3 px-5 py-3 border-b border-white/[0.04] bg-white/[0.02] shrink-0">
          <button onClick={() => setLogFile(null)} className="p-1.5 -ml-1 rounded-full hover:bg-white/10 text-gray-400 hover:text-white transition-colors flex items-center gap-1 group">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-hover:-translate-x-0.5 transition-transform"><path d="M15 18l-6-6 6-6" /></svg>
            <span className="text-[11px] font-medium tracking-wide">返回</span>
          </button>
          <div className="h-4 w-px bg-white/10 mx-1"></div>
          <div className="flex flex-col min-w-0 flex-1">
            <span className="text-[10px] text-gray-400 font-mono tracking-wide">{selectedJob}</span>
            <span className="text-sm font-medium text-gray-200 truncate tracking-wide leading-tight">{logFile.replace('.md', '')}</span>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-5 custom-scrollbar">
          {logContentLoading ? (
            <div className="text-center py-12 text-gray-500 text-sm">加载中…</div>
          ) : (
            <div className="hub-md-content text-sm text-gray-300 leading-relaxed max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{logContent || ''}</ReactMarkdown>
            </div>
          )}
        </div>
        <MdStyles />
      </div>
    )
  }

  // ─── Sub-List View: Log Entries ───
  if (tab === 'logs' && selectedJob) {
    return (
      <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'slideInRight 0.2s cubic-bezier(0.4, 0, 0.2, 1)' }}>
        <div className="flex items-center gap-3 px-5 py-3 border-b border-white/[0.04] bg-white/[0.02] shrink-0">
          <button onClick={() => setSelectedJob(null)} className="p-1.5 -ml-1 rounded-full hover:bg-white/10 text-gray-400 hover:text-white transition-colors flex items-center gap-1 group">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-hover:-translate-x-0.5 transition-transform"><path d="M15 18l-6-6 6-6" /></svg>
            <span className="text-[11px] font-medium tracking-wide">返回</span>
          </button>
          <div className="h-4 w-px bg-white/10 mx-1"></div>
          <span className="text-sm font-semibold text-gray-200 truncate flex-1 tracking-wide">{selectedJob} <span className="text-gray-500 font-normal">执行记录</span></span>
          <span className="text-[10px] uppercase tracking-widest text-gray-500 font-semibold bg-white/5 px-2 py-0.5 rounded-md border border-white/5">{logEntries.length} 次</span>
        </div>

        <div className="flex-1 overflow-y-auto p-4 custom-scrollbar">
          {logsLoading ? (
            <div className="text-center py-12 text-gray-500 text-sm">加载中…</div>
          ) : logEntries.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-28 relative overflow-hidden">
              <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[180px] h-[180px] bg-emerald-500/10 rounded-full blur-[50px] pointer-events-none" />
              <div className="relative z-10 w-16 h-16 rounded-3xl bg-white/[0.03] border border-white/[0.05] shadow-[0_8px_32px_rgba(0,0,0,0.3)] flex items-center justify-center mb-6 ring-1 ring-white/5">
                <span className="text-3xl opacity-80 drop-shadow-md">📝</span>
              </div>
              <span className="text-[13px] font-semibold tracking-wider text-gray-300 mb-1.5 z-10">No Executions</span>
              <span className="text-[11px] font-medium tracking-wide text-gray-500 z-10">该任务尚未产生任何日志记录</span>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              {logEntries.map(entry => (
                <button key={entry.file} onClick={() => readLog(selectedJob, entry.file)}
                  className="flex items-center justify-between p-3.5 rounded-xl border border-white/[0.04] bg-white/[0.02] hover:bg-white/[0.04] hover:border-white/10 transition-all group group-hover:shadow-sm">
                  <div className="flex items-center gap-3">
                    <div className={`w-8 h-8 rounded-full flex items-center justify-center border ${entry.success ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-400' : 'bg-red-500/10 border-red-500/20 text-red-400'}`}>
                      {entry.success ? (
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><path d="M20 6L9 17l-5-5" /></svg>
                      ) : (
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><path d="M18 6L6 18M6 6l12 12" /></svg>
                      )}
                    </div>
                    <div className="flex flex-col items-start">
                      <span className="text-[13px] font-medium text-gray-200 group-hover:text-white transition-colors tracking-wide">{entry.time.replace('T', ' ')}</span>
                      <span className="text-[10px] font-mono text-gray-500 mt-0.5">{(entry.size / 1024).toFixed(1)} KB</span>
                    </div>
                  </div>
                  <div className="text-gray-500 group-hover:text-blue-400 transition-colors opacity-0 group-hover:opacity-100">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M9 18l6-6-6-6" /></svg>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    )
  }

  // ─── Root View: Main Tabs (Jobs / Logs Selector) ───
  return (
    <div className="w-full h-full flex flex-col bg-transparent relative" style={{ animation: 'fadeIn 0.2s ease-out' }}>
      <div className="shrink-0 bg-white/[0.02] border-b border-white/[0.04]">
        {/* Hub Top Tab Switcher */}
        <div className="flex items-center justify-between px-5 py-2.5 border-b border-white/[0.02]">
          <div className="flex bg-black/40 p-1 rounded-lg ring-1 ring-white/[0.05]">
            {(['jobs', 'logs'] as const).map(t => (
              <button key={t} onClick={() => setTab(t)}
                className={`px-4 py-1.5 text-[11px] font-medium rounded-md transition-all duration-300 ${tab === t ? 'bg-white/10 text-white shadow-sm ring-1 ring-black/20' : 'text-gray-500 hover:text-gray-300'
                  }`}>
                {t === 'jobs' ? '任务列表' : '执行日志'}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-1.5">
            {tab === 'jobs' && (
              <button onClick={() => setShowCreate(!showCreate)} className={`p-1.5 rounded-full transition-colors ${showCreate ? 'bg-blue-500/20 text-blue-400' : 'hover:bg-white/10 text-gray-400'}`} title="新建任务">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><path d="M12 5v14M5 12h14" /></svg>
              </button>
            )}
            <button onClick={loadJobs} className="p-1.5 rounded-full hover:bg-white/10 text-gray-500 hover:text-gray-200 transition-colors group" title="刷新列表">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-active:rotate-180 transition-transform duration-300"><path d="M23 4v6h-6M1 20v-6h6" /><path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15" /></svg>
            </button>
          </div>
        </div>

        {tab === 'jobs' && showCreate && (
          <div className="px-5 py-4 bg-black/20 border-b border-white/[0.04]" style={{ animation: 'slideDown 0.2s ease-out' }}>
            <div className="flex flex-col gap-3">
              <div className="flex gap-3">
                <div className="flex-1">
                  <label className="block text-[10px] text-gray-500 mb-1.5 uppercase tracking-wider font-semibold">识别名</label>
                  <input value={newName} onChange={e => setNewName(e.target.value)} placeholder="名称 (英文,无空格)"
                    className="w-full bg-white/[0.04] border border-white/10 rounded-lg px-3 py-2 text-[13px] text-gray-200 placeholder-gray-600 outline-none focus:border-blue-500/50 focus:bg-white/[0.06] transition-all shadow-inner" />
                </div>
                <div className="w-24 shrink-0">
                  <label className="block text-[10px] text-gray-500 mb-1.5 uppercase tracking-wider font-semibold">间隔</label>
                  <input value={newSchedule} onChange={e => setNewSchedule(e.target.value)} placeholder="5m"
                    className="w-full bg-white/[0.04] border border-white/10 rounded-lg px-3 py-2 text-[13px] text-gray-200 placeholder-gray-600 font-mono outline-none focus:border-blue-500/50 focus:bg-white/[0.06] transition-all shadow-inner" />
                </div>
              </div>
              <div>
                <label className="block text-[10px] text-gray-500 mb-1.5 uppercase tracking-wider font-semibold">Prompt 提示词</label>
                <textarea value={newPrompt} onChange={e => setNewPrompt(e.target.value)} placeholder="检查系统状态并生成报告…" rows={2}
                  className="w-full bg-white/[0.04] border border-white/10 rounded-lg px-3 py-2 text-[13px] text-gray-200 placeholder-gray-600 outline-none focus:border-blue-500/50 focus:bg-white/[0.06] transition-all shadow-inner custom-scrollbar resize-none" />
              </div>
              <button onClick={createJob} disabled={creating || !newName || !newPrompt}
                className="w-full mt-1 px-4 py-2.5 rounded-lg text-sm font-medium bg-emerald-600/80 text-white hover:bg-emerald-500 disabled:opacity-40 disabled:cursor-not-allowed transition-colors shadow-lg shadow-emerald-900/20 active:scale-[0.98]">
                {creating ? '创建中…' : '立即创建'}
              </button>
            </div>
          </div>
        )}
      </div>

      <div className="flex-1 overflow-y-auto p-4 custom-scrollbar">
        {loading ? (
          <div className="text-center py-16 text-gray-500 text-sm">加载中…</div>
        ) : jobs.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-28 relative overflow-hidden">
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[180px] h-[180px] bg-purple-500/10 rounded-full blur-[50px] pointer-events-none" />
            <div className="relative z-10 w-16 h-16 rounded-3xl bg-white/[0.03] border border-white/[0.05] shadow-[0_8px_32px_rgba(0,0,0,0.3)] flex items-center justify-center mb-6 ring-1 ring-white/5">
              <span className="text-3xl opacity-80 drop-shadow-md">⏰</span>
            </div>
            <span className="text-[13px] font-semibold tracking-wider text-gray-300 mb-1.5 z-10">No Cron Jobs</span>
            <span className="text-[11px] font-medium tracking-wide text-gray-500 z-10">点击顶部 "+" 号创建新任务</span>
          </div>
        ) : tab === 'jobs' ? (
          /* Jobs List */
          <div className="flex flex-col gap-3">
            {jobs.map(job => (
              <div key={job.name} className="flex flex-col p-4 rounded-xl border border-white/[0.04] bg-white/[0.02] hover:bg-white/[0.04] hover:border-white/10 transition-all group group-hover:shadow-[0_4px_20px_-5px_rgba(0,0,0,0.5)]">
                <div className="flex items-start justify-between mb-2.5">
                  <div className="flex flex-col min-w-0 pr-3">
                    <span className="text-[14px] font-semibold text-gray-200 truncate tracking-wide">{job.name}</span>
                    <span className="text-[10px] text-gray-500 mt-0.5">上次运行: {formatTime(job.last_run)}</span>
                  </div>
                  {/* Status Toggle Bubble */}
                  <button onClick={() => toggleJob(job.name, job.enabled)}
                    className={`shrink-0 flex items-center gap-1.5 px-2 py-1 rounded-full border transition-all shadow-sm ${job.enabled ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/20' : 'bg-gray-800/50 border-gray-700 text-gray-500 hover:bg-gray-700'}`}>
                    <span className={`w-1.5 h-1.5 rounded-full ${job.enabled ? 'bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.8)]' : 'bg-gray-500'}`} />
                    <span className="text-[10px] font-bold tracking-wider">{job.enabled ? 'ON' : 'OFF'}</span>
                  </button>
                </div>

                <div className="text-[11px] text-gray-400/90 line-clamp-2 leading-relaxed mb-3 bg-black/20 p-2.5 rounded-lg border border-white/[0.02]">
                  {job.prompt}
                </div>

                <div className="flex items-center justify-between mt-auto">
                  <div className="flex gap-2">
                    <span className="px-2 py-0.5 rounded-md bg-white/5 border border-white/5 text-[10px] text-gray-400 font-mono tracking-wide">
                      <span className="text-gray-600 mr-1">T</span>{job.schedule}
                    </span>
                    <span className="px-2 py-0.5 rounded-md bg-white/5 border border-white/5 text-[10px] text-gray-400 font-mono tracking-wide" title="总执行次数">
                      <span className="text-gray-600 mr-1">R</span>{job.run_count}
                    </span>
                    {job.fail_count > 0 && (
                      <span className="px-2 py-0.5 rounded-md bg-red-500/10 border border-red-500/20 text-[10px] text-red-400 font-mono tracking-wide" title="失败次数">
                        <span className="text-red-900 mr-1">F</span>{job.fail_count}
                      </span>
                    )}
                  </div>
                  <div className="flex gap-1">
                    <button onClick={() => runNow(job.name)} className="p-1.5 rounded bg-blue-500/10 text-blue-400 hover:bg-blue-500/20 transition-colors" title="立即运行">
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z" /></svg>
                    </button>
                    <button onClick={() => deleteJob(job.name)} className="p-1.5 rounded bg-red-500/10 text-red-400 hover:bg-red-500/20 transition-colors" title="删除任务">
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" /></svg>
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          /* Logs Tab (Job Selector) */
          <div className="flex flex-col gap-2">
            <div className="text-[10px] text-gray-500 uppercase tracking-widest px-1 mb-1">选择任务以查看日志</div>
            {jobs.map(j => (
              <button key={j.name} onClick={() => loadLogs(j.name)}
                className="flex items-center justify-between p-3.5 rounded-xl border border-white/[0.04] bg-white/[0.02] hover:bg-white/[0.04] hover:border-white/10 transition-all group group-hover:shadow-sm">
                <div className="flex flex-col items-start min-w-0 pr-3">
                  <span className="text-[13px] font-semibold text-gray-200 group-hover:text-blue-300 transition-colors truncate tracking-wide">{j.name}</span>
                  <span className="text-[10px] text-gray-500 mt-0.5 font-mono bg-white/5 px-1.5 py-0.5 rounded">{j.schedule}</span>
                </div>
                <div className="text-gray-500 group-hover:text-blue-400 transition-colors opacity-0 group-hover:opacity-100 shrink-0">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M9 18l6-6-6-6" /></svg>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}


