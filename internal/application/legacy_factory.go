package application

// legacyFacade is the internal bridge from ApplicationServices to the concrete
// compatibility shell retained for legacy callers.
func (s *ApplicationServices) legacyFacade() *AgentAPI {
	return &AgentAPI{
		chat:    s.chatService,
		runtime: s.runtime,
		session: s.session,
		admin:   s.admin,
		cost:    s.cost,
	}
}
