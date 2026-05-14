package supervisor

import (
	"fmt"

	pluginpkg "github.com/pdai/pdai/internal/plugin"
)

// Plugin implements long-running process supervision.
type Plugin struct {
	svc     *Service
	handler *Handler
}

func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "supervisor",
		Name:        "Supervisor",
		Version:     "1.0.0",
		Description: "Long-running process supervision with auto-restart and logs",
		Author:      "Pdai",
		Priority:    50,
		Icon:        "ServerCog",
		Category:    "management",
	}
}

func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	if err := ctx.DB.AutoMigrate(&Process{}, &ProcessLog{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if ctx.DB.Migrator().HasColumn(&Process{}, "p_id") && !ctx.DB.Migrator().HasColumn(&Process{}, "pid") {
		if err := ctx.DB.Exec("ALTER TABLE plugin_supervisor_processes ADD COLUMN pid integer DEFAULT 0").Error; err != nil {
			return fmt.Errorf("migrate supervisor pid column: %w", err)
		}
		if err := ctx.DB.Exec("UPDATE plugin_supervisor_processes SET pid = COALESCE(p_id, 0)").Error; err != nil {
			return fmt.Errorf("backfill supervisor pid column: %w", err)
		}
	}

	p.svc = NewService(ctx.DB, ctx.Logger)
	p.handler = NewHandler(p.svc)

	r := ctx.Router
	a := ctx.AdminRouter

	r.GET("/processes", p.handler.ListProcesses)
	r.GET("/processes/:id", p.handler.GetProcess)
	r.GET("/processes/:id/logs", p.handler.ListProcessLogs)
	r.GET("/logs", p.handler.ListAllLogs)

	a.POST("/processes", p.handler.CreateProcess)
	a.PUT("/processes/:id", p.handler.UpdateProcess)
	a.DELETE("/processes/:id", p.handler.DeleteProcess)
	a.POST("/processes/:id/start", p.handler.StartProcess)
	a.POST("/processes/:id/stop", p.handler.StopProcess)
	a.POST("/processes/:id/restart", p.handler.RestartProcess)

	ctx.Logger.Info("Supervisor plugin routes registered")
	return nil
}

func (p *Plugin) Start() error {
	if p.svc != nil {
		p.svc.Start()
	}
	return nil
}

func (p *Plugin) Stop() error {
	if p.svc != nil {
		p.svc.Stop()
	}
	return nil
}

func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "supervisor",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/supervisor", Component: "SupervisorManager", Menu: true, Icon: "ServerCog", Label: "Supervisor", LabelZh: "进程守护"},
		},
		MenuGroup: "tool",
		MenuOrder: 43,
	}
}

var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
