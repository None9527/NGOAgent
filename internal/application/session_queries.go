package application

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func (s *SessionQueries) NewSession(title string) apitype.SessionResponse {
	sessionID, err := s.sessMgr.CreatePersisted("web", title)
	if err != nil {
		slog.Info(fmt.Sprintf("[NewSession] DB create failed, falling back to memory: %v", err))
		session := s.sessMgr.New(title)
		return apitype.SessionResponse{SessionID: session.ID, Title: session.Title}
	}
	return apitype.SessionResponse{SessionID: sessionID, Title: title}
}

func (s *SessionQueries) ListSessions() apitype.SessionListResponse {
	dbSessions, err := s.sessMgr.ListFromRepo(200, 0)
	if err == nil && len(dbSessions) > 0 {
		infos := make([]apitype.SessionInfo, 0, len(dbSessions))
		for _, session := range dbSessions {
			title := session.Title
			if mem, ok := s.sessMgr.Get(session.ID); ok && mem.Title != "" {
				title = mem.Title
			}
			infos = append(infos, apitype.SessionInfo{
				ID:        session.ID,
				Title:     title,
				Channel:   session.Channel,
				CreatedAt: session.CreatedAt,
				UpdatedAt: session.UpdatedAt,
			})
		}
		return apitype.SessionListResponse{Sessions: infos, Active: s.sessMgr.Active()}
	}

	sessions := s.sessMgr.List()
	infos := make([]apitype.SessionInfo, len(sessions))
	for i, session := range sessions {
		infos[i] = apitype.SessionInfo{ID: session.ID, Title: session.Title}
	}
	return apitype.SessionListResponse{Sessions: infos, Active: s.sessMgr.Active()}
}

func (s *SessionQueries) GetHistory(sessionID string) ([]apitype.HistoryMessage, error) {
	if s.histQuery == nil {
		return nil, fmt.Errorf("history store not configured")
	}

	if sessionID != "" {
		s.sessMgr.Activate(sessionID)
	}

	if loop := service.ResidentSessionLoop(s.loop, s.loopPool, sessionID); loop != nil {
		msgs := loop.GetHistory()
		if len(msgs) > 0 {
			return s.convertLLMToHistory(msgs), nil
		}
	}

	exports, err := s.histQuery.LoadAll(sessionID)
	if err != nil {
		return nil, err
	}
	return s.convertExportsToHistory(exports), nil
}

func (s *SessionQueries) convertLLMToHistory(msgs []llm.Message) []apitype.HistoryMessage {
	nameMap := make(map[string]string)
	argsMap := make(map[string]string)
	for _, msg := range msgs {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				nameMap[toolCall.ID] = toolCall.Function.Name
				argsMap[toolCall.ID] = string(toolCall.Function.Arguments)
			}
		}
	}

	apiMsgs := make([]apitype.HistoryMessage, len(msgs))
	for i, msg := range msgs {
		historyMsg := apitype.HistoryMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Reasoning: msg.Reasoning,
		}
		if msg.Role == "tool" && msg.ToolCallID != "" {
			historyMsg.ToolName = nameMap[msg.ToolCallID]
			historyMsg.ToolArgs = argsMap[msg.ToolCallID]
		}
		apiMsgs[i] = historyMsg
	}
	return apiMsgs
}

func (s *SessionQueries) convertExportsToHistory(exports []service.HistoryExport) []apitype.HistoryMessage {
	nameMap := make(map[string]string)
	argsMap := make(map[string]string)
	for _, export := range exports {
		if export.Role == "assistant" && export.ToolCalls != "" {
			var calls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}
			if json.Unmarshal([]byte(export.ToolCalls), &calls) == nil {
				for _, call := range calls {
					if call.ID == "" {
						continue
					}
					if call.Function.Name != "" {
						nameMap[call.ID] = call.Function.Name
					}
					if call.Function.Arguments != "" {
						argsMap[call.ID] = call.Function.Arguments
					}
				}
			}
		}
	}

	msgs := make([]apitype.HistoryMessage, len(exports))
	for i, export := range exports {
		msg := apitype.HistoryMessage{
			Role:      export.Role,
			Content:   export.Content,
			Reasoning: export.Reasoning,
		}
		if export.Role == "tool" && export.ToolCallID != "" {
			msg.ToolName = nameMap[export.ToolCallID]
			msg.ToolArgs = argsMap[export.ToolCallID]
		}
		msgs[i] = msg
	}
	return msgs
}
