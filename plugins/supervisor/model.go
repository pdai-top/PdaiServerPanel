package supervisor

import "time"

const (
	StatusStopped  = "stopped"
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusStopping = "stopping"
	StatusFailed   = "failed"

	StreamStdout = "stdout"
	StreamStderr = "stderr"
	StreamSystem = "system"
)

// Process defines a long-running command supervised by Pdai.
type Process struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	Name            string     `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Command         string     `gorm:"type:text;not null" json:"command"`
	WorkingDir      string     `gorm:"size:512" json:"working_dir"`
	Env             string     `gorm:"type:text" json:"env"`
	Enabled         bool       `gorm:"default:true" json:"enabled"`
	Autostart       bool       `gorm:"default:true" json:"autostart"`
	Autorestart     bool       `gorm:"default:true" json:"autorestart"`
	RestartDelaySec int        `gorm:"default:3" json:"restart_delay_sec"`
	StopTimeoutSec  int        `gorm:"default:10" json:"stop_timeout_sec"`
	MaxRestarts     int        `gorm:"default:10" json:"max_restarts"`
	RestartCount    int        `gorm:"default:0" json:"restart_count"`
	Status          string     `gorm:"size:32;default:stopped" json:"status"`
	PID             int        `gorm:"column:pid" json:"pid"`
	ExitCode        int        `json:"exit_code"`
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastStoppedAt   *time.Time `json:"last_stopped_at"`
	LastError       string     `gorm:"type:text" json:"last_error"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (Process) TableName() string {
	return "plugin_supervisor_processes"
}

// ProcessLog stores stdout, stderr, and lifecycle events for supervised processes.
type ProcessLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProcessID uint      `gorm:"index;not null" json:"process_id"`
	Name      string    `gorm:"size:128" json:"name"`
	Stream    string    `gorm:"size:16" json:"stream"`
	Line      string    `gorm:"type:text" json:"line"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (ProcessLog) TableName() string {
	return "plugin_supervisor_logs"
}

type ProcessRequest struct {
	Name            string `json:"name" binding:"required"`
	Command         string `json:"command" binding:"required"`
	WorkingDir      string `json:"working_dir"`
	Env             string `json:"env"`
	Enabled         *bool  `json:"enabled"`
	Autostart       *bool  `json:"autostart"`
	Autorestart     *bool  `json:"autorestart"`
	RestartDelaySec int    `json:"restart_delay_sec"`
	StopTimeoutSec  int    `json:"stop_timeout_sec"`
	MaxRestarts     int    `json:"max_restarts"`
}
