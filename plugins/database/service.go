package database

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	instanceSourceLocal  = "local"
	instanceSourceRemote = "remote"
)

// Service implements the business logic for database instance management.
type Service struct {
	db      *gorm.DB
	dataDir string
	logger  *slog.Logger
}

// NewService creates a database Service.
func NewService(db *gorm.DB, dataDir string, logger *slog.Logger) *Service {
	return &Service{db: db, dataDir: dataDir, logger: logger}
}

// ── Instance CRUD ──

// ListInstances returns all instances with live status.
func (s *Service) ListInstances() ([]Instance, error) {
	var instances []Instance
	if err := s.db.Order("id ASC").Find(&instances).Error; err != nil {
		return nil, err
	}
	for i := range instances {
		instances[i].Status = s.resolveInstanceStatus(&instances[i])
	}
	return instances, nil
}

// GetInstance returns a single instance with live status.
func (s *Service) GetInstance(id uint) (*Instance, error) {
	var inst Instance
	if err := s.db.First(&inst, id).Error; err != nil {
		return nil, err
	}
	inst.Status = s.resolveInstanceStatus(&inst)
	return &inst, nil
}

// CreateInstance creates a new database instance and optionally starts it.
func (s *Service) CreateInstance(req *CreateInstanceRequest) (*Instance, error) {
	// Validate engine.
	engineInfo := findEngine(req.Engine)
	if engineInfo == nil {
		return nil, fmt.Errorf("unsupported engine: %s", req.Engine)
	}

	// Require password for non-Redis engines.
	if req.Engine != EngineRedis && req.RootPassword == "" {
		return nil, fmt.Errorf("root_password is required for %s", req.Engine)
	}

	// Default version.
	version := req.Version
	if version == "" {
		version = engineInfo.Default
	}
	source := normalizeInstanceSource(req.Source)

	// Check name uniqueness.
	var count int64
	s.db.Model(&Instance{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("instance name %q already exists", req.Name)
	}

	// Validate that sanitized name is meaningful (not all special chars).
	safeName := sanitizeName(req.Name)
	if safeName == "unnamed" {
		return nil, fmt.Errorf("instance name must contain at least one letter or digit")
	}

	if source == instanceSourceLocal {
		// Check slug collision: different names like "Foo!" and "Foo?" map to the same
		// container name and data directory, causing cross-interference.
		var existingInstances []Instance
		s.db.Select("name").Find(&existingInstances)
		for _, ex := range existingInstances {
			if sanitizeName(ex.Name) == safeName {
				return nil, fmt.Errorf("instance name %q conflicts with existing instance %q (same container name)", req.Name, ex.Name)
			}
		}
	}

	// Allocate port — default to engine's standard port.
	port := req.Port
	if port == 0 {
		port = engineInfo.Port
	}
	// Validate port range.
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1024 and 65535, got %d", port)
	}
	if source == instanceSourceLocal {
		// Check for port conflicts with existing instances.
		var portConflict int64
		s.db.Model(&Instance{}).Where("source <> ? AND port = ?", instanceSourceRemote, port).Count(&portConflict)
		if portConflict > 0 {
			return nil, fmt.Errorf("port %d is already in use by another instance", port)
		}
	}

	// Memory limit — default 0.5g.
	memLimit := req.MemoryLimit
	if memLimit == "" {
		memLimit = "0.5g"
	}

	containerName := "pdai-db-" + safeName
	host := strings.TrimSpace(req.Host)
	if source == instanceSourceRemote {
		if host == "" {
			return nil, fmt.Errorf("host is required for remote database")
		}
		containerName = ""
	}

	// Apply postgres workload preset (no-op for other engines or empty preset).
	cfg, presetID, err := resolveTuningPreset(req, memLimit)
	if err != nil {
		return nil, err
	}

	// Serialize engine config to JSON.
	var configJSON string
	if cfg != nil {
		if data, err := json.Marshal(cfg); err == nil {
			configJSON = string(data)
		}
	}

	inst := &Instance{
		Name:          req.Name,
		Engine:        req.Engine,
		Version:       version,
		Status:        "stopped",
		Source:        source,
		Host:          host,
		Port:          port,
		Username:      strings.TrimSpace(req.Username),
		RootPassword:  req.RootPassword,
		SSLMode:       normalizeSSLMode(req.SSLMode),
		DataDir:       "",
		ContainerName: containerName,
		MemoryLimit:   memLimit,
		TuningPreset:  presetID,
		Config:        configJSON,
	}
	if source == instanceSourceLocal {
		inst.DataDir = filepath.Join(s.dataDir, "instances", sanitizeName(req.Name))
	}

	if source == instanceSourceRemote {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := NewDBClient().Ping(ctx, inst); err != nil {
			return nil, fmt.Errorf("remote database connection failed: %w", err)
		}
		inst.Status = "running"
		if err := s.db.Create(inst).Error; err != nil {
			return nil, fmt.Errorf("create instance record: %w", err)
		}
		return s.GetInstance(inst.ID)
	}

	// Create data directory.
	if err := os.MkdirAll(inst.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create instance dir: %w", err)
	}

	// Generate and write compose file.
	composeContent := GenerateComposeFile(inst)
	composePath := filepath.Join(inst.DataDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0600); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	// Write .env with root password.
	envContent := fmt.Sprintf("ROOT_PASSWORD=%s\n", inst.RootPassword)
	envPath := filepath.Join(inst.DataDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return nil, fmt.Errorf("write env file: %w", err)
	}

	// Save to DB.
	if err := s.db.Create(inst).Error; err != nil {
		return nil, fmt.Errorf("create instance record: %w", err)
	}

	// Auto-start if requested.
	if req.AutoStart {
		if err := s.startInstance(inst); err != nil {
			// Rollback: remove DB record and data dir.
			s.db.Where("instance_id = ?", inst.ID).Delete(&Database{})
			s.db.Where("instance_id = ?", inst.ID).Delete(&DatabaseUser{})
			s.db.Delete(&Instance{}, inst.ID)
			os.RemoveAll(inst.DataDir)
			return nil, fmt.Errorf("auto-start failed: %w", err)
		}
	}

	return s.GetInstance(inst.ID)
}

