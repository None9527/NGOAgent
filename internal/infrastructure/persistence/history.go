package persistence

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HistoryMessage is the persistence DTO used by the application/service layers.
// It no longer maps to a legacy table; normalized message tables are the only source of truth.
type HistoryMessage struct {
	Role        string
	Content     string
	ToolCalls   string
	ToolCallID  string
	TokenCount  int
	Reasoning   string
	Attachments string
	CreatedAt   time.Time
}

// HistoryStore persists and retrieves conversation history from normalized tables.
type HistoryStore struct {
	db *gorm.DB
}

func NewHistoryStore(db *gorm.DB) *HistoryStore {
	return &HistoryStore{db: db}
}

func (hs *HistoryStore) SaveAll(sessionID string, msgs []HistoryMessage) error {
	return hs.db.Transaction(func(tx *gorm.DB) error {
		if err := ensureConversationRecord(tx, sessionID); err != nil {
			return err
		}
		if err := tx.Where("conversation_id = ?", sessionID).Delete(&MessageRecord{}).Error; err != nil {
			return err
		}
		return hs.saveNormalizedMessages(tx, sessionID, msgs, 0)
	})
}

func (hs *HistoryStore) Save(sessionID string, msg *HistoryMessage) error {
	return hs.db.Transaction(func(tx *gorm.DB) error {
		if err := ensureConversationRecord(tx, sessionID); err != nil {
			return err
		}
		lastSeq, err := hs.lastSeq(tx, sessionID)
		if err != nil {
			return err
		}
		return hs.saveNormalizedMessages(tx, sessionID, []HistoryMessage{*msg}, lastSeq)
	})
}

func (hs *HistoryStore) LoadSession(sessionID string) ([]HistoryMessage, error) {
	return hs.loadNormalizedMessages(sessionID, 0)
}

func (hs *HistoryStore) LoadSessionRecent(sessionID string, limit int) ([]HistoryMessage, error) {
	return hs.loadNormalizedMessages(sessionID, limit)
}

func (hs *HistoryStore) DeleteSession(sessionID string) error {
	return hs.db.Where("conversation_id = ?", sessionID).Delete(&MessageRecord{}).Error
}

func (hs *HistoryStore) AppendBatch(sessionID string, msgs []HistoryMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	return hs.db.Transaction(func(tx *gorm.DB) error {
		if err := ensureConversationRecord(tx, sessionID); err != nil {
			return err
		}
		lastSeq, err := hs.lastSeq(tx, sessionID)
		if err != nil {
			return err
		}
		return hs.saveNormalizedMessages(tx, sessionID, msgs, lastSeq)
	})
}

func (hs *HistoryStore) TruncateSession(sessionID string, keep int) error {
	if keep <= 0 {
		return hs.DeleteSession(sessionID)
	}

	var records []MessageRecord
	if err := hs.db.Where("conversation_id = ?", sessionID).Order("seq DESC").Offset(keep).Find(&records).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return hs.db.Where("id IN ?", ids).Delete(&MessageRecord{}).Error
}

func (hs *HistoryStore) saveNormalizedMessages(tx *gorm.DB, sessionID string, msgs []HistoryMessage, startingSeq int64) error {
	for i, msg := range msgs {
		seq := startingSeq + int64(i) + 1
		if err := hs.saveOneNormalizedMessage(tx, sessionID, seq, msg); err != nil {
			return err
		}
	}
	return nil
}

func (hs *HistoryStore) saveOneNormalizedMessage(tx *gorm.DB, sessionID string, seq int64, msg HistoryMessage) error {
	createdAt := msg.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	messageID := normalizedMessageID(sessionID, seq)

	record := MessageRecord{
		ID:             messageID,
		ConversationID: sessionID,
		Seq:            seq,
		Role:           msg.Role,
		MessageType:    normalizedMessageType(msg.Role),
		ContentText:    msg.Content,
		ReasoningText:  msg.Reasoning,
		ToolCallID:     msg.ToolCallID,
		TokenCount:     msg.TokenCount,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"conversation_id",
			"seq",
			"role",
			"message_type",
			"content_text",
			"reasoning_text",
			"tool_call_id",
			"token_count",
			"created_at",
			"updated_at",
		}),
	}).Create(&record).Error; err != nil {
		return err
	}

	if err := tx.Where("message_id = ?", messageID).Delete(&MessageToolCallRecord{}).Error; err != nil {
		return err
	}
	if err := tx.Where("message_id = ?", messageID).Delete(&MessageAttachmentRecord{}).Error; err != nil {
		return err
	}

	if err := saveNormalizedToolCalls(tx, messageID, createdAt, msg.ToolCalls); err != nil {
		return err
	}
	if err := updateNormalizedToolCallResult(tx, msg); err != nil {
		return err
	}
	return saveNormalizedAttachments(tx, messageID, createdAt, msg.Attachments)
}

