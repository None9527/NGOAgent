package application

import "github.com/ngoclaw/ngoagent/internal/capability"

// Compile-time interface checks — ensures application services satisfy the
// shared capability and compatibility contracts.
var _ LegacyAPI = (*AgentAPI)(nil)
var _ LegacyChatAPI = (*ChatService)(nil)
var _ LegacyRuntimeAPI = (*RuntimeService)(nil)
var _ LegacySessionAPI = (*SessionService)(nil)
var _ LegacyAdminAPI = (*AdminService)(nil)
var _ LegacyCostAPI = (*CostService)(nil)

var _ capability.Chat = (*ChatService)(nil)
var _ capability.ChatControl = (*ChatService)(nil)
var _ capability.Runtime = (*RuntimeService)(nil)
var _ capability.Session = (*SessionService)(nil)
var _ capability.Admin = (*AdminService)(nil)
var _ capability.Cost = (*CostService)(nil)
