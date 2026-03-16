package bot

import (
	"context"
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ngoclaw/ngoagent/internal/interfaces/grpc/agentpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Bot is the main Telegram bot instance.
type Bot struct {
	tg      *tgbotapi.BotAPI
	handler *Handler
	cfg     *Config
}

// New creates a Bot, connects to the gRPC server, and wires all components.
func New(cfg *Config) (*Bot, error) {
	// Connect to NGOAgent gRPC
	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", cfg.GRPCAddr, err)
	}

	client := agentpb.NewAgentServiceClient(conn)

	// Connect to Telegram
	tg, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("telegram bot init: %w", err)
	}

	sessions := NewSessionStore(client)
	handler := NewHandler(tg, client, sessions, cfg)

	log.Printf("[TGBot] Authorized as @%s", tg.Self.UserName)
	return &Bot{tg: tg, handler: handler, cfg: cfg}, nil
}

// Run starts the update polling loop. Blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.tg.GetUpdatesChan(u)
	log.Println("[TGBot] Listening for updates...")

	for {
		select {
		case <-ctx.Done():
			b.tg.StopReceivingUpdates()
			log.Println("[TGBot] Stopped.")
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			go b.handler.Dispatch(update)
		}
	}
}

// bgCtx returns a background context (shared helper for gRPC calls).
func bgCtx() context.Context {
	return context.Background()
}