// CreateInstanceStream creates a new database instance with progress callback for streaming output.
func (s *Service) CreateInstanceStream(req *CreateInstanceRequest, progressCb func(string)) (*Instance, error) {
	// Validate engine.
	engineInfo := findEngine(req.Engine)
	if engineInfo == nil {
		return nil, fmt.Errorf("unsupported engine: %s", req.Engine)
	}

	if req.Engine != EngineRedis && req.RootPassword == "" {
		return nil, fmt.Errorf("root_password is required for %s", req.Engine)
	}

	version := req.Version
	if version == "" {
		version = engineInfo.Default
	}

	if normalizeInstanceSource(req.Source) == instanceSourceRemote {
		progressCb("Testing remote database connection...")
		inst, err := s.CreateInstance(req)
		if err != nil {
			return nil, err
		}
		progressCb("Remote database registered.")
		return inst, nil
	}

	var count int64
	s.db.Model(&Instance{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("instance name %q already exists", req.Name)
	}

	safeName := sanitizeName(req.Name)
	if safeName == "unnamed" {
		return nil, fmt.Errorf("instance name must contain at least one letter or digit")
	}

	// Check slug collision: different names mapping to the same container/directory.
	var existingInstances []Instance
	s.db.Select("name").Find(&existingInstances)
	for _, ex := range existingInstances {
		if sanitizeName(ex.Name) == safeName {
			return nil, fmt.Errorf("instance name %q conflicts with existing instance %q (same container name)", req.Name, ex.Name)
		}
	}

	port := req.Port
	if port == 0 {
		port = engineInfo.Port
	}
	// Validate port range.
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1024 and 65535, got %d", port)
	}
	// Check for port conflicts with existing instances.
	var portConflict int64
	s.db.Model(&Instance{}).Where("port = ?", port).Count(&portConflict)
	if portConflict > 0 {
		return nil, fmt.Errorf("port %d is already in use by another instance", port)
	}

	memLimit := req.MemoryLimit
	if memLimit == "" {
		memLimit = "0.5g"
	}

	containerName := "pdai-db-" + safeName

	cfg, presetID, err := resolveTuningPreset(req, memLimit)
	if err != nil {
		return nil, err
	}

	var configJSON string
	if cfg != nil {
		if data, err := json.Marshal(cfg); err == nil {
			configJSON = string(data)
		}
	}

	inst := &Instance{
		Name:          req.Name,
		Engine:        req.Engine,
		Version:       version,
		Status:        "stopped",
		Port:          port,
		RootPassword:  req.RootPassword,
		DataDir:       filepath.Join(s.dataDir, "instances", sanitizeName(req.Name)),
		ContainerName: containerName,
		MemoryLimit:   memLimit,
		TuningPreset:  presetID,
		Config:        configJSON,
	}

	progressCb("Preparing instance directory...")
	if err := os.MkdirAll(inst.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create instance dir: %w", err)
	}

	composeContent := GenerateComposeFile(inst)
	composePath := filepath.Join(inst.DataDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0600); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	envContent := fmt.Sprintf("ROOT_PASSWORD=%s\n", inst.RootPassword)
	envPath := filepath.Join(inst.DataDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return nil, fmt.Errorf("write env file: %w", err)
	}

	progressCb("Saving instance record...")
	if err := s.db.Create(inst).Error; err != nil {
		return nil, fmt.Errorf("create instance record: %w", err)
	}

	if req.AutoStart {
		progressCb("Starting instance (pulling image if needed)...")
		if err := s.runComposeStream(inst.DataDir, progressCb, "up", "-d", "--remove-orphans"); err != nil {
			// Rollback: remove DB record and data dir.
			s.db.Where("instance_id = ?", inst.ID).Delete(&Database{})
			s.db.Where("instance_id = ?", inst.ID).Delete(&DatabaseUser{})
			s.db.Delete(&Instance{}, inst.ID)
			os.RemoveAll(inst.DataDir)
			return nil, fmt.Errorf("auto-start failed: %w", err)
		}
	}

	return s.GetInstance(inst.ID)
}

// runComposeStream executes a docker compose command and streams output line-by-line via callback.
func (s *Service) runComposeStream(dir string, cb func(string), args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker compose %s: %w", args[0], err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		cb(scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker compose %s failed: %w", args[0], err)
	}
	return nil
}

