/**
 * Settings UI Primitives — reusable form components for the Settings page.
 * Extracted from SettingsPage.tsx for single-responsibility.
 */

import React, { useState, useEffect } from 'react'

/* ════════════════ Layout ════════════════ */

export function FieldGroup({ title, description, children }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <h3 className="text-[15px] font-medium text-zinc-100">{title}</h3>
      {description && <p className="text-[13px] text-zinc-500 mb-4">{description}</p>}
      <div className="flex flex-col gap-4">
        {children}
      </div>
    </div>
  )
}

export function ReadOnlyRow({ label, val }: { label: string; val: any }) {
  return (
    <div className="flex justify-between items-center py-1">
      <span className="text-[13px] text-zinc-400 font-medium">{label}</span>
      <span className="text-[12px] text-zinc-300 font-mono truncate max-w-[60%]">{String(val || '—')}</span>
    </div>
  )
}

/* ════════════════ Blur-to-Save Input ════════════════ */

export function InputAutoSave({ label, val, configKey, type, onSave, status, placeholder }: {
  label: string, val: any, configKey: string, type?: string, 
  onSave: (k: string, v: any) => Promise<void>, status?: 'saving' | 'saved' | 'error',
  placeholder?: string
}) {
  const [localVal, setLocalVal] = useState(String(val ?? ''))
  const [isFocused, setIsFocused] = useState(false)
  const isDirty = localVal !== String(val ?? '')

  useEffect(() => {
    if (!isFocused) setLocalVal(String(val ?? ''))
  }, [val, isFocused])

  const commit = () => {
    if (isDirty) {
      const payload = type === 'number' ? Number(localVal) : localVal
      onSave(configKey, payload)
    }
  }

  return (
    <div className="flex flex-col gap-1.5 relative">
      <label className="text-[13px] font-medium text-zinc-300">{label}</label>
      <div className="relative group">
        <input
          type={type === 'number' ? 'number' : 'text'}
          value={localVal}
          onChange={e => setLocalVal(e.target.value)}
          onFocus={() => setIsFocused(true)}
          onBlur={() => { setIsFocused(false); commit() }}
          onKeyDown={e => { if (e.key === 'Enter' && !e.nativeEvent.isComposing) { e.currentTarget.blur() } }}
          placeholder={placeholder}
          className="w-full bg-[#18181b] border border-[#27272a] rounded-lg px-3 py-2 text-[13.5px] text-zinc-200 outline-none
            focus:border-blue-500/50 focus:ring-[3px] focus:ring-blue-500/10 transition-all font-mono placeholder:text-zinc-600"
        />
        <div className="absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none flex items-center gap-1.5 opacity-0 transition-opacity duration-300"
          style={{ opacity: status ? 1 : (isDirty && !isFocused) ? 0.5 : 0 }}>
          {status === 'saving' && <span className="text-[10px] uppercase text-blue-400 font-semibold tracking-wider animate-pulse">Saving</span>}
          {status === 'saved' && <span className="text-[10px] uppercase text-emerald-400 font-semibold tracking-wider">Saved</span>}
          {status === 'error' && <span className="text-[10px] uppercase text-red-400 font-semibold tracking-wider">Error</span>}
        </div>
      </div>
    </div>
  )
}

/* ════════════════ Toggle Switch ════════════════ */

export function NativeSwitch({ label, value, configKey, onSave }: { label: string, value: boolean, configKey: string, onSave: (k: string, v: any) => Promise<void> }) {
  const [localState, setLocalState] = useState(value)
  const toggle = () => {
    const next = !localState
    setLocalState(next)
    onSave(configKey, next)
  }

  useEffect(() => { setLocalState(value) }, [value])

  return (
    <div className="flex items-center justify-between py-2 cursor-pointer group" onClick={toggle}>
      <span className="text-[13px] font-medium text-zinc-300 group-hover:text-white transition-colors">{label}</span>
      <div className={`w-9 h-5 rounded-full relative transition-colors duration-200 ease-in-out border border-[#27272a] shadow-inner ${localState ? 'bg-zinc-200' : 'bg-[#18181b]'}`}>
        <div className={`absolute top-[2px] w-3.5 h-3.5 rounded-full transition-transform duration-200 ease-in-out shadow-sm ${localState ? 'bg-[#09090b] translate-x-[18px]' : 'bg-zinc-500 translate-x-[3px]'}`} />
      </div>
    </div>
  )
}

/* ════════════════ Tag Editor ════════════════ */

export function TagEditor({ label, items, configKey, onSave, status }: { label: string, items: string[], configKey: string, onSave: (k: string, v: any) => Promise<void>, status?: string }) {
  const [val, setVal] = useState(items.join(', '))
  const [isFocused, setIsFocused] = useState(false)
  const isDirty = val !== items.join(', ')

  useEffect(() => { if (!isFocused) setVal(items.join(', ')) }, [items, isFocused])

  const commit = () => {
    if (isDirty) {
      const arr = val.split(',').map(s => s.trim()).filter(Boolean)
      onSave(configKey, arr)
    }
  }

  return (
    <div className="flex flex-col gap-1.5 relative">
      <label className="text-[13px] font-medium text-zinc-300">{label}</label>
      <input
        type="text"
        value={val}
        onChange={e => setVal(e.target.value)}
        onFocus={() => setIsFocused(true)}
        onBlur={() => { setIsFocused(false); commit() }}
        onKeyDown={e => { if (e.key === 'Enter' && !e.nativeEvent.isComposing) e.currentTarget.blur() }}
        placeholder="Comma separated values..."
        className="w-full bg-[#18181b] border border-[#27272a] rounded-lg px-3 py-2 text-[13px] text-zinc-300 outline-none
            focus:border-blue-500/50 focus:ring-[3px] focus:ring-blue-500/10 transition-all font-mono"
      />
      {status === 'saved' && <span className="absolute right-3 top-[34px] text-[10px] uppercase text-emerald-400 font-semibold tracking-wider">Saved</span>}
    </div>
  )
}
