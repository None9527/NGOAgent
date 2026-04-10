package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func resolvedRuntimeRunID(r apitype.RuntimeResumeRequest) string {
	if r.Run.RunID != "" {
		return r.Run.RunID
	}
	return r.RunID
}

func decodeRuntimeResumeRequest(r *http.Request) (apitype.RuntimeResumeRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return apitype.RuntimeResumeRequest{}, err
	}
	var req apitype.RuntimeResumeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return apitype.RuntimeResumeRequest{}, err
	}
	return req, nil
}

func decodeRuntimeDecisionApplyRequest(r *http.Request) (apitype.RuntimeDecisionApplyRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return apitype.RuntimeDecisionApplyRequest{}, err
	}

	type runtimeDecisionEnvelope struct {
		SessionID string          `json:"session_id"`
		Decision  json.RawMessage `json:"decision"`
		Kind      string          `json:"kind"`
		Reason    string          `json:"reason"`
		Feedback  string          `json:"feedback"`
	}

	var envelope runtimeDecisionEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return apitype.RuntimeDecisionApplyRequest{}, err
	}
	req := apitype.RuntimeDecisionApplyRequest{SessionID: envelope.SessionID}
	if len(envelope.Decision) > 0 && string(envelope.Decision) != "null" {
		if envelope.Decision[0] == '{' {
			if err := json.Unmarshal(envelope.Decision, &req.Decision); err != nil {
				return apitype.RuntimeDecisionApplyRequest{}, err
			}
			return req, nil
		}
		var legacyDecision string
		if err := json.Unmarshal(envelope.Decision, &legacyDecision); err != nil {
			return apitype.RuntimeDecisionApplyRequest{}, err
		}
		req.Decision.Decision = legacyDecision
	}
	req.Decision.Kind = envelope.Kind
	req.Decision.Reason = envelope.Reason
	req.Decision.Feedback = envelope.Feedback
	return req, nil
}

func decodeRuntimeIngressRequest(r *http.Request) (apitype.RuntimeIngressRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return apitype.RuntimeIngressRequest{}, err
	}
	var req apitype.RuntimeIngressRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return apitype.RuntimeIngressRequest{}, err
	}
	return req, nil
}