func saveNormalizedToolCalls(tx *gorm.DB, messageID string, createdAt time.Time, raw string) error {
	if raw == "" {
		return nil
	}
	var toolCalls []model.ToolCall
	if err := json.Unmarshal([]byte(raw), &toolCalls); err != nil {
		return fmt.Errorf("unmarshal tool calls: %w", err)
	}
	for i, tc := range toolCalls {
		row := MessageToolCallRecord{
			ID:         normalizedChildID(messageID, "tool", i),
			MessageID:  messageID,
			ToolName:   tc.Function.Name,
			ToolCallID: tc.ID,
			Position:   i,
			ArgsJSON:   tc.Function.Arguments,
			Status:     "requested",
			CreatedAt:  createdAt,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func updateNormalizedToolCallResult(tx *gorm.DB, msg HistoryMessage) error {
	if msg.Role != "tool" || msg.ToolCallID == "" {
		return nil
	}
	resultJSON, err := json.Marshal(map[string]any{
		"output": msg.Content,
	})
	if err != nil {
		return fmt.Errorf("marshal tool result: %w", err)
	}
	return tx.Model(&MessageToolCallRecord{}).
		Where("tool_call_id = ? AND status = ?", msg.ToolCallID, "requested").
		Updates(map[string]any{
			"result_json": string(resultJSON),
			"status":      "completed",
		}).Error
}

func saveNormalizedAttachments(tx *gorm.DB, messageID string, createdAt time.Time, raw string) error {
	if raw == "" {
		return nil
	}
	var attachments []model.Attachment
	if err := json.Unmarshal([]byte(raw), &attachments); err != nil {
		return fmt.Errorf("unmarshal attachments: %w", err)
	}
	for i, att := range attachments {
		row := MessageAttachmentRecord{
			ID:           normalizedChildID(messageID, "attachment", i),
			MessageID:    messageID,
			Position:     i,
			Kind:         att.Type,
			Path:         att.Path,
			MIMEType:     att.MimeType,
			Name:         att.Name,
			MetadataJSON: "{}",
			CreatedAt:    createdAt,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (hs *HistoryStore) loadNormalizedMessages(sessionID string, limit int) ([]HistoryMessage, error) {
	var records []MessageRecord
	q := hs.db.Where("conversation_id = ?", sessionID)
	if limit > 0 {
		q = q.Order("seq DESC").Limit(limit)
	} else {
		q = q.Order("seq ASC")
	}
	if err := q.Find(&records).Error; err != nil {
		return nil, err
	}
	if limit > 0 && len(records) > 1 {
		for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
			records[i], records[j] = records[j], records[i]
		}
	}

	out := make([]HistoryMessage, 0, len(records))
	for _, record := range records {
		msg, err := hs.normalizedRecordToHistory(record)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func (hs *HistoryStore) normalizedRecordToHistory(record MessageRecord) (HistoryMessage, error) {
	msg := HistoryMessage{
		Role:       record.Role,
		Content:    record.ContentText,
		ToolCallID: record.ToolCallID,
		TokenCount: record.TokenCount,
		Reasoning:  record.ReasoningText,
		CreatedAt:  record.CreatedAt,
	}

	var toolCalls []MessageToolCallRecord
	if err := hs.db.Where("message_id = ?", record.ID).Order("position ASC").Find(&toolCalls).Error; err != nil {
		return HistoryMessage{}, err
	}
	if len(toolCalls) > 0 {
		exported := make([]model.ToolCall, 0, len(toolCalls))
		for _, tc := range toolCalls {
			exported = append(exported, model.ToolCall{
				ID:   tc.ToolCallID,
				Type: "function",
				Function: model.ToolCallFunc{
					Name:      tc.ToolName,
					Arguments: tc.ArgsJSON,
				},
			})
		}
		raw, err := json.Marshal(exported)
		if err != nil {
			return HistoryMessage{}, err
		}
		msg.ToolCalls = string(raw)
	}

	var attachments []MessageAttachmentRecord
	if err := hs.db.Where("message_id = ?", record.ID).Order("position ASC").Find(&attachments).Error; err != nil {
		return HistoryMessage{}, err
	}
	if len(attachments) > 0 {
		exported := make([]model.Attachment, 0, len(attachments))
		for _, att := range attachments {
			exported = append(exported, model.Attachment{
				Type:     att.Kind,
				Path:     att.Path,
				MimeType: att.MIMEType,
				Name:     att.Name,
			})
		}
		raw, err := json.Marshal(exported)
		if err != nil {
			return HistoryMessage{}, err
		}
		msg.Attachments = string(raw)
	}

	return msg, nil
}

func (hs *HistoryStore) lastSeq(tx *gorm.DB, sessionID string) (int64, error) {
	var record MessageRecord
	if err := tx.Where("conversation_id = ?", sessionID).Order("seq DESC").First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return record.Seq, nil
}

func ensureConversationRecord(tx *gorm.DB, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	var count int64
	if err := tx.Model(&Conversation{}).Where("id = ?", sessionID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	now := time.Now().UTC()
	return tx.Create(&Conversation{
		ID:        sessionID,
		Channel:   "legacy",
		Title:     sessionID,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error
}

func normalizedMessageID(sessionID string, seq int64) string {
	return sessionID + ":msg:" + strconv.FormatInt(seq, 10)
}

func normalizedChildID(messageID, kind string, position int) string {
	return messageID + ":" + kind + ":" + strconv.Itoa(position)
}

func normalizedMessageType(role string) string {
	switch role {
	case "assistant", "user", "tool", "system":
		return role
	default:
		return "message"
	}
}
