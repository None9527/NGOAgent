package bot

import (
	"context"
	"fmt"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot is the main Telegram bot instance.
// Uses HTTP+SSE to communicate with the application transport backend.
type Bot struct {
	tg      *tgbotapi.BotAPI
	handler *Handler
	cfg     *Config
}

// New creates a Bot, connects to the HTTP server, and wires all components.
func New(cfg *Config) (*Bot, error) {
	// HTTP StreamHandler replaces gRPC client
	stream := NewStreamHandler(cfg.EffectiveHTTPAddr(), cfg.AuthToken)

	// Connect to Telegram
	tg, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, err
	}

	sessions := NewSessionStore(stream)
	handler := NewHandler(tg, stream, sessions, cfg)

	slog.Info(fmt.Sprintf("[TGBot] Authorized as @%s (HTTP: %s)", tg.Self.UserName, cfg.EffectiveHTTPAddr()))
	return &Bot{tg: tg, handler: handler, cfg: cfg}, nil
}

// Run starts the update polling loop. Blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.tg.GetUpdatesChan(u)
	slog.Info(fmt.Sprint("[TGBot] Listening for updates..."))

	for {
		select {
		case <-ctx.Done():
			b.tg.StopReceivingUpdates()
			slog.Info(fmt.Sprint("[TGBot] Stopped."))
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			go b.handler.Dispatch(update)
		}
	}
}