// DeleteInstance stops and removes an instance.
func (s *Service) DeleteInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}

	if inst.IsRemote() {
		s.db.Where("instance_id = ?", id).Delete(&Database{})
		s.db.Where("instance_id = ?", id).Delete(&DatabaseUser{})
		return s.db.Delete(&Instance{}, id).Error
	}

	// Stop and remove containers + volumes — if this fails, keep the instance visible.
	if err := s.runCompose(inst.DataDir, "down", "--volumes", "--remove-orphans"); err != nil {
		s.logger.Error("compose down failed", "instance", inst.Name, "err", err)
		return fmt.Errorf("failed to stop instance: %w (instance kept for manual cleanup)", err)
	}

	// Remove data directory.
	os.RemoveAll(inst.DataDir)

	// Delete related records.
	s.db.Where("instance_id = ?", id).Delete(&Database{})
	s.db.Where("instance_id = ?", id).Delete(&DatabaseUser{})

	return s.db.Delete(&Instance{}, id).Error
}

// ── Instance Lifecycle ──

// StartInstance starts the instance.
func (s *Service) StartInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	if inst.IsRemote() {
		return s.TestConnection(id)
	}
	return s.startInstance(inst)
}

func (s *Service) startInstance(inst *Instance) error {
	return s.runCompose(inst.DataDir, "up", "-d", "--remove-orphans")
}

// StopInstance stops the instance.
func (s *Service) StopInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	if inst.IsRemote() {
		return fmt.Errorf("remote database lifecycle is managed outside the panel")
	}
	return s.runCompose(inst.DataDir, "down")
}

// RestartInstance restarts the instance.
func (s *Service) RestartInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	if inst.IsRemote() {
		return fmt.Errorf("remote database lifecycle is managed outside the panel")
	}
	return s.runCompose(inst.DataDir, "restart")
}

// InstanceLogs returns recent logs.
func (s *Service) InstanceLogs(id uint, tail string) (string, error) {
	inst, err := s.GetInstance(id)
	if err != nil {
		return "", err
	}
	if inst.IsRemote() {
		return "", fmt.Errorf("logs are only available for local container instances")
	}
	if tail == "" {
		tail = "200"
	}
	return s.runComposeOutput(inst.DataDir, "logs", "--tail", tail, "--no-color")
}

// InstanceLogsFollow starts a streaming log process.
func (s *Service) InstanceLogsFollow(ctx context.Context, id uint, tail string) (io.ReadCloser, error) {
	inst, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	if inst.IsRemote() {
		return nil, fmt.Errorf("logs are only available for local container instances")
	}
	if tail == "" {
		tail = "100"
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--follow", "--tail", tail, "--no-color")
	cmd.Dir = inst.DataDir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(inst.DataDir))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() { cmd.Wait() }()
	return stdout, nil
}

// ── Connection Info ──

// GetConnectionInfo generates connection strings for an instance.
func (s *Service) GetConnectionInfo(id uint) (*ConnectionInfo, error) {
	inst, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}

	info := &ConnectionInfo{
		Host:           inst.DBHost(),
		Port:           inst.Port,
		DockerInternal: "",
	}
	if !inst.IsRemote() {
		info.Host = "localhost"
		info.DockerInternal = fmt.Sprintf("%s:%d", inst.ContainerName, defaultInternalPort(inst.Engine))
	}

	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		info.Username = inst.DBUsername()
		info.ConnectionURI = fmt.Sprintf("mysql://%s@%s:%d/", info.Username, info.Host, inst.Port)
		info.CLICommand = fmt.Sprintf("mysql -h %s -P %d -u %s -p", info.Host, inst.Port, info.Username)
		info.EnvVar = fmt.Sprintf("DATABASE_URL=mysql://%s:PASSWORD@%s:%d/dbname", info.Username, info.Host, inst.Port)
	case EnginePostgres:
		info.Username = inst.DBUsername()
		info.ConnectionURI = fmt.Sprintf("postgresql://%s@%s:%d/", info.Username, info.Host, inst.Port)
		info.CLICommand = fmt.Sprintf("psql -h %s -p %d -U %s", info.Host, inst.Port, info.Username)
		info.EnvVar = fmt.Sprintf("DATABASE_URL=postgresql://%s:PASSWORD@%s:%d/dbname", info.Username, info.Host, inst.Port)
	case EngineRedis:
		info.Username = ""
		info.ConnectionURI = fmt.Sprintf("redis://:%s@%s:%d/0", "PASSWORD", info.Host, inst.Port)
		info.CLICommand = fmt.Sprintf("redis-cli -h %s -p %d -a PASSWORD", info.Host, inst.Port)
		info.EnvVar = fmt.Sprintf("REDIS_URL=redis://:PASSWORD@%s:%d/0", info.Host, inst.Port)
	}

	return info, nil
}

// GetRootPassword returns the root password for an instance.
func (s *Service) GetRootPassword(id uint) (string, error) {
	var inst Instance
	if err := s.db.First(&inst, id).Error; err != nil {
		return "", err
	}
	return inst.RootPassword, nil
}

func (s *Service) TestConnection(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return NewDBClient().Ping(ctx, inst)
}

