package bot

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Handler dispatches Telegram updates to the appropriate handler functions.
// Uses StreamHandler (HTTP+SSE) for backend communication.
type Handler struct {
	tg       *tgbotapi.BotAPI
	stream   *StreamHandler
	sessions *SessionStore
	cfg      *Config
}

func NewHandler(tg *tgbotapi.BotAPI, stream *StreamHandler, sessions *SessionStore, cfg *Config) *Handler {
	return &Handler{
		tg:       tg,
		stream:   stream,
		sessions: sessions,
		cfg:      cfg,
	}
}

// Dispatch routes an update to the correct handler.
func (h *Handler) Dispatch(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		h.handleCallback(update.CallbackQuery)
		return
	}
	if update.Message == nil {
		return
	}

	msg := update.Message
	userID := msg.From.ID
	chatID := msg.Chat.ID

	if !h.cfg.IsAllowed(userID) {
		h.send(chatID, "⛔ 您没有使用权限。")
		return
	}

	if msg.IsCommand() {
		h.handleCommand(msg)
		return
	}

	h.handleMessage(msg)
}

// handleCommand handles /start /new /stop /status /help
func (h *Handler) handleCommand(msg *tgbotapi.Message) {
	userID := msg.From.ID
	chatID := msg.Chat.ID

	switch msg.Command() {
	case "start":
		sid, err := h.sessions.Reset(userID)
		if err != nil {
			h.send(chatID, fmt.Sprintf("❌ 初始化失败: %v", err))
			return
		}
		h.send(chatID, fmt.Sprintf(
			"👋 欢迎使用 NGOAgent！\n会话已创建: `%s`\n直接发消息开始对话。", sid))

	case "new":
		sid, err := h.sessions.Reset(userID)
		if err != nil {
			h.send(chatID, fmt.Sprintf("❌ 创建失败: %v", err))
			return
		}
		h.send(chatID, fmt.Sprintf("✅ 新会话已创建: `%s`", sid))

	case "stop":
		sid, err := h.sessions.Get(userID)
		if err != nil {
			h.send(chatID, "⚠️ 当前没有活跃会话。")
			return
		}
		if err := h.stream.Stop(sid); err != nil {
			h.send(chatID, fmt.Sprintf("❌ 停止失败: %v", err))
			return
		}
		h.send(chatID, "🛑 已发送停止信号。")

	case "status":
		sid, err := h.sessions.Get(userID)
		if err != nil {
			h.send(chatID, "⚠️ 当前没有活跃会话。")
			return
		}
		h.send(chatID, fmt.Sprintf("📊 *会话状态*\n会话: `%s`\n状态: 运行中", sid))

	case "help":
		h.send(chatID, strings.Join([]string{
			"*NGOAgent Bot 命令*",
			"/start — 创建新会话",
			"/new — 重置当前会话",
			"/stop — 停止正在运行的任务",
			"/status — 查看会话状态",
			"/help — 显示此帮助",
			"",
			"直接发送消息与 Agent 对话 💬",
		}, "\n"))
	}
}

// handleMessage handles regular chat messages (non-commands).
func (h *Handler) handleMessage(msg *tgbotapi.Message) {
	userID := msg.From.ID
	chatID := msg.Chat.ID

	sid, err := h.sessions.Get(userID)
	if err != nil {
		h.send(chatID, fmt.Sprintf("❌ 会话创建失败: %v", err))
		return
	}

	tg := h.tg
	go h.stream.StreamToTelegram(
		context.Background(),
		sid, msg.Text,
		func(text string) (int, error) {
			m := tgbotapi.NewMessage(chatID, text)
			sent, err := tg.Send(m)
			if err != nil {
				return 0, err
			}
			return sent.MessageID, nil
		},
		func(msgID int, text string) {
			if text == "" {
				return
			}
			edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
			_, _ = tg.Send(edit)
		},
	)
}

// handleCallback processes inline keyboard approval callbacks.
// Callback data format: "approve:<approvalID>:<1|0>"
func (h *Handler) handleCallback(cb *tgbotapi.CallbackQuery) {
	parts := strings.SplitN(cb.Data, ":", 3)
	if len(parts) != 3 || parts[0] != "approve" {
		return
	}
	approvalID := parts[1]
	approved := parts[2] == "1"

	err := h.stream.Approve(approvalID, approved)

	answer := tgbotapi.NewCallback(cb.ID, "")
	if err != nil {
		answer.Text = fmt.Sprintf("操作失败: %v", err)
	} else if approved {
		answer.Text = "✅ 已允许执行"
	} else {
		answer.Text = "❌ 已拒绝"
	}
	_, _ = h.tg.Request(answer)

	// Update button message to reflect decision
	label := "✅ 已允许"
	if !approved {
		label = "❌ 已拒绝"
	}
	if cb.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			cb.Message.Chat.ID, cb.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{{
				tgbotapi.NewInlineKeyboardButtonData(label, "done"),
			}}},
		)
		_, _ = h.tg.Send(edit)
	}
}

func (h *Handler) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, _ = h.tg.Send(msg)
}
