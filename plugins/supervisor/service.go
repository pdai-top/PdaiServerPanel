package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pdai/pdai/internal/execx"
	"gorm.io/gorm"
)

type Service struct {
	db      *gorm.DB
	logger  *slog.Logger
	mu      sync.Mutex
	running map[uint]*runningProcess
	closing bool
}

type runningProcess struct {
	cancel        context.CancelFunc
	cmd           *exec.Cmd
	manualStop    bool
	restartCount  int
	lastStartTime time.Time
}

func NewService(db *gorm.DB, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger, running: make(map[uint]*runningProcess)}
}

func (s *Service) Start() {
	var items []Process
	if err := s.db.Where("enabled = ? AND autostart = ?", true, true).Find(&items).Error; err != nil {
		s.logger.Error("load supervisor autostart processes", "err", err)
		return
	}
	for _, item := range items {
		proc := item
		go func() {
			if err := s.StartProcess(proc.ID); err != nil {
				s.logger.Error("autostart supervisor process", "id", proc.ID, "err", err)
			}
		}()
	}
}

func (s *Service) Stop() {
	s.mu.Lock()
	s.closing = true
	ids := make([]uint, 0, len(s.running))
	for id := range s.running {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	for _, id := range ids {
		if err := s.StopProcess(id); err != nil {
			s.logger.Warn("stop supervisor process", "id", id, "err", err)
		}
	}
}

func (s *Service) ListProcesses() ([]Process, error) {
	var items []Process
	return items, s.db.Order("id ASC").Find(&items).Error
}

func (s *Service) GetProcess(id uint) (*Process, error) {
	var item Process
	if err := s.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) CreateProcess(req *ProcessRequest) (*Process, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	item := Process{
		Name:            strings.TrimSpace(req.Name),
		Command:         strings.TrimSpace(req.Command),
		WorkingDir:      strings.TrimSpace(req.WorkingDir),
		Env:             strings.TrimSpace(req.Env),
		Enabled:         boolDefault(req.Enabled, true),
		Autostart:       boolDefault(req.Autostart, true),
		Autorestart:     boolDefault(req.Autorestart, true),
		RestartDelaySec: intDefault(req.RestartDelaySec, 3),
		StopTimeoutSec:  intDefault(req.StopTimeoutSec, 10),
		MaxRestarts:     intDefault(req.MaxRestarts, 10),
		Status:          StatusStopped,
	}
	if err := s.db.Create(&item).Error; err != nil {
		return nil, err
	}
	s.writeLog(item.ID, item.Name, StreamSystem, "process created")
	return &item, nil
}

func (s *Service) UpdateProcess(id uint, req *ProcessRequest) (*Process, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	item, err := s.GetProcess(id)
	if err != nil {
		return nil, err
	}
	item.Name = strings.TrimSpace(req.Name)
	item.Command = strings.TrimSpace(req.Command)
	item.WorkingDir = strings.TrimSpace(req.WorkingDir)
	item.Env = strings.TrimSpace(req.Env)
	item.Enabled = boolDefault(req.Enabled, true)
	item.Autostart = boolDefault(req.Autostart, true)
	item.Autorestart = boolDefault(req.Autorestart, true)
	item.RestartDelaySec = intDefault(req.RestartDelaySec, 3)
	item.StopTimeoutSec = intDefault(req.StopTimeoutSec, 10)
	item.MaxRestarts = intDefault(req.MaxRestarts, 10)
	if err := s.db.Save(item).Error; err != nil {
		return nil, err
	}
	s.writeLog(item.ID, item.Name, StreamSystem, "process updated")
	return item, nil
}

func (s *Service) DeleteProcess(id uint) error {
	if err := s.StopProcess(id); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.logger.Warn("stop process before delete", "id", id, "err", err)
	}
	var item Process
	if err := s.db.First(&item, id).Error; err != nil {
		return err
	}
	if err := s.db.Delete(&item).Error; err != nil {
		return err
	}
	s.db.Where("process_id = ?", id).Delete(&ProcessLog{})
	return nil
}

