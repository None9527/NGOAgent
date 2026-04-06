package llm

// PresetProviderConfig contains the necessary metadata for natively routing a provider's requests.
type PresetProviderConfig struct {
	Name             string
	DefaultBaseURL   string
	RequiredAuthType string // "bearer", "x-api-key", "ak-sk", "none"
}

// PresetProviders maps the 16 core providers imported from the OpenClaw ecosystem.
// This allows NGOAgent's config.yaml to dynamically route to these providers
// without users manually looking up the exact BaseURL or Auth Type.
var PresetProviders = map[string]PresetProviderConfig{
	"openai":     {Name: "OpenAI", DefaultBaseURL: "https://api.openai.com/v1", RequiredAuthType: "bearer"},
	"azure":      {Name: "Azure OpenAI", DefaultBaseURL: "", RequiredAuthType: "api-key"},
	"anthropic":  {Name: "Anthropic", DefaultBaseURL: "https://api.anthropic.com/v1", RequiredAuthType: "x-api-key"},
	"google":     {Name: "Google Gemini", DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta", RequiredAuthType: "x-goog-api-key"},
	"bedrock":    {Name: "AWS Bedrock", DefaultBaseURL: "", RequiredAuthType: "ak-sk"},
	"dashscope":  {Name: "DashScope (阿里云)", DefaultBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", RequiredAuthType: "bearer"},
	"volcengine": {Name: "Volcengine (火山方舟)", DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/v3", RequiredAuthType: "bearer"},
	"groq":       {Name: "Groq", DefaultBaseURL: "https://api.groq.com/openai/v1", RequiredAuthType: "bearer"},
	"mistral":    {Name: "Mistral AI", DefaultBaseURL: "https://api.mistral.ai/v1", RequiredAuthType: "bearer"},
	"cohere":     {Name: "Cohere", DefaultBaseURL: "https://api.cohere.ai/v1", RequiredAuthType: "bearer"},
	"deepseek":   {Name: "DeepSeek", DefaultBaseURL: "https://api.deepseek.com", RequiredAuthType: "bearer"},
	"together":   {Name: "Together AI", DefaultBaseURL: "https://api.together.xyz/v1", RequiredAuthType: "bearer"},
	"cloudflare": {Name: "Cloudflare Workers AI", DefaultBaseURL: "https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/v1", RequiredAuthType: "bearer"},
	"ollama":     {Name: "Ollama", DefaultBaseURL: "http://localhost:11434/v1", RequiredAuthType: "none"},
	"venice":     {Name: "Venice AI", DefaultBaseURL: "https://api.venice.ai/api/v1", RequiredAuthType: "bearer"},
	"zhipuai":    {Name: "ZhipuAI (智谱)", DefaultBaseURL: "https://open.bigmodel.cn/api/paas/v4", RequiredAuthType: "bearer"},
	"minimax":    {Name: "MiniMax", DefaultBaseURL: "https://api.minimax.chat/v1", RequiredAuthType: "bearer"},
}

// GetPresetProvider returns the preset configuration if the provider is known.
func GetPresetProvider(key string) (PresetProviderConfig, bool) {
	pc, ok := PresetProviders[key]
	return pc, ok
}