// ListBackups returns local backup files recorded for an instance.
func (s *Service) ListBackups(instanceID uint) ([]DatabaseBackup, error) {
	if _, err := s.GetInstance(instanceID); err != nil {
		return nil, err
	}
	var backups []DatabaseBackup
	if err := s.db.Where("instance_id = ?", instanceID).Order("created_at DESC").Find(&backups).Error; err != nil {
		return nil, err
	}
	return backups, nil
}

// CreateBackup creates a logical SQL dump for an instance.
func (s *Service) CreateBackup(instanceID uint) (*DatabaseBackup, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}
	if inst.Engine == EngineRedis {
		return nil, fmt.Errorf("Redis backup is not supported yet")
	}

	if err := s.TestConnection(instanceID); err != nil {
		return nil, fmt.Errorf("connection check failed: %w", err)
	}

	backupDir := filepath.Join(s.dataDir, "backups", "database", fmt.Sprintf("%d-%s", inst.ID, sanitizeName(inst.Name)))
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}
	fileName := fmt.Sprintf("%s-%s.sql", sanitizeName(inst.Name), time.Now().Format("20060102150405"))
	filePath := filepath.Join(backupDir, fileName)

	backup := &DatabaseBackup{
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		Engine:       inst.Engine,
		Source:       inst.Source,
		FilePath:     filePath,
		FileName:     fileName,
		Status:       "running",
	}
	if err := s.db.Create(backup).Error; err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd, err := s.databaseDumpCommand(ctx, inst)
	if err != nil {
		s.db.Model(backup).Updates(map[string]interface{}{"status": "failed", "error_msg": err.Error()})
		return nil, err
	}

	outFile, err := os.Create(filePath)
	if err != nil {
		s.db.Model(backup).Updates(map[string]interface{}{"status": "failed", "error_msg": err.Error()})
		return nil, err
	}
	cmd.Stdout = outFile
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	runErr := cmd.Run()
	closeErr := outFile.Close()
	if runErr != nil {
		_ = os.Remove(filePath)
		errDetail := strings.TrimSpace(stderrBuf.String())
		if len(errDetail) > 512 {
			errDetail = errDetail[:512]
		}
		errMsg := fmt.Sprintf("%v - %s", runErr, errDetail)
		s.db.Model(backup).Updates(map[string]interface{}{"status": "failed", "error_msg": errMsg})
		return nil, fmt.Errorf("backup failed: %s", errMsg)
	}
	if closeErr != nil {
		_ = os.Remove(filePath)
		s.db.Model(backup).Updates(map[string]interface{}{"status": "failed", "error_msg": closeErr.Error()})
		return nil, closeErr
	}

	info, err := os.Stat(filePath)
	if err != nil {
		s.db.Model(backup).Updates(map[string]interface{}{"status": "failed", "error_msg": err.Error()})
		return nil, err
	}
	if err := s.db.Model(backup).Updates(map[string]interface{}{
		"status":     "completed",
		"size_bytes": info.Size(),
	}).Error; err != nil {
		return nil, err
	}
	return s.GetBackup(inst.ID, backup.ID)
}

// GetBackup returns a backup record belonging to an instance.
func (s *Service) GetBackup(instanceID, backupID uint) (*DatabaseBackup, error) {
	var backup DatabaseBackup
	if err := s.db.Where("id = ? AND instance_id = ?", backupID, instanceID).First(&backup).Error; err != nil {
		return nil, err
	}
	return &backup, nil
}

// DeleteBackup removes a backup file and its record.
func (s *Service) DeleteBackup(instanceID, backupID uint) error {
	backup, err := s.GetBackup(instanceID, backupID)
	if err != nil {
		return err
	}
	if backup.FilePath != "" {
		_ = os.Remove(backup.FilePath)
	}
	return s.db.Delete(&DatabaseBackup{}, backup.ID).Error
}

// RestoreBackup restores a logical SQL dump into the instance.
func (s *Service) RestoreBackup(instanceID, backupID uint) error {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return err
	}
	if inst.Status != "running" {
		return fmt.Errorf("instance is not running")
	}
	backup, err := s.GetBackup(instanceID, backupID)
	if err != nil {
		return err
	}
	if backup.Status != "completed" {
		return fmt.Errorf("backup is not completed")
	}
	if backup.Engine != inst.Engine {
		return fmt.Errorf("backup engine %s does not match instance engine %s", backup.Engine, inst.Engine)
	}
	if _, err := os.Stat(backup.FilePath); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}
	if err := s.TestConnection(instanceID); err != nil {
		return fmt.Errorf("connection check failed: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd, err := s.databaseRestoreCommand(ctx, inst)
	if err != nil {
		return err
	}
	inFile, err := os.Open(backup.FilePath)
	if err != nil {
		return err
	}
	defer inFile.Close()
	cmd.Stdin = inFile
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		errDetail := strings.TrimSpace(stderrBuf.String())
		if len(errDetail) > 512 {
			errDetail = errDetail[:512]
		}
		return fmt.Errorf("restore failed: %v - %s", err, errDetail)
	}
	return nil
}

