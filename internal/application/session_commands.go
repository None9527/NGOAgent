package application

import "github.com/ngoclaw/ngoagent/internal/domain/service"

func (s *SessionCommands) DeleteSession(id string) error {
	return s.chatEngine.DeleteSession(id)
}

func (s *SessionCommands) SetSessionTitle(id, title string) {
	s.sessMgr.SetTitle(id, title)
}

func (s *SessionCommands) ClearHistory() {
	if loop := service.ResolveActiveManagedLoop(s.loop, s.loopPool, s.sessMgr); loop != nil {
		loop.ClearHistory()
	}
}

func (s *SessionCommands) CompactContext() {
	if loop := service.ResolveActiveManagedLoop(s.loop, s.loopPool, s.sessMgr); loop != nil {
		loop.Compact()
	}
}
