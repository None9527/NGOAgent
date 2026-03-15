import React from 'react';

export interface WelcomeScreenProps {
  onSuggestionClick: (text: string) => void;
}

const SUGGESTIONS = [
  { title: 'Help me debug', text: 'Help me debug a React application error', icon: '🐛' },
  { title: 'Analyze codebase', text: 'Analyze this project structure and explain its architecture', icon: '🏗️' },
  { title: 'Write a test', text: 'Write a unit test for a utility function', icon: '🧪' },
  { title: 'Refactor code', text: 'Help me refactor a complex React component', icon: '✨' },
];

export const WelcomeScreen: React.FC<WelcomeScreenProps> = ({ onSuggestionClick }) => {
  return (
    <div className="flex-1 flex flex-col items-center justify-center p-4 relative overflow-hidden">
      {/* Background gradient glow - softer and more ambient */}
      <div className="absolute inset-0 bg-gradient-to-br from-white/[0.02] via-transparent to-transparent pointer-events-none" />
      <div className="absolute top-1/4 left-1/2 -translate-x-1/2 w-[500px] h-[500px] bg-white/[0.01] rounded-full blur-[100px] pointer-events-none" />
      
      <div className="relative z-10 flex flex-col items-center">
        <div className="w-16 h-16 mb-6 rounded-2xl bg-white/[0.03] backdrop-blur-2xl flex items-center justify-center text-white/80 text-3xl shadow-[0_8px_32px_rgba(0,0,0,0.5)] border border-white/10 ring-1 ring-white/5 transition-transform duration-500 hover:scale-[1.05] hover:shadow-[0_16px_48px_rgba(255,255,255,0.05)] cursor-default">
          ⌘
        </div>
        <h1 className="text-4xl font-bold tracking-tight text-white mb-3">
          NGOAgent
        </h1>
        <p className="text-lg text-gray-400 font-medium mb-12">
          Your Intelligent Copilot
        </p>
        
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 max-w-2xl w-full">
          {SUGGESTIONS.map((suggestion, idx) => (
            <button
              key={idx}
              onClick={() => onSuggestionClick(suggestion.text)}
              className="group flex flex-col text-left p-6 rounded-[24px] bg-white/[0.02] hover:bg-white/[0.04] border border-white/[0.04] hover:border-white/10 transition-all duration-500 hover:-translate-y-1 active:scale-[0.98] shadow-sm hover:shadow-[0_20px_40px_-15px_rgba(0,0,0,0.7),0_0_30px_rgba(255,255,255,0.03)] backdrop-blur-xl"
            >
              <div className="text-2xl mb-4 opacity-80 group-hover:opacity-100 transition-opacity duration-300">{suggestion.icon}</div>
              <div className="font-semibold text-gray-200 text-sm mb-2 transition-colors">
                {suggestion.title}
              </div>
              <div className="text-[13px] text-gray-500 leading-relaxed">
                {suggestion.text}
              </div>
            </button>
          ))}
        </div>
        
        <div className="mt-12 flex items-center gap-3 text-xs text-gray-500 font-medium tracking-wide">
          <span className="px-2.5 py-1 rounded-md bg-white/5 border border-white/10 shadow-sm backdrop-blur-sm">⌘K</span>
          <span>Open Command Palette</span>
        </div>
      </div>
    </div>
  );
};
