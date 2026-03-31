import React, { useState, useRef, useEffect } from 'react';

export interface SidebarProps {
  isOpen: boolean;
  onToggle: () => void;
  sessions: { id: string; title: string; channel: string; created_at: string; updated_at: string }[];
  currentSessionId: string;
  onSelectSession: (id: string) => void
  onNewSession: () => void
  onDeleteSession: (id: string) => void
  onRenameSession: (id: string, newTitle: string) => void
}

// ── Date group helper ────────────────────────────────────────
function getDateLabel(dateStr: string): string {
  if (!dateStr) return '今天'  // new sessions not yet in DB
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return '更早'

  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)
  const sessionDay = new Date(d.getFullYear(), d.getMonth(), d.getDate())

  if (sessionDay.getTime() === today.getTime()) return '今天'
  if (sessionDay.getTime() === yesterday.getTime()) return '昨天'
  // Specific date: e.g. "3月10日"
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

function groupSessions<T extends { created_at: string }>(sessions: T[]): { label: string; items: T[] }[] {
  const map = new Map<string, T[]>()
  const order: string[] = []

  for (const s of sessions) {
    const label = getDateLabel(s.created_at)
    if (!map.has(label)) {
      map.set(label, [])
      order.push(label)
    }
    map.get(label)!.push(s)
  }

  return order.map(label => ({ label, items: map.get(label)! }))
}

// ── Icons ────────────────────────────────────────────────────
const PencilIcon = () => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
    className="w-3.5 h-3.5">
    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
  </svg>
)

const TrashIcon = () => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
    className="w-3.5 h-3.5">
    <polyline points="3 6 5 6 21 6" />
    <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
    <path d="M10 11v6M14 11v6" />
    <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
  </svg>
)

const CheckIcon = () => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
    className="w-3.5 h-3.5">
    <polyline points="20 6 9 17 4 12" />
  </svg>
)

const XIcon = () => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
    className="w-3.5 h-3.5">
    <line x1="18" y1="6" x2="6" y2="18" />
    <line x1="6" y1="6" x2="18" y2="18" />
  </svg>
)

// ── SessionItem ──────────────────────────────────────────────
interface SessionItemProps {
  session: { id: string; title: string; channel: string; created_at: string }
  isActive: boolean
  onSelect: () => void
  onDelete: () => void
  onRename: (newTitle: string) => void
}

const SessionItem = React.memo<SessionItemProps>(({ session, isActive, onSelect, onDelete, onRename }) => {
  const [hovered, setHovered] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState(session.title || '无标题对话')
  const [titleFlash, setTitleFlash] = useState(false)
  const prevTitleRef = useRef(session.title)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => { if (editing) inputRef.current?.select() }, [editing])

  // Detect title changes → trigger flash animation
  useEffect(() => {
    if (session.title && session.title !== prevTitleRef.current) {
      setTitleFlash(true)
      const timer = setTimeout(() => setTitleFlash(false), 1200)
      prevTitleRef.current = session.title
      return () => clearTimeout(timer)
    }
  }, [session.title])

  const startEdit = (e: React.MouseEvent) => {
    e.stopPropagation()
    setEditValue(session.title || '无标题对话')
    setEditing(true)
  }

  const confirmEdit = () => {
    const t = editValue.trim()
    if (t && t !== session.title) onRename(t)
    setEditing(false)
  }

  const cancelEdit = () => setEditing(false)

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') confirmEdit()
    if (e.key === 'Escape') cancelEdit()
  }

  const bg = isActive ? 'rgba(255,255,255,0.1)' : hovered ? 'rgba(255,255,255,0.05)' : 'transparent'
  const fg = isActive ? '#ffffff' : '#a3a3a3'

  return (
    <div
      className="group relative flex items-center rounded-lg transition-all duration-200"
      style={{ backgroundColor: bg }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {editing ? (
        <div className="flex items-center w-full gap-1 px-2 py-1.5">
          <input
            ref={inputRef}
            value={editValue}
            onChange={e => setEditValue(e.target.value)}
            onKeyDown={handleKeyDown}
            className="flex-1 text-sm bg-[#1a1a1a] border border-[#4a4a4a] rounded px-2 py-1 outline-none text-white min-w-0"
            onClick={e => e.stopPropagation()}
          />
          <button onClick={confirmEdit} title="确认"
            className="p-1 rounded hover:bg-green-700/40 text-green-400 shrink-0">
            <CheckIcon />
          </button>
          <button onClick={cancelEdit} title="取消"
            className="p-1 rounded hover:bg-red-700/40 text-red-400 shrink-0">
            <XIcon />
          </button>
        </div>
      ) : (
        <>
          <button
            className="flex-1 text-left px-3 py-2.5 text-sm truncate min-w-0 rounded-lg"
            style={{
              color: fg,
              transition: 'color 0.3s ease',
              ...(titleFlash ? {
                animation: 'sidebar-title-flash 1.2s ease-out',
              } : {}),
            }}
            onClick={onSelect}
          >
            {session.title || '无标题对话'}
          </button>
          {(hovered || isActive) && (
            <div className={`flex items-center gap-0.5 pr-1.5 shrink-0 ${!isActive && !hovered ? 'hidden' : ''}`}>
              <button onClick={startEdit} title="重命名"
                className="p-1.5 rounded transition-colors text-gray-500 hover:text-gray-200 hover:bg-white/10 active:bg-white/20">
                <PencilIcon />
              </button>
              <button onClick={(e) => { e.stopPropagation(); onDelete() }} title="删除"
                className="p-1.5 rounded transition-colors text-gray-500 hover:text-red-400 hover:bg-red-900/30 active:bg-red-900/50">
                <TrashIcon />
              </button>
            </div>
          )}
        </>
      )}
    </div>
  )
}
)

