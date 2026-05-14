package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const serviceTemplate = `[Unit]
Description=Pdai Project: {{.Name}}
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory={{.WorkDir}}
ExecStart={{.StartCommand}}
Restart=on-failure
RestartSec=5
{{range .EnvLines}}Environment={{.}}
{{end}}{{if .MemoryMax}}MemoryMax={{.MemoryMax}}
{{end}}{{if .CPUQuota}}CPUQuota={{.CPUQuota}}
{{end}}# Logging
StandardOutput=append:{{.LogFile}}
StandardError=append:{{.LogFile}}

[Install]
WantedBy=multi-user.target
`

var svcTmpl = template.Must(template.New("service").Parse(serviceTemplate))

const openRCServiceTemplate = `#!/sbin/openrc-run
description={{.Description}}
pidfile="/run/{{.ServiceName}}.pid"
{{range .EnvLines}}export {{.}}
{{end}}
depend() {
	need net
}

start() {
	ebegin "Starting {{.ServiceName}}"
	start-stop-daemon --start --background --make-pidfile --pidfile "$pidfile" --chdir {{.WorkDir}} --exec /bin/sh -- -c {{.StartCommand}}
	eend $?
}

stop() {
	ebegin "Stopping {{.ServiceName}}"
	start-stop-daemon --stop --pidfile "$pidfile"
	eend $?
}
`

var openRCSvcTmpl = template.Must(template.New("openrc-service").Parse(openRCServiceTemplate))

// ProcessManager manages project processes via systemd.
type ProcessManager struct {
	logDir string
}

// NewProcessManager creates a new process manager.
func NewProcessManager(logDir string) *ProcessManager {
	return &ProcessManager{logDir: logDir}
}

// ServiceName returns the systemd service name for a project.
func ServiceName(projectID uint) string {
	return fmt.Sprintf("pdai-project-%d", projectID)
}

// Install creates and enables a systemd service for the project.
func (pm *ProcessManager) Install(project *Project, workDir string) error {
	return pm.installService(ServiceName(project.ID), project, workDir, project.Port)
}

// InstallStaging creates a staging systemd service with a custom port for zero-downtime deployment.
func (pm *ProcessManager) InstallStaging(project *Project, workDir string, port int) error {
	return pm.installService(ServiceName(project.ID)+"-staging", project, workDir, port)
}

// PromoteStaging stops the main service, removes it, renames the staging service, and reloads systemd.
func (pm *ProcessManager) PromoteStaging(projectID uint) error {
	mainName := ServiceName(projectID)
	stagingName := mainName + "-staging"

	// Stop and remove old main service
	stopService(mainName)
	disableService(mainName)
	mainUnitPath := serviceFilePath(mainName)
	os.Remove(mainUnitPath)

	// Rename staging service file to main
	stagingUnitPath := serviceFilePath(stagingName)
	os.Rename(stagingUnitPath, mainUnitPath)

	// Reload service manager to recognize the renamed file
	if err := reloadServiceManager(); err != nil {
		return err
	}

	// Enable the service under its canonical name
	return enableService(mainName)
}

// CleanupStaging removes a staging service if it exists (on failure).
func (pm *ProcessManager) CleanupStaging(projectID uint) {
	stagingName := ServiceName(projectID) + "-staging"
	stopService(stagingName)
	disableService(stagingName)
	unitPath := serviceFilePath(stagingName)
	os.Remove(unitPath)
	reloadServiceManager()
}

// StartStaging starts the staging service.
func (pm *ProcessManager) StartStaging(projectID uint) error {
	return startService(ServiceName(projectID) + "-staging")
}

// IsStagingRunning checks if the staging service is active.
func (pm *ProcessManager) IsStagingRunning(projectID uint) bool {
	return isServiceRunning(ServiceName(projectID) + "-staging")
}