func (s *Service) databaseDumpCommand(ctx context.Context, inst *Instance) (*exec.Cmd, error) {
	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		dbNames, err := mysqlDumpDatabaseNames(ctx, inst)
		if err != nil {
			return nil, err
		}
		dumpArgs := append([]string{"--single-transaction", "--routines", "--events", "--databases"}, dbNames...)
		if inst.IsRemote() {
			if cmd := s.remoteMySQLDumpCommand(ctx, inst); cmd != nil {
				return cmd, nil
			}
			args := []string{"-h", inst.DBHost(), "-P", fmt.Sprintf("%d", inst.Port), "-u", inst.DBUsername()}
			args = append(args, dumpArgs...)
			cmd := exec.CommandContext(ctx, "mysqldump", args...)
			cmd.Env = append(os.Environ(), "MYSQL_PWD="+inst.RootPassword)
			return cmd, nil
		}
		args := []string{"exec", "-e", "MYSQL_PWD=" + inst.RootPassword, inst.ContainerName, mysqlDumpBinary(inst), "-u", inst.DBUsername()}
		args = append(args, dumpArgs...)
		return exec.CommandContext(ctx, "docker", args...), nil
	case EnginePostgres:
		if inst.IsRemote() {
			if cmd := s.remotePostgresDumpCommand(ctx, inst); cmd != nil {
				return cmd, nil
			}
			cmd := exec.CommandContext(ctx, "pg_dumpall", "-h", inst.DBHost(), "-p", fmt.Sprintf("%d", inst.Port), "-U", inst.DBUsername())
			cmd.Env = append(os.Environ(), "PGPASSWORD="+inst.RootPassword, "PGSSLMODE="+inst.DBSSLMode())
			return cmd, nil
		}
		return exec.CommandContext(ctx, "docker", "exec", "-e", "PGPASSWORD="+inst.RootPassword, "-e", "PGSSLMODE="+inst.DBSSLMode(), inst.ContainerName, "pg_dumpall", "-U", inst.DBUsername()), nil
	default:
		return nil, fmt.Errorf("unsupported engine: %s", inst.Engine)
	}
}

func mysqlDumpDatabaseNames(ctx context.Context, inst *Instance) ([]string, error) {
	dbs, err := NewDBClient().ListDatabases(ctx, inst)
	if err != nil {
		return nil, fmt.Errorf("list databases for backup: %w", err)
	}
	names := make([]string, 0, len(dbs))
	for _, db := range dbs {
		name := strings.TrimSpace(db.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no user databases found to back up")
	}
	return names, nil
}

func (s *Service) databaseRestoreCommand(ctx context.Context, inst *Instance) (*exec.Cmd, error) {
	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		if inst.IsRemote() {
			if cmd := s.remoteMySQLRestoreCommand(ctx, inst); cmd != nil {
				return cmd, nil
			}
			cmd := exec.CommandContext(ctx, "mysql", "-h", inst.DBHost(), "-P", fmt.Sprintf("%d", inst.Port), "-u", inst.DBUsername())
			cmd.Env = append(os.Environ(), "MYSQL_PWD="+inst.RootPassword)
			return cmd, nil
		}
		return exec.CommandContext(ctx, "docker", "exec", "-i", "-e", "MYSQL_PWD="+inst.RootPassword, inst.ContainerName, mysqlClientBinary(inst), "-u", inst.DBUsername()), nil
	case EnginePostgres:
		if inst.IsRemote() {
			if cmd := s.remotePostgresRestoreCommand(ctx, inst); cmd != nil {
				return cmd, nil
			}
			cmd := exec.CommandContext(ctx, "psql", "-h", inst.DBHost(), "-p", fmt.Sprintf("%d", inst.Port), "-U", inst.DBUsername(), "-d", "postgres")
			cmd.Env = append(os.Environ(), "PGPASSWORD="+inst.RootPassword, "PGSSLMODE="+inst.DBSSLMode())
			return cmd, nil
		}
		return exec.CommandContext(ctx, "docker", "exec", "-i", "-e", "PGPASSWORD="+inst.RootPassword, "-e", "PGSSLMODE="+inst.DBSSLMode(), inst.ContainerName, "psql", "-U", inst.DBUsername(), "-d", "postgres"), nil
	default:
		return nil, fmt.Errorf("unsupported engine: %s", inst.Engine)
	}
}

func (s *Service) remoteMySQLDumpCommand(ctx context.Context, inst *Instance) *exec.Cmd {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	dbNames, err := mysqlDumpDatabaseNames(ctx, inst)
	if err != nil {
		return nil
	}
	host := inst.DBHost()
	args := append([]string{"run", "--rm"}, dockerNetworkArgs(host)...)
	args = append(args,
		"-e", "MYSQL_PWD="+inst.RootPassword,
		mysqlClientImage(inst),
		mysqlDumpBinary(inst),
		"-h", host,
		"-P", fmt.Sprintf("%d", inst.Port),
		"-u", inst.DBUsername(),
	)
	args = append(args, "--single-transaction", "--routines", "--events", "--databases")
	args = append(args, dbNames...)
	return exec.CommandContext(ctx, "docker", args...)
}

func (s *Service) remoteMySQLRestoreCommand(ctx context.Context, inst *Instance) *exec.Cmd {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	host := inst.DBHost()
	args := append([]string{"run", "--rm", "-i"}, dockerNetworkArgs(host)...)
	args = append(args,
		"-e", "MYSQL_PWD="+inst.RootPassword,
		mysqlClientImage(inst),
		mysqlClientBinary(inst),
		"-h", host,
		"-P", fmt.Sprintf("%d", inst.Port),
		"-u", inst.DBUsername(),
	)
	return exec.CommandContext(ctx, "docker", args...)
}

