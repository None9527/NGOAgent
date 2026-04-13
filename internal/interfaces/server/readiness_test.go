package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

type readinessAdminStub struct {
	health apitype.HealthResponse
}

func (s readinessAdminStub) ListModels() apitype.ModelListResponse {
	return apitype.ModelListResponse{}
}
func (s readinessAdminStub) SwitchModel(string) error               { return nil }
func (s readinessAdminStub) CurrentModel() string                   { return "" }
func (s readinessAdminStub) GetConfig() map[string]any              { return nil }
func (s readinessAdminStub) SetConfig(string, any) error            { return nil }
func (s readinessAdminStub) AddProvider(config.ProviderDef) error   { return nil }
func (s readinessAdminStub) RemoveProvider(string) error            { return nil }
func (s readinessAdminStub) AddMCPServer(config.MCPServerDef) error { return nil }
func (s readinessAdminStub) RemoveMCPServer(string) error           { return nil }
func (s readinessAdminStub) ListTools() []apitype.ToolInfoResponse  { return nil }
func (s readinessAdminStub) EnableTool(string) error                { return nil }
func (s readinessAdminStub) DisableTool(string) error               { return nil }
func (s readinessAdminStub) Health() apitype.HealthResponse         { return s.health }
func (s readinessAdminStub) GetSecurity() apitype.SecurityResponse  { return apitype.SecurityResponse{} }
func (s readinessAdminStub) GetContextStats() apitype.ContextStats  { return apitype.ContextStats{} }
func (s readinessAdminStub) GetSystemInfo() apitype.SystemInfoResponse {
	return apitype.SystemInfoResponse{}
}
func (s readinessAdminStub) CronStatus() map[string]any                         { return nil }
func (s readinessAdminStub) ListCronJobs() ([]apitype.CronJobInfo, error)       { return nil, nil }
func (s readinessAdminStub) CreateCronJob(string, string, string) error         { return nil }
func (s readinessAdminStub) DeleteCronJob(string) error                         { return nil }
func (s readinessAdminStub) EnableCronJob(string) error                         { return nil }
func (s readinessAdminStub) DisableCronJob(string) error                        { return nil }
func (s readinessAdminStub) RunCronJobNow(string) error                         { return nil }
func (s readinessAdminStub) ListCronLogs(string) ([]apitype.CronLogInfo, error) { return nil, nil }
func (s readinessAdminStub) ReadCronLog(string, string) (string, error)         { return "", nil }
func (s readinessAdminStub) ListSkills() ([]apitype.SkillInfoResponse, error)   { return nil, nil }
func (s readinessAdminStub) ReadSkillContent(string) (string, error)            { return "", nil }
func (s readinessAdminStub) RefreshSkills() error                               { return nil }
func (s readinessAdminStub) DeleteSkill(string) error                           { return nil }
func (s readinessAdminStub) ListMCPServers() ([]apitype.MCPServerInfo, error)   { return nil, nil }
func (s readinessAdminStub) ListMCPTools() ([]apitype.MCPToolInfo, error)       { return nil, nil }
func (s readinessAdminStub) ListBrainArtifacts(string) ([]apitype.BrainArtifactInfo, error) {
	return nil, nil
}
func (s readinessAdminStub) ReadBrainArtifact(string, string) (string, error) { return "", nil }
func (s readinessAdminStub) ListKI() ([]apitype.KIInfo, error)                { return nil, nil }
func (s readinessAdminStub) GetKI(string) (apitype.KIDetailResponse, error) {
	return apitype.KIDetailResponse{}, nil
}
func (s readinessAdminStub) DeleteKI(string) error { return nil }
func (s readinessAdminStub) ListKIArtifacts(string) ([]apitype.BrainArtifactInfo, error) {
	return nil, nil
}
func (s readinessAdminStub) ReadKIArtifact(string, string) (string, error) { return "", nil }

func TestReadyEndpoint_AllowsAnonymousReadyProbe(t *testing.T) {
	srv := NewServer(Capabilities{
		Admin: readinessAdminStub{health: apitype.HealthResponse{
			Status: "ok",
			Ready:  true,
		}},
	}, ":0", "secret")

	req := httptest.NewRequest(http.MethodGet, "/v1/ready", nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from ready endpoint, got %d", rec.Code)
	}

	var health apitype.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode readiness payload: %v", err)
	}
	if !health.Ready || health.Status != "ok" {
		t.Fatalf("unexpected readiness payload: %#v", health)
	}
}

func TestReadyEndpoint_Returns503WhenNotReady(t *testing.T) {
	srv := NewServer(Capabilities{
		Admin: readinessAdminStub{health: apitype.HealthResponse{
			Status: "degraded",
			Ready:  false,
			Checks: map[string]string{"runtime": "missing"},
		}},
	}, ":0", "secret")

	req := httptest.NewRequest(http.MethodGet, "/v1/ready", nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 from not-ready endpoint, got %d", rec.Code)
	}
}

func TestHealthEndpointStillAllowsAnonymousAccess(t *testing.T) {
	srv := NewServer(Capabilities{
		Admin: readinessAdminStub{health: apitype.HealthResponse{
			Status: "ok",
			Ready:  true,
		}},
	}, ":0", "secret")

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected anonymous health access, got %d", rec.Code)
	}
}

func TestAuthMiddleware_StillProtectsNonProbeRoutes(t *testing.T) {
	srv := NewServer(Capabilities{
		Admin: readinessAdminStub{health: apitype.HealthResponse{Status: "ok", Ready: true}},
	}, ":0", "secret")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected auth protection for non-probe route, got %d", rec.Code)
	}
}
