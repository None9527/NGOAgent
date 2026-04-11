package application

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

func (a *AdminCommands) SwitchModel(name string) error {
	return a.router.SwitchModel(name)
}

func (a *AdminCommands) SetConfig(key string, value any) error {
	return a.cfg.Set(key, value)
}

func (a *AdminCommands) AddProvider(provider config.ProviderDef) error {
	return a.cfg.AddProvider(provider)
}

func (a *AdminCommands) RemoveProvider(name string) error {
	return a.cfg.RemoveProvider(name)
}

func (a *AdminCommands) AddMCPServer(server config.MCPServerDef) error {
	return a.cfg.AddMCPServer(server)
}

func (a *AdminCommands) RemoveMCPServer(name string) error {
	return a.cfg.RemoveMCPServer(name)
}

func (a *AdminCommands) EnableTool(name string) error {
	return a.toolAdmin.Enable(name)
}

func (a *AdminCommands) DisableTool(name string) error {
	return a.toolAdmin.Disable(name)
}

func (a *AdminCommands) CreateCronJob(name, schedule, prompt string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Create(name, schedule, prompt)
}

func (a *AdminCommands) DeleteCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Delete(name)
}

func (a *AdminCommands) EnableCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Enable(name)
}

func (a *AdminCommands) DisableCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Disable(name)
}

func (a *AdminCommands) RunCronJobNow(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.RunNow(name)
}

func (a *AdminCommands) RefreshSkills() error {
	if a.skillMgr == nil {
		return fmt.Errorf("skill manager not configured")
	}
	a.skillMgr.Discover()
	return nil
}

func (a *AdminCommands) DeleteSkill(name string) error {
	if a.skillMgr == nil {
		return fmt.Errorf("skill manager not configured")
	}
	return a.skillMgr.Delete(name)
}

func (a *AdminCommands) DeleteKI(id string) error {
	if a.kiStore == nil || id == "" {
		return fmt.Errorf("KI store not configured or id empty")
	}
	return os.RemoveAll(filepath.Join(a.kiStore.BaseDir(), id))
}