func (s *Service) remotePostgresDumpCommand(ctx context.Context, inst *Instance) *exec.Cmd {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	host := inst.DBHost()
	args := append([]string{"run", "--rm"}, dockerNetworkArgs(host)...)
	args = append(args,
		"-e", "PGPASSWORD="+inst.RootPassword,
		"-e", "PGSSLMODE="+inst.DBSSLMode(),
		postgresClientImage(inst),
		"pg_dumpall",
		"-h", host,
		"-p", fmt.Sprintf("%d", inst.Port),
		"-U", inst.DBUsername(),
	)
	return exec.CommandContext(ctx, "docker", args...)
}

func (s *Service) remotePostgresRestoreCommand(ctx context.Context, inst *Instance) *exec.Cmd {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	host := inst.DBHost()
	args := append([]string{"run", "--rm", "-i"}, dockerNetworkArgs(host)...)
	args = append(args,
		"-e", "PGPASSWORD="+inst.RootPassword,
		"-e", "PGSSLMODE="+inst.DBSSLMode(),
		postgresClientImage(inst),
		"psql",
		"-h", host,
		"-p", fmt.Sprintf("%d", inst.Port),
		"-U", inst.DBUsername(),
		"-d", "postgres",
	)
	return exec.CommandContext(ctx, "docker", args...)
}

func mysqlClientImage(inst *Instance) string {
	if inst.Engine == EngineMariaDB {
		if strings.TrimSpace(inst.Version) != "" {
			return "mariadb:" + strings.TrimSpace(inst.Version)
		}
		return "mariadb:11.8"
	}
	if strings.TrimSpace(inst.Version) != "" {
		return "mysql:" + strings.TrimSpace(inst.Version)
	}
	return "mysql:8.4"
}

func mysqlDumpBinary(inst *Instance) string {
	if inst.Engine == EngineMariaDB {
		return "mariadb-dump"
	}
	return "mysqldump"
}

func mysqlClientBinary(inst *Instance) string {
	if inst.Engine == EngineMariaDB {
		return "mariadb"
	}
	return "mysql"
}

func postgresClientImage(inst *Instance) string {
	if strings.TrimSpace(inst.Version) != "" {
		return "postgres:" + strings.TrimSpace(inst.Version) + "-alpine"
	}
	return "postgres:17-alpine"
}

func dockerNetworkArgs(host string) []string {
	if runtime.GOOS == "linux" && isLoopbackHost(host) {
		return []string{"--network", "host"}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// ExecuteQuery executes a read-only query against a running database instance.
// SQL: only SELECT, SHOW, DESCRIBE, and EXPLAIN are allowed.
// Redis: only read-only commands (GET, KEYS, INFO, etc.) are allowed.
func (s *Service) ExecuteQuery(instanceID uint, database, query string, limit int) (*QueryResult, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}

	if inst.Engine == EngineRedis {
		// Redis: validate command is read-only.
		if !isReadOnlyRedisCommand(query) {
			return nil, fmt.Errorf("only read-only Redis commands are allowed (GET, MGET, KEYS, SCAN, INFO, TTL, TYPE, EXISTS, DBSIZE, LRANGE, SCARD, SMEMBERS, HGETALL, HGET, LLEN, ZRANGE, ZCARD)")
		}
	} else {
		// SQL: validate query is read-only.
		if !isReadOnlyQuery(query) {
			return nil, fmt.Errorf("only SELECT, SHOW, DESCRIBE, and EXPLAIN statements are allowed")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewDBClient()
	return client.ExecuteQuery(ctx, inst, database, query, limit)
}

// isReadOnlyRedisCommand checks if a Redis command is read-only.
func isReadOnlyRedisCommand(cmd string) bool {
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) == 0 {
		return false
	}
	readOnly := map[string]bool{
		"GET": true, "MGET": true, "KEYS": true, "SCAN": true,
		"INFO": true, "TTL": true, "PTTL": true, "TYPE": true,
		"EXISTS": true, "DBSIZE": true, "RANDOMKEY": true,
		"LRANGE": true, "LLEN": true, "LINDEX": true,
		"SCARD": true, "SMEMBERS": true, "SISMEMBER": true,
		"HGET": true, "HGETALL": true, "HLEN": true, "HKEYS": true, "HVALS": true,
		"ZRANGE": true, "ZCARD": true, "ZSCORE": true, "ZRANK": true,
		"STRLEN": true, "PING": true, "ECHO": true, "TIME": true,
		"SELECT": true,
	}
	return readOnly[strings.ToUpper(parts[0])]
}

// isReadOnlyQuery checks if a SQL query is a read-only, single statement.
func isReadOnlyQuery(query string) bool {
	// Normalize: trim whitespace and get the first keyword
	normalized := strings.TrimSpace(query)
	// Remove leading comments (single-line and multi-line)
	for {
		if strings.HasPrefix(normalized, "--") {
			if idx := strings.Index(normalized, "\n"); idx >= 0 {
				normalized = strings.TrimSpace(normalized[idx+1:])
				continue
			}
			return false // comment-only query
		}
		if strings.HasPrefix(normalized, "/*") {
			if idx := strings.Index(normalized, "*/"); idx >= 0 {
				normalized = strings.TrimSpace(normalized[idx+2:])
				continue
			}
			return false // unclosed comment
		}
		break
	}

	// Reject stacked queries: look for semicolons outside quoted strings.
	body := strings.TrimRight(normalized, "; \t\n\r")
	if containsUnquotedSemicolon(body) {
		return false
	}

	upper := strings.ToUpper(normalized)

	// Reject SELECT INTO (write side-effects via single statement).
	if strings.Contains(upper, " INTO ") && (strings.Contains(upper, "OUTFILE") || strings.Contains(upper, "DUMPFILE") || strings.Contains(upper, "TEMP ") || strings.Contains(upper, "TEMPORARY ")) {
		return false
	}
	allowedPrefixes := []string{"SELECT ", "SELECT\t", "SELECT\n",
		"SHOW ", "SHOW\t", "SHOW\n",
		"DESCRIBE ", "DESCRIBE\t", "DESCRIBE\n",
		"DESC ", "DESC\t", "DESC\n",
		"EXPLAIN ", "EXPLAIN\t", "EXPLAIN\n"}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			// EXPLAIN ANALYZE executes the underlying statement (DELETE/UPDATE/INSERT)
			// on PostgreSQL and MySQL 8+, so it is NOT read-only.
			if strings.HasPrefix(upper, "EXPLAIN") && strings.Contains(upper, "ANALYZE") {
				return false
			}
			return true
		}
	}
	return false
}

