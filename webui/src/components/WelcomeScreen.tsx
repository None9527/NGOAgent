import React from 'react';

const kernelLanes = [
  ['Loop', '10-state agent loop with reconnectable stream control'],
  ['Graph', 'node runtime, snapshots, and resumable execution'],
  ['Memory', 'brain artifacts, diary hooks, semantic retrieval'],
  ['Guard', 'permission gate, security mode, tool audit trail'],
]

export const WelcomeScreen: React.FC = () => {
  return (
    <div className="welcome-screen flex-1 flex flex-col justify-center p-4 md:p-8 relative overflow-hidden select-none">
      <div className="relative z-10 mx-auto flex w-full max-w-4xl flex-col gap-8">
        <div className="max-w-2xl">
          <div className="mb-3 inline-flex items-center gap-2 rounded-lg border border-cyan-300/20 bg-cyan-300/10 px-3 py-1 text-[11px] font-semibold uppercase text-cyan-100">
            <span className="h-1.5 w-1.5 rounded-sm bg-cyan-200" />
            Kernel console ready
          </div>
          <h1 className="mb-4 text-4xl font-black text-white md:text-6xl">
            NGOAgent
          </h1>
          <p className="max-w-xl text-base leading-7 text-gray-300 md:text-lg">
            Send one instruction. The runtime will plan, execute tools, request approval when needed, and stream every kernel transition back into this console.
          </p>
        </div>

        <div className="grid gap-3 md:grid-cols-4">
          {kernelLanes.map(([label, body]) => (
            <div key={label} className="kernel-lane rounded-lg border border-white/[0.08] bg-white/[0.035] p-4">
              <div className="mb-3 flex items-center justify-between">
                <span className="text-[11px] font-semibold uppercase text-white/45">{label}</span>
                <span className="h-1.5 w-6 rounded-sm bg-white/20" />
              </div>
              <p className="text-sm leading-6 text-gray-300">{body}</p>
            </div>
          ))}
        </div>

        <div className="grid gap-3 text-sm text-gray-400 md:grid-cols-[1fr_auto] md:items-end">
          <p className="leading-6">
            Use Plan for reviewable work, Agentic for direct execution, and Evo when the kernel should iterate on its own repair loop.
          </p>
          <div className="rounded-lg border border-amber-300/20 bg-amber-300/10 px-3 py-2 text-[11px] font-semibold uppercase text-amber-100">
            Waiting for input
          </div>
        </div>
      </div>
    </div>
  );
};
