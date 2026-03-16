package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ngoclaw/ngoagent/internal/interfaces/bot"
	"gopkg.in/yaml.v3"
)

func main() {
	cfg := loadConfig()

	b, err := bot.New(cfg)
	if err != nil {
		log.Fatalf("[TGBot] Failed to initialize: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := b.Run(ctx); err != nil {
		log.Fatalf("[TGBot] Run error: %v", err)
	}
}

// loadConfig reads telegram config from the environment or a config file.
// Priority: env vars > ~/.ngoagent/config.yaml > ./config.yaml
func loadConfig() *bot.Config {
	cfg := &bot.Config{
		GRPCAddr: "localhost:50051",
	}

	// Allow pure env-var based config for Docker/CI
	if token := os.Getenv("TELEGRAM_TOKEN"); token != "" {
		cfg.Token = token
	}
	if addr := os.Getenv("NGOAGENT_GRPC_ADDR"); addr != "" {
		cfg.GRPCAddr = addr
	}

	// Try loading from config.yaml if env vars are not set
	if cfg.Token == "" {
		candidates := []string{
			"config.yaml",
			os.ExpandEnv("$HOME/.ngoagent/config.yaml"),
		}
		for _, path := range candidates {
			if data, err := os.ReadFile(path); err == nil {
				var raw struct {
					Telegram *bot.Config `yaml:"telegram"`
				}
				if err := yaml.Unmarshal(data, &raw); err == nil && raw.Telegram != nil {
					if raw.Telegram.Token != "" {
						cfg = raw.Telegram
					}
					if cfg.GRPCAddr == "" {
						cfg.GRPCAddr = "localhost:50051"
					}
				}
				break
			}
		}
	}

	if cfg.Token == "" {
		log.Fatal("[TGBot] No Telegram token configured. Set TELEGRAM_TOKEN env var or telegram.token in config.yaml")
	}

	return cfg
}
