import { authFetch } from '../chat/api'
import { useState, useEffect, useCallback } from 'react'
import { FieldGroup, ReadOnlyRow, InputAutoSave, NativeSwitch, TagEditor } from './settings/SettingsPrimitives'

const API = ''

interface ProviderDef {
  name: string
  type: string
  base_url: string
  api_key: string
  models: string[]
}

interface SettingsPageProps {
  isOpen: boolean
  onClose: () => void
}

type TabId = 'general' | 'integrations' | 'knowledge' | 'advanced'

const TABS: { id: TabId; label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'integrations', label: 'Integrations' },
  { id: 'knowledge', label: 'Knowledge' },
  { id: 'advanced', label: 'Advanced' },
]

export function SettingsPage({ isOpen, onClose }: SettingsPageProps) {
  const [config, setConfig] = useState<Record<string, any>>({})
  const [loading, setLoading] = useState(true)
  const [activeTab, setActiveTab] = useState<TabId>('general')
  
  // Ephemeral toast state overlayed on inputs
  const [saveStatus, setSaveStatus] = useState<Record<string, 'saving' | 'saved' | 'error'>>({})

  const loadConfig = useCallback(async () => {
    setLoading(true)
    try {
      const res = await authFetch(`${API}/v1/config`)
      if (res.ok) setConfig(await res.json())
    } catch (err) {
      console.error('Failed to load config', err)
    }
    setLoading(false)
  }, [])

  useEffect(() => {
    if (isOpen) loadConfig()
  }, [isOpen, loadConfig])

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  const setVal = async (key: string, value: any) => {
    setSaveStatus(prev => ({ ...prev, [key]: 'saving' }))
    try {
      const res = await authFetch(`${API}/api/v1/config`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key, value })
      })
      if (res.ok) {
        setSaveStatus(prev => ({ ...prev, [key]: 'saved' }))
        setTimeout(() => setSaveStatus(prev => {
          const next = { ...prev }
          delete next[key]
          return next
        }), 2000)
        await loadConfig() // Silent reload to stay in sync
      } else {
        setSaveStatus(prev => ({ ...prev, [key]: 'error' }))
      }
    } catch {
      setSaveStatus(prev => ({ ...prev, [key]: 'error' }))
    }
  }

  if (!isOpen) return null

  const agent = config.agent || {}
  const security = config.security || {}
  const search = config.search || {}
  const embedding = config.embedding || {}
  const cron = config.cron || {}
  const forge = config.forge || {}
  const server = config.server || {}
  const storage = config.storage || {}
  const providers: ProviderDef[] = config.llm?.providers || []
  const mcpServers: any[] = config.mcp?.servers || []

  return (
    <div className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm flex items-center justify-center p-4">
      <div 
        className="w-full max-w-[1000px] w-[75vw] h-[80vh] flex flex-col bg-[#09090b] border border-[#27272a] rounded-xl shadow-2xl overflow-hidden text-gray-200 font-sans"
        onClick={e => e.stopPropagation()}
      >
        {/* Header & Tabs */}
        <div className="flex flex-col border-b border-[#27272a] shrink-0 bg-[#09090b] px-6 pt-5">
          <div className="flex items-center justify-between mb-6">
            <h2 className="text-xl font-semibold tracking-tight text-white">Settings</h2>
            <button onClick={onClose} className="p-1.5 rounded-md hover:bg-[#27272a] transition-colors text-gray-400 hover:text-white">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M18 6L6 18M6 6l12 12"/></svg>
            </button>
          </div>
          <div className="flex gap-6 relative">
            {TABS.map(tab => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`pb-3 text-sm font-medium transition-colors relative ${activeTab === tab.id ? 'text-white' : 'text-zinc-500 hover:text-zinc-300'}`}
              >
                {tab.label}
                {activeTab === tab.id && (
                  <div className="absolute bottom-0 left-0 w-full h-[2px] bg-white rounded-t-full" />
                )}
              </button>
            ))}
          </div>
        </div>

        {/* Content Area */}
        <div className="flex-1 overflow-y-auto w-full">
          {loading ? (
            <div className="flex h-full items-center justify-center text-sm text-zinc-500">Loading config...</div>
          ) : (
            <div className="max-w-2xl mx-auto py-10 px-8 flex flex-col gap-10">
              
              {/* ═══ TAB: GENERAL ═══ */}
              <div className={activeTab === 'general' ? 'block' : 'hidden'}>
                <FieldGroup title="Agent Context" description="Core environmental execution constraints for the copilot.">
                  <InputAutoSave 
                    label="Workspace Directory"
                    configKey="agent.workspace"
                    val={agent.workspace}
                    onSave={setVal}
                    status={saveStatus['agent.workspace']}
                    placeholder="~/.ngoagent/workspace"
                  />
                  <InputAutoSave 
                    label="Default Model"
                    configKey="agent.default_model"
                    val={agent.default_model}
                    onSave={setVal}
                    status={saveStatus['agent.default_model']}
                    placeholder="E.g. qwen3.5-plus (Leave empty to use first available)"
                  />
                  <InputAutoSave 
                    label="Max Steps"
                    configKey="agent.max_steps"
                    val={agent.max_steps}
                    type="number"
                    onSave={setVal}
                    status={saveStatus['agent.max_steps']}
                  />
                </FieldGroup>
              </div>

              {/* ═══ TAB: INTEGRATIONS ═══ */}
              <div className={activeTab === 'integrations' ? 'block space-y-10' : 'hidden'}>
                <FieldGroup title="LLM Providers" description="Configured inference endpoints. NGOAgent auto-rotates through them if defaults are not met.">
                  <ProviderManager 
                    providers={providers} 
                    onReload={loadConfig} 
                    API={API} 
                  />
                </FieldGroup>
                
                <FieldGroup title="MCP Servers" description="Model Context Protocol servers providing external tools.">
                  <MCPManager 
                    servers={mcpServers} 
                    onReload={loadConfig} 
                    API={API} 
                  />
                </FieldGroup>

                <FieldGroup title="Metasearch" description="SearXNG endpoint for agent web queries.">
                  <InputAutoSave 
                    label="Endpoint URL"
                    configKey="search.endpoint"
                    val={search.endpoint}
                    onSave={setVal}
                    status={saveStatus['search.endpoint']}
                    placeholder="http://localhost:8080"
                  />
                </FieldGroup>
              </div>

              {/* ═══ TAB: KNOWLEDGE ═══ */}
              <div className={activeTab === 'knowledge' ? 'block space-y-10' : 'hidden'}>
                <FieldGroup title="Vector Embedding" description="Configuration for semantic knowledge chunking and retrieval.">
                  <InputAutoSave 
                    label="Provider"
                    configKey="embedding.provider"
                    val={embedding.provider}
                    onSave={setVal}
                    status={saveStatus['embedding.provider']}
                    placeholder="dashscope, openai, etc. (Leave empty to disable)"
                  />
                  <InputAutoSave 
                    label="Base URL"
                    configKey="embedding.base_url"
                    val={embedding.base_url}
                    onSave={setVal}
                    status={saveStatus['embedding.base_url']}
                  />
                  <InputAutoSave 
                    label="Model"
                    configKey="embedding.model"
                    val={embedding.model}
                    onSave={setVal}
                    status={saveStatus['embedding.model']}
                  />
                  <div className="grid grid-cols-2 gap-4">
                    <InputAutoSave label="Dimensions" configKey="embedding.dimensions" val={embedding.dimensions} type="number" onSave={setVal} status={saveStatus['embedding.dimensions']} />
                    <InputAutoSave label="Similarity Threshold" configKey="embedding.similarity_threshold" val={embedding.similarity_threshold} type="number" onSave={setVal} status={saveStatus['embedding.similarity_threshold']} />
                    <InputAutoSave label="Min KIs for Embedding" configKey="embedding.min_ki_for_embedding" val={embedding.min_ki_for_embedding} type="number" onSave={setVal} status={saveStatus['embedding.min_ki_for_embedding']} />
                    <InputAutoSave label="Top-K Query" configKey="embedding.top_k" val={embedding.top_k} type="number" onSave={setVal} status={saveStatus['embedding.top_k']} />
                  </div>
                </FieldGroup>
              </div>

              {/* ═══ TAB: ADVANCED ═══ */}
              <div className={activeTab === 'advanced' ? 'block space-y-10' : 'hidden'}>
                <FieldGroup title="Security Enclave" description="Command-level boundaries for shell execution tools. Mode (Ask/Allow) is set globally in the chat interface.">
                  <div className="flex flex-col gap-4 pt-1">
                    <TagEditor label="Block List (High-risk limits)" items={security.block_list || []} configKey="security.block_list" onSave={setVal} status={saveStatus['security.block_list']} />
                    <TagEditor label="Safe Commands (Pre-approved)" items={security.safe_commands || []} configKey="security.safe_commands" onSave={setVal} status={saveStatus['security.safe_commands']} />
                  </div>
                </FieldGroup>

                <FieldGroup title="Engine & Sandbox" description="Cron jobs and Forge capability testing.">
                  <NativeSwitch label="Background Cron Scheduler" configKey="cron.enabled" value={!!cron.enabled} onSave={setVal} />
                  <div className="h-px bg-[#27272a] w-full my-4" />
                  <InputAutoSave label="Forge Sandbox Directory" configKey="forge.sandbox_dir" val={forge.sandbox_dir} onSave={setVal} status={saveStatus['forge.sandbox_dir']} />
                  <div className="grid grid-cols-2 gap-4">
                    <InputAutoSave label="Max Retries" configKey="forge.max_retries" val={forge.max_retries} type="number" onSave={setVal} status={saveStatus['forge.max_retries']} />
                    <InputAutoSave label="History Limit" configKey="forge.history_limit" val={forge.history_limit} type="number" onSave={setVal} status={saveStatus['forge.history_limit']} />
                  </div>
                  <div className="mt-4">
                    <NativeSwitch label="Auto-forge on Install" configKey="forge.auto_forge_on_install" value={!!forge.auto_forge_on_install} onSave={setVal} />
                  </div>
                </FieldGroup>

                <FieldGroup title="System Telemetry" description="Read-only system directories and networking info.">
                  <div className="space-y-3 bg-[#121214] border border-[#27272a] rounded-xl p-4">
                    <ReadOnlyRow label="Database Path" val={storage.db_path} />
                    <ReadOnlyRow label="Brain Domain" val={storage.brain_dir} />
                    <ReadOnlyRow label="API Bindings" val={`HTTP: ${server.http_port}`} />
                    <ReadOnlyRow label="Auth Token" val={server.auth_token ? `${server.auth_token.slice(0, 8)}${'*'.repeat(8)}` : '—'} />
                  </div>
                </FieldGroup>
              </div>

            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/* ════════════════ Intricate Custom Managers ════════════════ */

function ProviderManager({ providers, onReload, API }: { providers: ProviderDef[], onReload: () => void, API: string }) {
  const [isAdding, setIsAdding] = useState(false)
  
  const removeProvider = async (name: string) => {
    if (!confirm(`Remove provider "${name}"?`)) return
    await authFetch(`${API}/api/v1/config/provider/remove`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    })
    onReload()
  }

  return (
    <div className="flex flex-col gap-3">
      {providers.map(p => (
        <div key={p.name} className="flex flex-col p-4 rounded-xl border border-[#27272a] bg-[#121214] group transition-colors hover:border-[#3f3f46]">
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2.5">
              <span className="text-[14px] font-medium text-white">{p.name}</span>
              <span className="text-[10px] px-1.5 py-0.5 rounded border border-[#27272a] bg-[#18181b] text-zinc-400 uppercase tracking-widest">{p.type}</span>
            </div>
            <button onClick={() => removeProvider(p.name)} className="text-zinc-500 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-all">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
            </button>
          </div>
          <div className="text-[12px] text-zinc-500 font-mono truncate mb-1">{p.base_url}</div>
          {p.api_key && <div className="text-[11px] text-zinc-600 font-mono truncate mb-3">Key: {p.api_key}</div>}
          <div className="flex flex-wrap gap-1.5 mt-auto">
            {p.models.map(m => (
              <span key={m} className="text-[10px] px-2 py-0.5 rounded-full bg-[#18181b] border border-[#27272a] text-zinc-400">{m}</span>
            ))}
          </div>
        </div>
      ))}

      {isAdding ? (
        <InlineProviderForm API={API} onSuccess={() => { setIsAdding(false); onReload() }} onCancel={() => setIsAdding(false)} />
      ) : (
        <button onClick={() => setIsAdding(true)} className="w-full py-3 rounded-xl border border-dashed border-[#27272a] text-[13px] font-medium text-zinc-400 hover:text-white hover:border-[#3f3f46] hover:bg-[#121214] transition-all flex items-center justify-center gap-2">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 5v14M5 12h14"/></svg>
          Add Provider
        </button>
      )}
    </div>
  )
}

function InlineProviderForm({ API, onSuccess, onCancel }: { API: string, onSuccess: () => void, onCancel: () => void }) {
  const [form, setForm] = useState({ name: '', type: 'openai', base_url: '', api_key: '', models: '' })
  const [submitting, setSubmitting] = useState(false)

  const submit = async () => {
    if (!form.name || !form.base_url) return
    setSubmitting(true)
    const payload = {
      ...form,
      models: form.models.split(',').map(m => m.trim()).filter(Boolean)
    }
    const res = await authFetch(`${API}/api/v1/config/provider/add`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })
    setSubmitting(false)
    if (res.ok) onSuccess()
    else alert('Failed to add provider')
  }

  return (
    <div className="p-4 rounded-xl border border-blue-500/30 bg-[#121214] flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-3">
        <input placeholder="Name (e.g. dashscope)" value={form.name} onChange={e => setForm(p => ({...p, name: e.target.value}))} className="bg-[#18181b] border border-[#27272a] rounded-lg px-3 py-2 text-[13px] outline-none focus:border-blue-500/50" />
        <input placeholder="Type (e.g. openai)" value={form.type} onChange={e => setForm(p => ({...p, type: e.target.value}))} className="bg-[#18181b] border border-[#27272a] rounded-lg px-3 py-2 text-[13px] outline-none focus:border-blue-500/50" />
      </div>
      <input placeholder="Base URL" value={form.base_url} onChange={e => setForm(p => ({...p, base_url: e.target.value}))} className="bg-[#18181b] border border-[#27272a] rounded-lg px-3 py-2 text-[13px] outline-none focus:border-blue-500/50 font-mono" />
      <input placeholder="API Key (${ENV_VAR} or strict key)" value={form.api_key} onChange={e => setForm(p => ({...p, api_key: e.target.value}))} className="bg-[#18181b] border border-[#27272a] rounded-lg px-3 py-2 text-[13px] outline-none focus:border-blue-500/50 font-mono" />
      <input placeholder="Models (comma separated, e.g. gpt-4, gpt-3.5-turbo)" value={form.models} onChange={e => setForm(p => ({...p, models: e.target.value}))} className="bg-[#18181b] border border-[#27272a] rounded-lg px-3 py-2 text-[13px] outline-none focus:border-blue-500/50 font-mono" />
      <div className="flex gap-2 justify-end mt-2">
        <button onClick={onCancel} className="px-4 py-1.5 text-[12px] font-medium text-zinc-400 hover:text-white transition-colors">Cancel</button>
        <button onClick={submit} disabled={submitting || !form.name || !form.base_url} className="px-4 py-1.5 text-[12px] font-medium bg-white text-black hover:bg-zinc-200 disabled:bg-zinc-700 disabled:text-zinc-500 rounded-md transition-colors">
          Add Provider
        </button>
      </div>
    </div>
  )
}

function MCPManager({ servers, onReload, API }: { servers: any[], onReload: () => void, API: string }) {
  const removeMCP = async (name: string) => {
    if (!confirm(`Remove MCP Server "${name}"?`)) return
    await authFetch(`${API}/api/v1/config/mcp/remove`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    })
    onReload()
  }

  return (
    <div className="flex flex-col gap-2">
      {servers.length === 0 && <div className="text-[13px] text-zinc-500 italic px-1 py-2">No servers connected.</div>}
      {servers.map(s => (
        <div key={s.name} className="flex items-center justify-between p-3 rounded-lg border border-[#27272a] bg-[#121214] group">
          <div className="flex flex-col">
            <span className="text-[13px] font-medium text-zinc-200">{s.name}</span>
            <span className="text-[11px] text-zinc-500 font-mono mt-0.5">{s.command} {(s.args || []).join(' ')}</span>
          </div>
          <button onClick={() => removeMCP(s.name)} className="p-1.5 text-zinc-500 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-colors">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
          </button>
        </div>
      ))}
    </div>
  )
}
