/**
 * Shared markdown rendering styles for BrainPanel and KIManager.
 * Single source of truth — eliminates ~50 lines of duplicate CSS.
 */

import React from 'react'

export const MdStyles: React.FC = () => (
  <style>{`
    .hub-md-content h1 { font-size: 1.2em; font-weight: 700; margin: 0.8em 0 0.4em; color: #e5e5e5; border-bottom: 1px solid rgba(255,255,255,0.08); padding-bottom: 0.3em; }
    .hub-md-content h2 { font-size: 1.08em; font-weight: 600; margin: 0.7em 0 0.3em; color: #d4d4d4; }
    .hub-md-content h3 { font-size: 0.95em; font-weight: 600; margin: 0.5em 0 0.2em; color: #a3a3a3; }
    .hub-md-content p { margin: 0.4em 0; line-height: 1.7; }
    .hub-md-content ul, .hub-md-content ol { padding-left: 1.4em; margin: 0.3em 0; }
    .hub-md-content li { margin: 0.15em 0; line-height: 1.65; }
    .hub-md-content li::marker { color: #525252; }
    .hub-md-content code { background: rgba(255,255,255,0.08); padding: 0.12em 0.38em; border-radius: 3px; font-size: 0.85em; color: #e879f9; }
    .hub-md-content pre { background: rgba(0,0,0,0.35); padding: 0.75em; border-radius: 6px; overflow-x: auto; margin: 0.5em 0; }
    .hub-md-content pre code { background: none; padding: 0; color: #d4d4d4; }
    .hub-md-content blockquote { border-left: 3px solid rgba(96,165,250,0.4); padding-left: 0.8em; margin: 0.5em 0; color: #a3a3a3; }
    .hub-md-content table { border-collapse: collapse; width: 100%; margin: 0.5em 0; font-size: 0.88em; display: block; overflow-x: auto; -webkit-overflow-scrolling: touch; }
    .hub-md-content th, .hub-md-content td { border: 1px solid rgba(255,255,255,0.1); padding: 0.35em 0.6em; text-align: left; }
    .hub-md-content th { background: rgba(255,255,255,0.06); font-weight: 600; color: #d4d4d4; }
    .hub-md-content a { color: #60a5fa; text-decoration: none; }
    .hub-md-content a:hover { text-decoration: underline; }
    .hub-md-content hr { border: none; border-top: 1px solid rgba(255,255,255,0.08); margin: 0.8em 0; }
    .hub-md-content strong { color: #f5f5f5; }
    .hub-md-content em { color: #a3a3a3; }
    .hub-md-content input[type="checkbox"] { appearance: none; -webkit-appearance: none; width: 14px; height: 14px; margin-right: 0.4em; border: 1.5px solid #525252; border-radius: 3px; vertical-align: middle; position: relative; top: -1px; cursor: default; flex-shrink: 0; }
    .hub-md-content input[type="checkbox"]:checked { background: #22c55e; border-color: #22c55e; }
    .hub-md-content input[type="checkbox"]:checked::before { content: ""; display: block; width: 4px; height: 8px; border: solid #fff; border-width: 0 2px 2px 0; transform: rotate(45deg); position: absolute; top: 1px; left: 4px; }
  `}</style>
)
