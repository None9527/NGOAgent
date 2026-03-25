package bot

// Config holds Telegram bot configuration loaded from the main config.
type Config struct {
	// Token is the Telegram Bot API token from @BotFather.
	Token string `yaml:"token"`

	// HTTPAddr is the NGOAgent HTTP server address (default: http://localhost:19996).
	HTTPAddr string `yaml:"http_addr"`

	// AuthToken is the Bearer token for HTTP API authentication.
	AuthToken string `yaml:"auth_token"`

	// GRPCAddr is deprecated — kept for backward compatibility.
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

// EffectiveHTTPAddr returns the HTTP address, falling back to default.
func (c *Config) EffectiveHTTPAddr() string {
	if c.HTTPAddr != "" {
		return c.HTTPAddr
	}
	return "http://localhost:19996"
}