// installService is the internal implementation for creating a systemd service unit.
func (pm *ProcessManager) installService(serviceName string, project *Project, workDir string, port int) error {
	unitPath := serviceFilePath(serviceName)

	// Prepare env lines
	var envLines []string
	for _, ev := range project.EnvVarList {
		envLines = append(envLines, fmt.Sprintf("%s=%s", ev.Key, ev.Value))
	}
	if port > 0 {
		envLines = append(envLines, fmt.Sprintf("PORT=%d", port))
	}

	// Runtime log file
	runtimeLog := filepath.Join(pm.logDir, fmt.Sprintf("project_%d", project.ID), "runtime.log")
	os.MkdirAll(filepath.Dir(runtimeLog), 0755)

	// Resource limits
	var memoryMax, cpuQuota string
	if project.MemoryLimit > 0 {
		memoryMax = fmt.Sprintf("%dM", project.MemoryLimit)
	}
	if project.CPULimit > 0 {
		cpuQuota = fmt.Sprintf("%d%%", project.CPULimit)
	}

	data := struct {
		Name         string
		WorkDir      string
		StartCommand string
		EnvLines     []string
		LogFile      string
		MemoryMax    string
		CPUQuota     string
	}{
		Name:         project.Name,
		WorkDir:      workDir,
		StartCommand: resolveStartCommand(project.StartCommand, workDir),
		EnvLines:     envLines,
		LogFile:      runtimeLog,
		MemoryMax:    memoryMax,
		CPUQuota:     cpuQuota,
	}

	if usesOpenRC() {
		openRCData := struct {
			Description  string
			ServiceName  string
			WorkDir      string
			StartCommand string
			EnvLines     []string
		}{
			Description:  shellSingleQuote("Pdai Project: " + project.Name),
			ServiceName:  serviceName,
			WorkDir:      shellSingleQuote(workDir),
			StartCommand: shellSingleQuote("exec " + data.StartCommand),
			EnvLines:     openRCEnvLines(project.EnvVarList, port),
		}
		f, err := os.Create(unitPath)
		if err != nil {
			return fmt.Errorf("create OpenRC service file: %w", err)
		}
		if err := openRCSvcTmpl.Execute(f, openRCData); err != nil {
			f.Close()
			return fmt.Errorf("render OpenRC service template: %w", err)
		}
		f.Close()
		if err := os.Chmod(unitPath, 0755); err != nil {
			return fmt.Errorf("chmod OpenRC service file: %w", err)
		}
	} else {
		f, err := os.Create(unitPath)
		if err != nil {
			return fmt.Errorf("create service file: %w", err)
		}
		defer f.Close()

		if err := svcTmpl.Execute(f, data); err != nil {
			return fmt.Errorf("render service template: %w", err)
		}
	}

	// Reload and enable service
	if err := reloadServiceManager(); err != nil {
		return err
	}
	return enableService(serviceName)
}

// Start starts the project's systemd service.
func (pm *ProcessManager) Start(projectID uint) error {
	return startService(ServiceName(projectID))
}

// Stop stops the project's systemd service.
func (pm *ProcessManager) Stop(projectID uint) error {
	return stopService(ServiceName(projectID))
}

// Restart restarts the project's systemd service.
func (pm *ProcessManager) Restart(projectID uint) error {
	return restartService(ServiceName(projectID))
}

// Uninstall stops, disables, and removes the systemd service.
func (pm *ProcessManager) Uninstall(projectID uint) error {
	name := ServiceName(projectID)
	stopService(name)
	disableService(name)
	unitPath := serviceFilePath(name)
	os.Remove(unitPath)
	return reloadServiceManager()
}

// IsRunning checks if the project's systemd service is active.
func (pm *ProcessManager) IsRunning(projectID uint) bool {
	return isServiceRunning(ServiceName(projectID))
}

// RuntimeLogPath returns the path to the runtime log file.
func (pm *ProcessManager) RuntimeLogPath(projectID uint) string {
	return filepath.Join(pm.logDir, fmt.Sprintf("project_%d", projectID), "runtime.log")
}

