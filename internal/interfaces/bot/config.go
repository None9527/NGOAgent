package bot

// Config holds Telegram bot configuration loaded from the main config.
type Config struct {
	// Token is the Telegram Bot API token from @BotFather.
	Token string `yaml:"token"`

	// GRPCAddr is the NGOAgent gRPC server address (default: localhost:50051).
	GRPCAddr string `yaml:"grpc_addr"`

	// AllowedUsers is an optional whitelist of Telegram user IDs.
	// Empty slice means all users are allowed.
	AllowedUsers []int64 `yaml:"allowed_users"`
}

// IsAllowed returns true if the user ID is permitted to use the bot.
func (c *Config) IsAllowed(userID int64) bool {
	if len(c.AllowedUsers) == 0 {
		return true
	}
	for _, id := range c.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}