// ── Sidebar ─────────────────────────────────────────────────
export const Sidebar: React.FC<SidebarProps> = ({
  isOpen, onToggle, sessions, currentSessionId,
  onSelectSession,
  onNewSession,
  onDeleteSession,
  onRenameSession,
}: SidebarProps) => {
  const [searchQuery, setSearchQuery] = useState('')

  // cron sessions shown in dedicated management page — hidden here
  const visible = [...sessions.filter(s => s.channel !== 'cron')]
    .sort((a, b) => {
      // sort by updated_at: most recently active first
      const ta = a.updated_at ? new Date(a.updated_at).getTime() : (a.created_at ? new Date(a.created_at).getTime() : Date.now())
      const tb = b.updated_at ? new Date(b.updated_at).getTime() : (b.created_at ? new Date(b.created_at).getTime() : Date.now())
      return tb - ta
    })

  // Filter by search query
  const filtered = searchQuery.trim()
    ? visible.filter(s => (s.title || '无标题对话').toLowerCase().includes(searchQuery.trim().toLowerCase()))
    : visible

  // Group by updated_at so sessions active today show under 今天
  const groups = groupSessions(filtered.map(s => ({ ...s, created_at: s.updated_at || s.created_at })))

  return (
    <>
      {/* Mobile backdrop */}
      {isOpen && (
        <div className="fixed inset-0 bg-black/50 z-20 md:hidden" onClick={onToggle} />
      )}

      <div className={`fixed md:relative flex flex-col h-full glass-panel border-l-0 border-t-0 border-b-0 w-[240px] md:w-[280px] shrink-0 z-30
          transition-transform duration-300 ease-in-out
          ${isOpen ? 'translate-x-0' : '-translate-x-full md:hidden'}`}>

        {/* Top: new session */}
        <div className="p-3 flex items-center justify-between">
          <button onClick={onNewSession}
            className="flex-1 flex items-center gap-2 px-3 py-2 rounded-lg hover:bg-[#2f2f2f] transition-colors text-sm"
            style={{ color: '#d4d4d4' }}>
            <svg stroke="currentColor" fill="none" strokeWidth="2" viewBox="0 0 24 24"
              strokeLinecap="round" strokeLinejoin="round" className="w-5 h-5" xmlns="http://www.w3.org/2000/svg">
              <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
            </svg>
            新建对话
          </button>
          <button onClick={onToggle}
            className="p-2 ml-2 rounded-lg hover:bg-[#2f2f2f] text-gray-400 md:hidden">
            <svg stroke="currentColor" fill="none" strokeWidth="2" viewBox="0 0 24 24"
              strokeLinecap="round" strokeLinejoin="round" className="w-5 h-5" xmlns="http://www.w3.org/2000/svg">
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2" /><line x1="9" y1="3" x2="9" y2="21" />
            </svg>
          </button>
        </div>

        {/* Search bar */}
        <div className="px-3 pb-2">
          <div className="relative">
            <svg className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-500 pointer-events-none"
              stroke="currentColor" fill="none" strokeWidth="2" viewBox="0 0 24 24"
              strokeLinecap="round" strokeLinejoin="round" xmlns="http://www.w3.org/2000/svg">
              <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            <input
              type="text"
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              placeholder="搜索对话..."
              className="w-full glass-input rounded-lg pl-8 pr-7 py-1.5 text-xs text-gray-300 outline-none placeholder:text-gray-600"
            />
            {searchQuery && (
              <button
                onClick={() => setSearchQuery('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors text-xs leading-none"
              >✕</button>
            )}
          </div>
        </div>

        {/* Session list with date groups */}
        <div className="flex-1 overflow-y-auto px-3 pb-3">
          {filtered.length === 0 && (
            <div className="text-xs text-gray-500 px-2">{searchQuery ? '无匹配结果' : '暂无对话'}</div>
          )}
          {groups.map(group => (
            <div key={group.label} className="mb-2">
              <div className="text-xs font-semibold px-2 py-1.5 sticky top-0 z-10 bg-transparent backdrop-blur-md flex items-center gap-2"
                style={{ color: '#8E8E93' }}>
                <div className="w-1.5 h-1.5 rounded-full bg-white/20" />
                {group.label}
              </div>
              <div className="space-y-0.5">
                {group.items.map(session => (
                  <SessionItem
                    key={session.id}
                    session={session}
                    isActive={session.id === currentSessionId}
                    onSelect={() => onSelectSession(session.id)}
                    onDelete={() => onDeleteSession(session.id)}
                    onRename={t => onRenameSession(session.id, t)}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </>
  )
}