func (s *Service) StartProcess(id uint) error {
	item, err := s.GetProcess(id)
	if err != nil {
		return err
	}
	if !item.Enabled {
		return fmt.Errorf("process is disabled")
	}

	s.mu.Lock()
	if _, ok := s.running[id]; ok {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	if item.PID > 0 && execx.ProcessAlive(item.PID) {
		if err := s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
			"status":     StatusRunning,
			"last_error": "",
		}).Error; err != nil {
			return err
		}
		s.logger.Info("supervisor process already running", "id", id, "pid", item.PID)
		return nil
	}

	if item.PID > 0 && item.Status == StatusRunning {
		now := time.Now()
		if err := s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
			"status":          StatusStopped,
			"pid":             0,
			"restart_count":   0,
			"last_stopped_at": &now,
			"last_error":      "",
		}).Error; err != nil {
			return err
		}
	}

	s.mu.Lock()
	if _, ok := s.running[id]; ok {
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	rp := &runningProcess{cancel: cancel, restartCount: item.RestartCount, lastStartTime: time.Now()}
	s.running[id] = rp
	s.mu.Unlock()

	go s.runProcess(ctx, item, rp)
	return nil
}

func (s *Service) StopProcess(id uint) error {
	item, err := s.GetProcess(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	rp, ok := s.running[id]
	s.mu.Unlock()
	if ok {
		s.mu.Lock()
		rp.manualStop = true
		s.mu.Unlock()
		stoppingAt := time.Now()
		if err := s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
			"status":          StatusStopping,
			"last_error":      "",
			"last_stopped_at": &stoppingAt,
		}).Error; err != nil {
			return err
		}
		rp.cancel()
	} else if item.PID > 0 && execx.ProcessAlive(item.PID) {
		stoppingAt := time.Now()
		if err := s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
			"status":          StatusStopping,
			"last_error":      "",
			"last_stopped_at": &stoppingAt,
		}).Error; err != nil {
			return err
		}
		if err := execx.KillProcessGroupPID(item.PID); err != nil {
			return err
		}
		now := time.Now()
		return s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
			"status":          StatusStopped,
			"pid":             0,
			"restart_count":   0,
			"last_stopped_at": &now,
			"last_error":      "",
		}).Error
	} else {
		now := time.Now()
		return s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
			"status":          StatusStopped,
			"pid":             0,
			"restart_count":   0,
			"last_stopped_at": &now,
			"last_error":      "",
		}).Error
	}

	s.writeLog(id, item.Name, StreamSystem, "stop requested")

	deadline := time.Now().Add(time.Duration(maxInt(item.StopTimeoutSec, 1)) * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		_, stillRunning := s.running[id]
		s.mu.Unlock()
		if !stillRunning {
			now := time.Now()
			return s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
				"status":          StatusStopped,
				"pid":             0,
				"restart_count":   0,
				"last_stopped_at": &now,
				"last_error":      "",
			}).Error
		}
		time.Sleep(100 * time.Millisecond)
	}

	s.writeLog(id, item.Name, StreamSystem, fmt.Sprintf("stop timed out after %d seconds; force killing process group", maxInt(item.StopTimeoutSec, 1)))
	if err := execx.KillProcessGroup(rp.cmd); err != nil {
		s.logger.Warn("force kill supervisor process", "id", id, "err", err)
	}

	forceDeadline := time.Now().Add(execx.DefaultWaitDelay)
	for time.Now().Before(forceDeadline) {
		s.mu.Lock()
		_, stillRunning := s.running[id]
		s.mu.Unlock()
		if !stillRunning {
			now := time.Now()
			return s.db.Model(&Process{}).Where("id = ?", id).Updates(map[string]any{
				"status":          StatusStopped,
				"pid":             0,
				"restart_count":   0,
				"last_stopped_at": &now,
				"last_error":      "",
			}).Error
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("process stop timed out after %d seconds and force kill did not complete", maxInt(item.StopTimeoutSec, 1))
}

func (s *Service) RestartProcess(id uint) error {
	if err := s.StopProcess(id); err != nil {
		return err
	}
	if err := s.StartProcess(id); err != nil {
		return err
	}
	return nil
}

func (s *Service) ListLogs(processID uint, limit int) ([]ProcessLog, error) {
	var logs []ProcessLog
	q := s.db.Order("id DESC").Limit(limit)
	if processID > 0 {
		q = q.Where("process_id = ?", processID)
	}
	if err := q.Find(&logs).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs, nil
}

func (s *Service) runProcess(ctx context.Context, item *Process, rp *runningProcess) {
	defer func() {
		s.mu.Lock()
		delete(s.running, item.ID)
		closing := s.closing
		manualStop := rp.manualStop
		s.mu.Unlock()

		if !closing && !manualStop && item.Autorestart && item.Enabled {
			if item.MaxRestarts > 0 && rp.restartCount >= item.MaxRestarts {
				s.writeLog(item.ID, item.Name, StreamSystem, fmt.Sprintf("autorestart disabled after %d failures", rp.restartCount))
				return
			}
			rp.restartCount++
			s.db.Model(&Process{}).Where("id = ?", item.ID).Update("restart_count", rp.restartCount)
			time.Sleep(time.Duration(item.RestartDelaySec) * time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			rp.cancel = cancel
			rp.lastStartTime = time.Now()
			s.mu.Lock()
			if !s.closing && !rp.manualStop {
				s.running[item.ID] = rp
				go s.runProcess(ctx, item, rp)
			}
			s.mu.Unlock()
		}
	}()

	now := time.Now()
	cmd := execx.BashContext(ctx, item.Command)
	cmd.Dir = item.WorkingDir
	cmd.Env = append(os.Environ(), parseEnv(item.Env)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.markFailed(item, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.markFailed(item, err)
		return
	}

	if err := cmd.Start(); err != nil {
		s.markFailed(item, err)
		return
	}

	rp.cmd = cmd

	s.db.Model(&Process{}).Where("id = ?", item.ID).Updates(map[string]any{
		"status":          StatusRunning,
		"pid":             cmd.Process.Pid,
		"last_started_at": &now,
		"last_error":      "",
	})
	s.writeLog(item.ID, item.Name, StreamSystem, "process started pid="+strconv.Itoa(cmd.Process.Pid))

	var wg sync.WaitGroup
	wg.Add(2)
	go s.capture(&wg, item, StreamStdout, stdout)
	go s.capture(&wg, item, StreamStderr, stderr)

	err = cmd.Wait()
	wg.Wait()
	stoppedAt := time.Now()
	exitCode := exitCode(err)
	status := StatusStopped
	lastErr := ""
	if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		status = StatusFailed
		lastErr = err.Error()
	}
	s.db.Model(&Process{}).Where("id = ?", item.ID).Updates(map[string]any{
		"status":          status,
		"pid":             0,
		"exit_code":       exitCode,
		"restart_count":   0,
		"last_stopped_at": &stoppedAt,
		"last_error":      lastErr,
	})
	s.writeLog(item.ID, item.Name, StreamSystem, fmt.Sprintf("process exited code=%d", exitCode))
}

func (s *Service) capture(wg *sync.WaitGroup, item *Process, stream string, r io.Reader) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		s.writeLog(item.ID, item.Name, stream, scanner.Text())
	}
}

func (s *Service) markFailed(item *Process, err error) {
	now := time.Now()
	s.db.Model(&Process{}).Where("id = ?", item.ID).Updates(map[string]any{
		"status":          StatusFailed,
		"pid":             0,
		"last_stopped_at": &now,
		"last_error":      err.Error(),
	})
	s.writeLog(item.ID, item.Name, StreamSystem, err.Error())
}

func (s *Service) writeLog(processID uint, name, stream, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if err := s.db.Create(&ProcessLog{ProcessID: processID, Name: name, Stream: stream, Line: line}).Error; err != nil {
		s.logger.Warn("write supervisor log", "process_id", processID, "err", err)
	}
}

func validateRequest(req *ProcessRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(req.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if req.WorkingDir != "" {
		info, err := os.Stat(req.WorkingDir)
		if err != nil {
			return fmt.Errorf("working directory not found")
		}
		if !info.IsDir() {
			return fmt.Errorf("working directory is not a directory")
		}
	}
	return nil
}

func parseEnv(value string) []string {
	lines := strings.Split(value, "\n")
	env := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		env = append(env, line)
	}
	return env
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func intDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func maxInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return -1
}
