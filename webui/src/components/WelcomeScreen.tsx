import React from 'react';

export const WelcomeScreen: React.FC = () => {
  return (
    <div className="flex-1 flex flex-col items-center justify-center p-4 relative overflow-hidden select-none">
      {/* Background gradient glow */}
      <div className="absolute inset-0 bg-gradient-to-br from-white/[0.02] via-transparent to-transparent pointer-events-none" />
      <div className="absolute top-1/3 left-1/2 -translate-x-1/2 w-[600px] h-[600px] bg-white/[0.015] rounded-full blur-[120px] pointer-events-none" />

      <div className="relative z-10 flex flex-col items-center max-w-xl">
        {/* Brand Identity */}
        <h1 className="text-6xl md:text-7xl font-black tracking-[-0.05em] text-white mb-6"
            style={{ textShadow: '0 0 80px rgba(255,255,255,0.08)' }}>
          NGOAgent
        </h1>

        {/* Tagline */}
        <p className="text-lg text-gray-400 font-medium mb-10 tracking-wide text-center leading-relaxed">
          Autonomous AI Agent · 自主智能体
        </p>

        {/* Feature pills */}
        <div className="flex flex-wrap justify-center gap-3 mb-10">
          {[
            '🧠 Multi-Model LLM',
            '🔧 30+ Built-in Tools',
            '🔌 MCP Protocol',
            '🛡️ Security Guard',
            '📡 SSE Reconnect',
            '🧩 Skill 插件生态',
          ].map((label) => (
            <span
              key={label}
              className="px-4 py-1.5 rounded-full text-xs font-medium text-gray-400 bg-white/[0.04] border border-white/[0.06] backdrop-blur-sm"
            >
              {label}
            </span>
          ))}
        </div>

        {/* Description */}
        <p className="text-sm text-gray-500 text-center leading-relaxed max-w-md">
          DDD 架构 · 10 态状态机 Agent Loop · 多会话并发 · Brain 工件系统 · 知识库语义检索 · Cron 定时任务
        </p>
      </div>
    </div>
  );
};
