package bot

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ngoclaw/ngoagent/internal/interfaces/grpc/agentpb"
)

// streamChat calls gRPC Chat() and pipes events into Telegram.
// It edits the initial placeholder message progressively as tokens arrive.
func streamChat(
	ctx context.Context,
	tg *tgbotapi.BotAPI,
	client agentpb.AgentServiceClient,
	chatID int64,
	sessionID string,
	userMsg string,
) {
	// Send typing action
	typingStop := startTyping(ctx, tg, chatID)
	defer typingStop()

	// Send placeholder message that we'll edit progressively
	placeholder := tgbotapi.NewMessage(chatID, "⏳ 思考中...")
	sent, err := tg.Send(placeholder)
	if err != nil {
		return
	}

	stream, err := client.Chat(ctx, &agentpb.AgentChatRequest{
		Message:   userMsg,
		SessionId: sessionID,
	})
	if err != nil {
		editMsg(tg, chatID, sent.MessageID, fmt.Sprintf("❌ 连接失败: %v", err))
		return
	}

	var (
		textBuf      strings.Builder
		lastEditLen  int
		toolMessages []string
	)

	flushText := func(final bool) {
		current := textBuf.String()
		if len(current)-lastEditLen < 20 && !final {
			return
		}
		lastEditLen = len(current)
		display := current
		if !final {
			display += " ▌"
		}
		editMsg(tg, chatID, sent.MessageID, display)
	}

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			textBuf.WriteString(fmt.Sprintf("\n\n❌ 流中断: %v", err))
			break
		}

		switch event.Type {
		case "text_delta":
			textBuf.WriteString(event.Text)
			flushText(false)

		case "thinking":
			// silently discard thinking tokens

		case "tool_call":
			toolMsg := fmt.Sprintf("🔧 `%s`", event.ToolName)
			toolMessages = append(toolMessages, toolMsg)
			// Send separate tool notification
			notification := tgbotapi.NewMessage(chatID, toolMsg)
			notification.ParseMode = "Markdown"
			_, _ = tg.Send(notification)

		case "approval_request":
			// Pause typing, send approval keyboard
			typingStop()
			sendApprovalKeyboard(tg, chatID, sessionID, event.CallId, event.ToolName, event.ToolInput)

		case "step_done":
			flushText(false)

		case "done", "error":
			break
		}
	}

	// Final edit with complete text
	finalText := textBuf.String()
	if finalText == "" {
		finalText = strings.Join(toolMessages, "\n")
	}
	if finalText == "" {
		finalText = "✅ 完成"
	}
	editMsg(tg, chatID, sent.MessageID, finalText)
}

// sendApprovalKeyboard sends an inline keyboard for tool approval.
func sendApprovalKeyboard(tg *tgbotapi.BotAPI, chatID int64, sessionID, callID, toolName, toolInput string) {
	text := fmt.Sprintf("⚠️ *需要审批*\n工具: `%s`", toolName)
	if toolInput != "" && len(toolInput) < 200 {
		text += fmt.Sprintf("\n参数: ```\n%s\n```", toolInput)
	}

	approveData := fmt.Sprintf("approve:%s:%s:1", sessionID, callID)
	rejectData := fmt.Sprintf("approve:%s:%s:0", sessionID, callID)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ 允许", approveData),
			tgbotapi.NewInlineKeyboardButtonData("❌ 拒绝", rejectData),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, _ = tg.Send(msg)
}

// editMsg edits an existing Telegram message with new text.
func editMsg(tg *tgbotapi.BotAPI, chatID int64, msgID int, text string) {
	if text == "" {
		return
	}
	// Telegram max message length is 4096 chars
	if len(text) > 4000 {
		text = text[:4000] + "\n…(截断)"
	}
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	_, _ = tg.Send(edit)
}

// startTyping sends periodic typing actions until the returned stop func is called.
func startTyping(ctx context.Context, tg *tgbotapi.BotAPI, chatID int64) func() {
	ctx2, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
		_, _ = tg.Send(action)
		for {
			select {
			case <-ctx2.Done():
				return
			case <-ticker.C:
				_, _ = tg.Send(action)
			}
		}
	}()
	return cancel
}