// containsUnquotedSemicolon checks whether s has a semicolon that is NOT
// inside a single-quoted SQL string literal. This avoids false positives
// for queries like SELECT ';' AS s while still blocking stacked statements.
func containsUnquotedSemicolon(s string) bool {
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			if inQuote && i+1 < len(s) && s[i+1] == '\'' {
				i++ // escaped quote '' — skip both
				continue
			}
			inQuote = !inQuote
			continue
		}
		if ch == '\\' && inQuote {
			i++ // skip escaped character inside string
			continue
		}
		if ch == ';' && !inQuote {
			return true
		}
	}
	return false
}

// ── Database CRUD ──

// ListDatabases returns databases for an instance.
func (s *Service) ListDatabases(instanceID uint) ([]Database, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status == "running" && inst.Engine != EngineRedis {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if dbs, err := NewDBClient().ListDatabases(ctx, inst); err == nil {
			for i := range dbs {
				dbs[i].InstanceID = instanceID
			}
			return dbs, nil
		} else if inst.IsRemote() {
			return nil, err
		}
	}
	var dbs []Database
	if err := s.db.Where("instance_id = ?", instanceID).Order("id ASC").Find(&dbs).Error; err != nil {
		return nil, err
	}
	return dbs, nil
}

// CreateDatabase creates a logical database in the instance.
func (s *Service) CreateDatabase(instanceID uint, req *CreateDatabaseRequest) (*Database, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}
	if inst.Engine == EngineRedis {
		return nil, fmt.Errorf("Redis does not support named databases")
	}

	charset := req.Charset
	if charset == "" {
		if inst.Engine == EnginePostgres {
			charset = "UTF8"
		} else {
			charset = "utf8mb4"
		}
	}

	// Validate database name matches the same pattern used by the SQL query executor,
	// so databases created here can always be queried later.
	if !validDBNameRe.MatchString(req.Name) {
		return nil, fmt.Errorf("invalid database name %q: must match [a-zA-Z_][a-zA-Z0-9_-]*", req.Name)
	}

	client := NewDBClient()
	if err := client.CreateDatabase(inst, req.Name, charset); err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}

	db := &Database{
		InstanceID: instanceID,
		Name:       req.Name,
		Charset:    charset,
	}
	if err := s.db.Create(db).Error; err != nil {
		return nil, err
	}
	return db, nil
}

// DeleteDatabase drops a logical database.
func (s *Service) DeleteDatabase(instanceID uint, dbName string) error {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return err
	}
	if inst.Status != "running" {
		return fmt.Errorf("instance is not running")
	}

	client := NewDBClient()
	if err := client.DropDatabase(inst, dbName); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}

	return s.db.Where("instance_id = ? AND name = ?", instanceID, dbName).Delete(&Database{}).Error
}

// ── User CRUD ──

