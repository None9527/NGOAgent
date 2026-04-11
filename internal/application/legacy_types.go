package application

// AgentAPI is a compatibility facade over the explicit application services.
// New code should prefer ApplicationServices and its capability services directly.
type AgentAPI struct {
	chat    *ChatService
	runtime *RuntimeService
	session *SessionService
	admin   *AdminService
	cost    *CostService
}