// ReadRuntimeLog reads the last N lines of the runtime log.
func (pm *ProcessManager) ReadRuntimeLog(projectID uint, lines int) (string, error) {
	logPath := pm.RuntimeLogPath(projectID)
	if _, err := os.Stat(logPath); err != nil {
		return "", nil
	}
	cmd := exec.Command("tail", "-n", fmt.Sprintf("%d", lines), logPath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s (%w)", strings.Join(args, " "), string(out), err)
	}
	return nil
}

func usesOpenRC() bool {
	if _, err := exec.LookPath("rc-service"); err != nil {
		return false
	}
	if _, err := exec.LookPath("rc-update"); err != nil {
		return false
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return false
	}
	return true
}

func serviceFilePath(name string) string {
	if usesOpenRC() {
		return filepath.Join("/etc/init.d", name)
	}
	return filepath.Join("/etc/systemd/system", name+".service")
}

func reloadServiceManager() error {
	if usesOpenRC() {
		return nil
	}
	return systemctl("daemon-reload")
}

func enableService(name string) error {
	if usesOpenRC() {
		cmd := exec.Command("rc-update", "add", name, "default")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("rc-update add %s: %s (%w)", name, string(out), err)
		}
		return nil
	}
	return systemctl("enable", name)
}

func disableService(name string) error {
	if usesOpenRC() {
		cmd := exec.Command("rc-update", "del", name, "default")
		out, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(string(out), "not found") {
			return fmt.Errorf("rc-update del %s: %s (%w)", name, string(out), err)
		}
		return nil
	}
	return systemctl("disable", name)
}

func startService(name string) error {
	if usesOpenRC() {
		cmd := exec.Command("rc-service", name, "start")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("rc-service %s start: %s (%w)", name, string(out), err)
		}
		return nil
	}
	return systemctl("start", name)
}

func stopService(name string) error {
	if usesOpenRC() {
		cmd := exec.Command("rc-service", name, "stop")
		out, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(strings.ToLower(string(out)), "not started") {
			return fmt.Errorf("rc-service %s stop: %s (%w)", name, string(out), err)
		}
		return nil
	}
	return systemctl("stop", name)
}

func restartService(name string) error {
	if usesOpenRC() {
		cmd := exec.Command("rc-service", name, "restart")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("rc-service %s restart: %s (%w)", name, string(out), err)
		}
		return nil
	}
	return systemctl("restart", name)
}

func isServiceRunning(name string) bool {
	if usesOpenRC() {
		return exec.Command("rc-service", name, "status").Run() == nil
	}
	return exec.Command("systemctl", "is-active", "--quiet", name).Run() == nil
}

func openRCEnvLines(envVars []EnvVar, port int) []string {
	lines := make([]string, 0, len(envVars)+1)
	for _, ev := range envVars {
		key := strings.TrimSpace(ev.Key)
		if !validShellEnvKey(key) {
			continue
		}
		lines = append(lines, key+"="+shellSingleQuote(ev.Value))
	}
	if port > 0 {
		lines = append(lines, fmt.Sprintf("PORT=%d", port))
	}
	return lines
}

func validShellEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if i == 0 {
			if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				return false
			}
			continue
		}
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// ExtraProcessServiceName returns the systemd service name for an extra process instance.
func ExtraProcessServiceName(projectID uint, procName string, instance int) string {
	// Sanitize name: lowercase, replace spaces with dashes
	safe := strings.ToLower(strings.ReplaceAll(procName, " ", "-"))
	return fmt.Sprintf("pdai-project-%d-%s-%d", projectID, safe, instance)
}