// ListUsers returns database users for an instance.
func (s *Service) ListUsers(instanceID uint) ([]DatabaseUser, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status == "running" && inst.Engine != EngineRedis {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if users, err := NewDBClient().ListUsers(ctx, inst); err == nil {
			for i := range users {
				users[i].InstanceID = instanceID
			}
			return users, nil
		} else if inst.IsRemote() {
			return nil, err
		}
	}
	var users []DatabaseUser
	if err := s.db.Where("instance_id = ?", instanceID).Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// CreateUser creates a database user and grants access.
func (s *Service) CreateUser(instanceID uint, req *CreateUserRequest) (*DatabaseUser, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}
	if inst.Engine == EngineRedis {
		return nil, fmt.Errorf("Redis does not support user management via this interface")
	}

	client := NewDBClient()
	if err := client.CreateUser(inst, req.Username, req.Password); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Grant access to specified databases.
	var grantErrors []string
	for _, dbName := range req.Databases {
		if err := client.GrantAll(inst, req.Username, dbName); err != nil {
			grantErrors = append(grantErrors, fmt.Sprintf("%s: %v", dbName, err))
		}
	}
	if len(grantErrors) > 0 {
		// Rollback: drop the user we just created since grants failed.
		_ = client.DropUser(inst, req.Username)
		return nil, fmt.Errorf("grant failed for databases: %s", strings.Join(grantErrors, "; "))
	}

	user := &DatabaseUser{
		InstanceID: instanceID,
		Username:   req.Username,
		Host:       "%",
	}
	if err := s.db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// DeleteUser drops a database user.
func (s *Service) DeleteUser(instanceID uint, username string) error {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return err
	}
	if inst.Status != "running" {
		return fmt.Errorf("instance is not running")
	}

	client := NewDBClient()
	if err := client.DropUser(inst, username); err != nil {
		return fmt.Errorf("drop user: %w", err)
	}

	return s.db.Where("instance_id = ? AND username = ?", instanceID, username).Delete(&DatabaseUser{}).Error
}

// ── Helpers ──

// allocatePort finds the next available port for the engine.
func (s *Service) allocatePort(engine EngineType) (int, error) {
	portRange := enginePortRange(engine)
	if portRange[0] == 0 {
		return 0, fmt.Errorf("unknown engine port range for %s", engine)
	}

	// Get all used ports.
	var usedPorts []int
	s.db.Model(&Instance{}).Pluck("port", &usedPorts)
	usedSet := make(map[int]bool, len(usedPorts))
	for _, p := range usedPorts {
		usedSet[p] = true
	}

	for port := portRange[0]; port <= portRange[1]; port++ {
		if !usedSet[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports for %s (range %d-%d)", engine, portRange[0], portRange[1])
}

func enginePortRange(engine EngineType) [2]int {
	switch engine {
	case EngineMySQL:
		return [2]int{13306, 13399}
	case EnginePostgres:
		return [2]int{15432, 15499}
	case EngineMariaDB:
		return [2]int{13400, 13499}
	case EngineRedis:
		return [2]int{16379, 16399}
	default:
		return [2]int{0, 0}
	}
}

func defaultInternalPort(engine EngineType) int {
	switch engine {
	case EngineMySQL, EngineMariaDB:
		return 3306
	case EnginePostgres:
		return 5432
	case EngineRedis:
		return 6379
	default:
		return 0
	}
}

func findEngine(engine EngineType) *EngineInfo {
	for _, e := range SupportedEngines {
		if e.Engine == engine {
			return &e
		}
	}
	return nil
}

// resolveInstanceStatus checks Docker for actual container state.
func (s *Service) resolveInstanceStatus(inst *Instance) string {
	if inst.IsRemote() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := NewDBClient().Ping(ctx, inst); err != nil {
			return "stopped"
		}
		return "running"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Status}}", inst.ContainerName)
	output, err := cmd.Output()
	if err != nil {
		return "stopped"
	}
	status := strings.TrimSpace(string(output))
	if status == "running" {
		return "running"
	}
	return "stopped"
}

func normalizeInstanceSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case instanceSourceRemote:
		return instanceSourceRemote
	default:
		return instanceSourceLocal
	}
}

func normalizeSSLMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "require", "verify-ca", "verify-full":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "disable"
	}
}

// runCompose executes a docker compose command in the given directory.
func (s *Service) runCompose(dir string, args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("docker compose failed",
			"dir", dir,
			"args", strings.Join(args, " "),
			"output", string(output),
			"err", err,
		)
		return fmt.Errorf("docker compose %s: %s", args[0], strings.TrimSpace(string(output)))
	}
	return nil
}

// runComposeOutput executes a compose command and returns stdout.
func (s *Service) runComposeOutput(dir string, args ...string) (string, error) {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose %s: %s", args[0], strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// resolveTuningPreset returns the EngineConfig + recorded preset ID for an
// instance creation request. For PostgreSQL with a non-empty TuningPreset,
// the preset is applied against the memory budget and overrides any
// user-supplied Config. For other engines or empty preset, the user-supplied
// Config (if any) is returned unchanged with an empty preset ID.
func resolveTuningPreset(req *CreateInstanceRequest, memLimit string) (*EngineConfig, string, error) {
	preset := strings.TrimSpace(req.TuningPreset)
	if preset == "" {
		return req.Config, "", nil
	}
	if req.Engine != EnginePostgres {
		// Silently ignore preset for non-postgres engines (forward compat: a
		// future MySQL/Redis preset set could populate this branch instead).
		return req.Config, "", nil
	}
	if !IsValidPostgresPreset(preset) {
		return nil, "", fmt.Errorf("unknown postgres tuning preset %q (valid: oltp, olap, tiny, crit)", preset)
	}
	cfg, err := ApplyPostgresPreset(PostgresTuningPreset(preset), memLimit, req.Config)
	if err != nil {
		return nil, "", err
	}
	return cfg, preset, nil
}

// sanitizeName converts a name to a filesystem-safe string.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "unnamed"
	}
	return name
}
