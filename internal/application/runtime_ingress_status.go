package application

import "github.com/ngoclaw/ngoagent/internal/interfaces/apitype"

func runtimeIngressStatus(req apitype.RuntimeIngressRequest) string {
	switch req.Ingress.Kind {
	case "decision":
		return "applied"
	case "resume":
		return "resumed"
	case "message", "reconnect":
		return "completed"
	default:
		return "processed"
	}
}