// InstallExtraProcess creates and enables systemd services for an extra process (one per instance).
func (pm *ProcessManager) InstallExtraProcess(project *Project, proc *ExtraProcess, workDir string) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(project.ID, proc.Name, i)
		unitPath := serviceFilePath(svcName)

		var envLines []string
		for _, ev := range project.EnvVarList {
			envLines = append(envLines, fmt.Sprintf("%s=%s", ev.Key, ev.Value))
		}

		runtimeLog := filepath.Join(pm.logDir, fmt.Sprintf("project_%d", project.ID), fmt.Sprintf("proc_%s_%d.log", proc.Name, i))
		os.MkdirAll(filepath.Dir(runtimeLog), 0755)

		data := struct {
			Name         string
			WorkDir      string
			StartCommand string
			EnvLines     []string
			LogFile      string
			MemoryMax    string
			CPUQuota     string
		}{
			Name:         fmt.Sprintf("%s - %s (#%d)", project.Name, proc.Name, i),
			WorkDir:      workDir,
			StartCommand: resolveStartCommand(proc.Command, workDir),
			EnvLines:     envLines,
			LogFile:      runtimeLog,
		}

		if usesOpenRC() {
			openRCData := struct {
				Description  string
				ServiceName  string
				WorkDir      string
				StartCommand string
				EnvLines     []string
			}{
				Description:  shellSingleQuote(fmt.Sprintf("%s - %s (#%d)", project.Name, proc.Name, i)),
				ServiceName:  svcName,
				WorkDir:      shellSingleQuote(workDir),
				StartCommand: shellSingleQuote("exec " + data.StartCommand),
				EnvLines:     openRCEnvLines(project.EnvVarList, 0),
			}
			f, err := os.Create(unitPath)
			if err != nil {
				return fmt.Errorf("create extra process OpenRC service file: %w", err)
			}
			if err := openRCSvcTmpl.Execute(f, openRCData); err != nil {
				f.Close()
				return fmt.Errorf("render extra process OpenRC service template: %w", err)
			}
			f.Close()
			if err := os.Chmod(unitPath, 0755); err != nil {
				return fmt.Errorf("chmod extra process OpenRC service file: %w", err)
			}
		} else {
			f, err := os.Create(unitPath)
			if err != nil {
				return fmt.Errorf("create extra process service file: %w", err)
			}
			if err := svcTmpl.Execute(f, data); err != nil {
				f.Close()
				return fmt.Errorf("render extra process service template: %w", err)
			}
			f.Close()
		}
	}

	if err := reloadServiceManager(); err != nil {
		return err
	}
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(project.ID, proc.Name, i)
		if err := enableService(svcName); err != nil {
			return err
		}
	}
	return nil
}

// StartExtraProcess starts all instances of an extra process.
func (pm *ProcessManager) StartExtraProcess(projectID uint, proc *ExtraProcess) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		if err := startService(svcName); err != nil {
			return err
		}
	}
	return nil
}

// StopExtraProcess stops all instances of an extra process.
func (pm *ProcessManager) StopExtraProcess(projectID uint, proc *ExtraProcess) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		if err := stopService(svcName); err != nil {
			return err
		}
	}
	return nil
}

// RestartExtraProcess restarts all instances of an extra process.
func (pm *ProcessManager) RestartExtraProcess(projectID uint, proc *ExtraProcess) error {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		if err := restartService(svcName); err != nil {
			return err
		}
	}
	return nil
}

// UninstallExtraProcess stops, disables, and removes all instances of an extra process.
func (pm *ProcessManager) UninstallExtraProcess(projectID uint, proc *ExtraProcess) {
	for i := 1; i <= proc.Instances; i++ {
		svcName := ExtraProcessServiceName(projectID, proc.Name, i)
		stopService(svcName)
		disableService(svcName)
		unitPath := serviceFilePath(svcName)
		os.Remove(unitPath)
	}
	reloadServiceManager()
}

// IsExtraProcessRunning checks if the first instance of an extra process is active.
func (pm *ProcessManager) IsExtraProcessRunning(projectID uint, proc *ExtraProcess) bool {
	svcName := ExtraProcessServiceName(projectID, proc.Name, 1)
	return isServiceRunning(svcName)
}

// resolveStartCommand makes relative paths absolute.
func resolveStartCommand(cmd, workDir string) string {
	if strings.HasPrefix(cmd, "./") {
		return filepath.Join(workDir, cmd[2:])
	}
	return cmd
}
