package firewall

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pdai/pdai/internal/execx"
)

var (
	portRe     = regexp.MustCompile(`^\d+(-\d+)?$`)
	protocolRe = regexp.MustCompile(`^(tcp|udp)$`)
	serviceRe  = regexp.MustCompile(`^[a-zA-Z0-9_. -]+$`)
	zoneRe     = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

const iptablesZoneName = "iptables"

// Service wraps iptables operations.
type Service struct {
	logger    *slog.Logger
	panelPort string // panel's own port to protect
}

// NewService creates a firewall Service.
func NewService(logger *slog.Logger, panelPort string) *Service {
	return &Service{logger: logger, panelPort: panelPort}
}

// Status returns the iptables status.
func (s *Service) Status() (*FirewallStatus, error) {
	status := &FirewallStatus{DefaultZone: iptablesZoneName, Zones: []string{iptablesZoneName}}

	if _, err := exec.LookPath("iptables"); err != nil {
		status.Zones = nil
		return status, nil
	}
	status.Installed = true
	status.Running = true

	if v, err := s.runCmd("--version"); err == nil {
		status.Version = firstLine(v)
	}

	return status, nil
}

// ListZones returns a single virtual iptables zone with its rules.
func (s *Service) ListZones() ([]ZoneInfo, error) {
	status, err := s.Status()
	if err != nil || !status.Running {
		return nil, fmt.Errorf("iptables is not available")
	}

	zone, err := s.GetZone(iptablesZoneName)
	if err != nil {
		return nil, err
	}
	zone.Active = true
	return []ZoneInfo{*zone}, nil
}

// GetZone returns detailed info for the virtual iptables zone.
func (s *Service) GetZone(name string) (*ZoneInfo, error) {
	if name == "" {
		name = iptablesZoneName
	}
	if !zoneRe.MatchString(name) {
		return nil, fmt.Errorf("invalid zone name")
	}
	if name != iptablesZoneName {
		return nil, fmt.Errorf("iptables does not support zones in this panel")
	}

	out, err := s.runCmd("-S", "INPUT")
	if err != nil {
		return nil, fmt.Errorf("get iptables rules: %w", err)
	}
	return s.parseIPTablesRules(out), nil
}

// AddPort adds one or more port rules.
func (s *Service) AddPort(zone, port, protocol string, comment string) error {
	ports, err := s.expandPorts(port)
	if err != nil {
		return err
	}
	protocols, err := s.expandProtocols(protocol)
	if err != nil {
		return err
	}
	if err := s.validateZone(s.resolveZone(zone)); err != nil {
		return err
	}

	for _, p := range ports {
		for _, proto := range protocols {
			if err := s.ensurePortRule(p, proto, comment); err != nil {
				return fmt.Errorf("add port %s/%s: %w", p, proto, err)
			}
		}
	}
	return nil
}

// UpdatePort edits a port rule by replacing the old rule with a new one.
func (s *Service) UpdatePort(zone, oldPort, oldProtocol, port, protocol string, comment string) error {
	if err := s.RemovePort(zone, oldPort, oldProtocol); err != nil {
		return err
	}
	if err := s.AddPort(zone, port, protocol, comment); err != nil {
		return fmt.Errorf("add updated port: %w", err)
	}
	return nil
}

// RemovePort removes one or more port rules.
func (s *Service) RemovePort(zone, port, protocol string) error {
	ports, err := s.expandPorts(port)
	if err != nil {
		return err
	}
	protocols, err := s.expandProtocols(protocol)
	if err != nil {
		return err
	}
	if err := s.validateZone(s.resolveZone(zone)); err != nil {
		return err
	}

	for _, p := range ports {
		for _, proto := range protocols {
			if err := s.checkPanelPort(p, proto); err != nil {
				return err
			}
			if err := s.deletePortRules(p, proto); err != nil {
				return fmt.Errorf("remove port %s/%s: %w", p, proto, err)
			}
		}
	}
	return nil
}

// AddService adds a service rule.
func (s *Service) AddService(zone, service string) error {
	port, protocol, err := servicePortSpec(service)
	if err != nil {
		return err
	}
	return s.AddPort(zone, port, protocol, service)
}

// RemoveService removes a service rule.
func (s *Service) RemoveService(zone, service string) error {
	port, protocol, err := servicePortSpec(service)
	if err != nil {
		return err
	}
	if err := s.checkProtectedService(service); err != nil {
		return err
	}
	return s.RemovePort(zone, port, protocol)
}

// AddRichRule is not supported by this iptables panel abstraction.
func (s *Service) AddRichRule(zone, rule string) error {
	return fmt.Errorf("iptables rich rule editing is not supported; add a port rule instead")
}

// RemoveRichRule is not supported by this iptables panel abstraction.
func (s *Service) RemoveRichRule(zone, rule string) error {
	return fmt.Errorf("iptables rich rule editing is not supported")
}

// StartIPTables ensures iptables is available and opens essential ports.
func (s *Service) StartIPTables() error {
	if _, err := exec.LookPath("iptables"); err != nil {
		return fmt.Errorf("iptables is not installed")
	}
	return s.EnsureEssentialRules()
}

// StartFirewalld is kept for route compatibility; it now checks iptables.
func (s *Service) StartFirewalld() error {
	return s.StartIPTables()
}

// EnsureEssentialRules opens the panel and SSH ports if iptables is installed.
func (s *Service) EnsureEssentialRules() error {
	status, err := s.Status()
	if err != nil {
		return err
	}
	if !status.Installed || !status.Running {
		return nil
	}

	var errs []string
	if s.panelPort != "" {
		if err := s.ensurePortRule(s.panelPort, "tcp", "Pdai panel"); err != nil {
			errs = append(errs, fmt.Sprintf("panel port %s/tcp: %v", s.panelPort, err))
		}
	}
	for _, port := range detectSSHPorts() {
		if err := s.ensurePortRule(port, "tcp", "SSH"); err != nil {
			errs = append(errs, fmt.Sprintf("ssh port %s/tcp: %v", port, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("ensure essential iptables rules: %s", strings.Join(errs, "; "))
	}
	return nil
}

// AvailableServices returns common service names supported by the panel.
func (s *Service) AvailableServices() ([]string, error) {
	return []string{"ssh", "http", "https"}, nil
}

// Reload reloads firewall rules where possible.
func (s *Service) Reload() error {
	return s.EnsureEssentialRules()
}

// ClearRules removes all non-essential port rules from INPUT and restores the
// panel and SSH rules afterward to avoid lockout.
func (s *Service) ClearRules() error {
	out, err := s.runCmd("-S", "INPUT")
	if err != nil {
		return err
	}

	sshPorts := map[string]bool{}
	for _, port := range detectSSHPorts() {
		sshPorts[port] = true
	}

	type ruleKey struct {
		port     string
		protocol string
	}

	seen := map[ruleKey]bool{}
	keys := make([]ruleKey, 0)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		port, protocol, _, ok := parseIPTablesPortRule(fields)
		if !ok {
			continue
		}
		if port == s.panelPort && protocol == "tcp" {
			continue
		}
		if protocol == "tcp" && sshPorts[port] {
			continue
		}
		key := ruleKey{port: port, protocol: protocol}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}

	for _, key := range keys {
		if err := s.deletePortRules(key.port, key.protocol); err != nil {
			return fmt.Errorf("clear port %s/%s: %w", key.port, key.protocol, err)
		}
	}

	return s.EnsureEssentialRules()
}

// ── internal helpers ──

func (s *Service) runCmd(args ...string) (string, error) {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		s.logger.Debug("iptables failed", "args", args, "output", result, "err", err)
		return result, fmt.Errorf("%s: %s", err, result)
	}
	return result, nil
}

func (s *Service) resolveZone(zone string) string {
	if zone == "" {
		return iptablesZoneName
	}
	return zone
}

func (s *Service) validateZone(zone string) error {
	if !zoneRe.MatchString(zone) {
		return fmt.Errorf("invalid zone name: %s", zone)
	}
	if zone != iptablesZoneName {
		return fmt.Errorf("iptables does not support zones in this panel")
	}
	return nil
}

func (s *Service) validatePort(port string) error {
	if !portRe.MatchString(port) {
		return fmt.Errorf("invalid port: %s (expected number or range like 8080-8090)", port)
	}
	parts := strings.SplitN(port, "-", 2)
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("port %s out of range (1-65535)", p)
		}
	}
	if len(parts) == 2 {
		lo, _ := strconv.Atoi(parts[0])
		hi, _ := strconv.Atoi(parts[1])
		if lo >= hi {
			return fmt.Errorf("invalid port range: start (%d) must be less than end (%d)", lo, hi)
		}
	}
	return nil
}

func (s *Service) validateProtocol(protocol string) error {
	if !protocolRe.MatchString(protocol) {
		return fmt.Errorf("invalid protocol: %s (expected tcp or udp)", protocol)
	}
	return nil
}

func (s *Service) expandPorts(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	ports := make([]string, 0, len(parts))
	for _, part := range parts {
		port := strings.TrimSpace(part)
		if port == "" {
			continue
		}
		if err := s.validatePort(port); err != nil {
			return nil, err
		}
		ports = append(ports, port)
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("port is required")
	}
	return ports, nil
}

func (s *Service) expandProtocols(protocol string) ([]string, error) {
	switch strings.TrimSpace(strings.ToLower(protocol)) {
	case "tcp", "udp":
		return []string{strings.TrimSpace(strings.ToLower(protocol))}, nil
	case "both", "tcp_udp":
		return []string{"tcp", "udp"}, nil
	default:
		return nil, fmt.Errorf("invalid protocol: %s (expected tcp, udp, or both)", protocol)
	}
}

// checkPanelPort prevents removing the panel's own port (exact or range).
func (s *Service) checkPanelPort(port, protocol string) error {
	if s.panelPort == "" || protocol != "tcp" {
		return nil
	}
	if port == s.panelPort {
		return fmt.Errorf("cannot remove panel port %s/tcp — this would lock you out", port)
	}
	if strings.Contains(port, "-") {
		parts := strings.SplitN(port, "-", 2)
		if len(parts) == 2 {
			lo, errLo := strconv.Atoi(parts[0])
			hi, errHi := strconv.Atoi(parts[1])
			pp, errPP := strconv.Atoi(s.panelPort)
			if errLo == nil && errHi == nil && errPP == nil && pp >= lo && pp <= hi {
				return fmt.Errorf("cannot remove port range %s/tcp — it contains panel port %s", port, s.panelPort)
			}
		}
	}
	return nil
}

// checkProtectedService prevents removing SSH (remote access lockout).
func (s *Service) checkProtectedService(service string) error {
	name := strings.ToLower(strings.TrimSpace(service))
	if name == "ssh" || name == "openssh" || strings.Contains(name, "ssh") {
		return fmt.Errorf("cannot remove SSH service — this would lock you out of remote access")
	}
	return nil
}

func (s *Service) ensurePortRule(port, protocol, comment string) error {
	args := inputAcceptRuleArgs("-C", port, protocol, "")
	if _, err := s.runCmd(args...); err == nil {
		return nil
	}
	args = inputAcceptRuleArgs("-A", port, protocol, strings.TrimSpace(comment))
	if _, err := s.runCmd(args...); err != nil {
		return err
	}
	return nil
}

func (s *Service) deletePortRules(port, protocol string) error {
	out, err := s.runCmd("-S", "INPUT")
	if err != nil {
		return err
	}

	matched := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if !iptablesRuleMatchesPort(fields, port, protocol) {
			continue
		}
		matched = true
		deleteArgs := append([]string{"-D", "INPUT"}, fields[2:]...)
		if _, err := s.runCmd(deleteArgs...); err != nil {
			return err
		}
	}
	if !matched {
		return nil
	}
	return nil
}

func (s *Service) parseIPTablesRules(output string) *ZoneInfo {
	z := &ZoneInfo{
		Name:       iptablesZoneName,
		Target:     "system default",
		Interfaces: []string{},
		Sources:    []string{},
		Services:   []string{},
		Ports:      []PortRule{},
		RichRules:  []string{},
		Active:     true,
	}

	type parsedPortRule struct {
		comment   string
		protocols map[string]bool
	}

	rules := map[string]*parsedPortRule{}
	order := []string{}

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		if len(fields) >= 3 && fields[0] == "-P" && fields[1] == "INPUT" {
			z.Target = strings.ToLower(fields[2])
			continue
		}
		port, protocol, comment, ok := parseIPTablesPortRule(fields)
		if !ok {
			continue
		}
		rule := rules[port]
		if rule == nil {
			rule = &parsedPortRule{protocols: map[string]bool{}}
			rules[port] = rule
			order = append(order, port)
		}
		rule.protocols[protocol] = true
		if rule.comment == "" && comment != "" {
			rule.comment = comment
		}
	}

	for _, port := range order {
		rule := rules[port]
		if rule == nil {
			continue
		}
		protocols := make([]string, 0, len(rule.protocols))
		for proto := range rule.protocols {
			protocols = append(protocols, proto)
		}
		slices.Sort(protocols)
		protocol := "both"
		if len(protocols) == 1 {
			protocol = protocols[0]
		}
		z.Ports = append(z.Ports, PortRule{
			Port:      port,
			Protocol:  protocol,
			Protocols: protocols,
			Comment:   rule.comment,
			Value:     port + "/" + protocol,
		})
	}
	return z
}

func inputAcceptRuleArgs(action, port, protocol, comment string) []string {
	args := []string{action, "INPUT", "-p", protocol, "-m", protocol, "--dport", iptablesPortSpec(port), "-j", "ACCEPT"}
	if comment != "" {
		args = append(args, "-m", "comment", "--comment", comment)
	}
	return args
}

func iptablesRuleMatchesPort(fields []string, port, protocol string) bool {
	foundInput := false
	foundProtocol := false
	foundPort := false
	foundAccept := false
	for i, field := range fields {
		switch field {
		case "-A":
			if i+1 < len(fields) && fields[i+1] == "INPUT" {
				foundInput = true
			}
		case "-p":
			if i+1 < len(fields) && fields[i+1] == protocol {
				foundProtocol = true
			}
		case "--dport":
			if i+1 < len(fields) && fields[i+1] == iptablesPortSpec(port) {
				foundPort = true
			}
		case "-j":
			if i+1 < len(fields) && fields[i+1] == "ACCEPT" {
				foundAccept = true
			}
		}
	}
	return foundInput && foundProtocol && foundPort && foundAccept
}

func parseIPTablesPortRule(fields []string) (string, string, string, bool) {
	if len(fields) == 0 || fields[0] != "-A" {
		return "", "", "", false
	}
	var protocol, port, comment string
	accept := false
	for i, field := range fields {
		switch field {
		case "-p":
			if i+1 < len(fields) {
				protocol = fields[i+1]
			}
		case "--dport":
			if i+1 < len(fields) {
				port = strings.ReplaceAll(fields[i+1], ":", "-")
			}
		case "--comment":
			if i+1 < len(fields) {
				j := i + 1
				parts := make([]string, 0)
				for j < len(fields) {
					if fields[j] == "-j" {
						break
					}
					parts = append(parts, fields[j])
					j++
				}
				comment = strings.Trim(strings.Join(parts, " "), `"`)
			}
		case "-j":
			if i+1 < len(fields) && fields[i+1] == "ACCEPT" {
				accept = true
			}
		}
	}
	if !protocolRe.MatchString(protocol) || port == "" || !accept {
		return "", "", "", false
	}
	return port, protocol, strings.TrimSpace(comment), true
}

func iptablesPortSpec(port string) string {
	return strings.ReplaceAll(port, "-", ":")
}

func servicePortSpec(service string) (string, string, error) {
	service = strings.ToLower(strings.TrimSpace(service))
	if !serviceRe.MatchString(service) {
		return "", "", fmt.Errorf("invalid service name")
	}
	switch service {
	case "ssh", "openssh":
		return "22", "tcp", nil
	case "http", "www", "web":
		return "80", "tcp", nil
	case "https":
		return "443", "tcp", nil
	default:
		return "", "", fmt.Errorf("unsupported service %q; add a port rule instead", service)
	}
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func detectSSHPorts() []string {
	ports := map[string]bool{}
	for _, path := range []string{"/etc/ssh/sshd_config", "/etc/ssh/sshd_config.d/*.conf"} {
		if strings.Contains(path, "*") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(fields[0], "Port") {
				if n, err := strconv.Atoi(fields[1]); err == nil && n >= 1 && n <= 65535 {
					ports[fields[1]] = true
				}
			}
		}
	}
	if len(ports) == 0 {
		ports["22"] = true
	}
	result := make([]string, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	return result
}

// InstallIPTables installs iptables and opens essential ports.
//
// ctx is the inbound request context so SSE client disconnect kills the
// install subprocess tree instead of leaving package managers running.
// Progress is streamed via writeSSE (log lines) and writeEvent (done/error signals).
func (s *Service) InstallIPTables(ctx context.Context, writeSSE func(string), writeEvent func(string, string)) {
	if _, err := exec.LookPath("iptables"); err == nil {
		writeSSE("iptables is already installed")
		if err := s.StartIPTables(); err != nil {
			writeSSE("ERROR: " + err.Error())
			writeEvent("error", "Failed to initialize iptables")
			return
		}
		writeEvent("done", "ok")
		return
	}

	osFamily := detectOSFamily()
	writeSSE("Detected OS family: " + osFamily)

	var installCmd string
	switch osFamily {
	case "rhel":
		installCmd = "dnf install -y iptables iptables-services"
	case "debian":
		installCmd = "apt-get update && apt-get install -y iptables"
	case "alpine":
		installCmd = "apk add --no-cache iptables"
	default:
		writeSSE("ERROR: Unsupported OS family: " + osFamily)
		writeEvent("error", "Unsupported OS family")
		return
	}

	writeSSE("Installing iptables...")
	if !s.streamCmd(ctx, installCmd, writeSSE) {
		writeEvent("error", "Installation failed")
		return
	}

	writeSSE("Adding essential iptables rules...")
	if err := s.StartIPTables(); err != nil {
		writeSSE("ERROR: " + err.Error())
		writeEvent("error", "Failed to initialize iptables")
		return
	}

	if v, err := s.runCmd("--version"); err == nil {
		writeSSE("iptables installed successfully: " + firstLine(v))
		writeEvent("done", "ok")
	} else {
		writeSSE("ERROR: iptables not found after installation")
		writeEvent("error", "iptables not found after install")
	}
}

// InstallFirewalld is kept for route compatibility; it now installs iptables.
func (s *Service) InstallFirewalld(ctx context.Context, writeSSE func(string), writeEvent func(string, string)) {
	s.InstallIPTables(ctx, writeSSE, writeEvent)
}

// streamCmd runs a shell command and streams stdout/stderr line by line.
// Returns true if the command exits successfully. ctx cancellation kills
// the whole subprocess group (execx.BashContext) so SSE disconnect doesn't
// leave a package manager transaction running.
func (s *Service) streamCmd(ctx context.Context, shellCmd string, writeSSE func(string)) bool {
	cmd := execx.BashContext(ctx, shellCmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeSSE("ERROR: " + err.Error())
		return false
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		writeSSE("ERROR: " + err.Error())
		return false
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			writeSSE(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		writeSSE("ERROR: command failed: " + err.Error())
		return false
	}
	return true
}

// detectOSFamily reads /etc/os-release to determine the OS family.
func detectOSFamily() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	content := strings.ToLower(string(data))

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id_like=") {
			val := strings.Trim(strings.TrimPrefix(line, "id_like="), "\"")
			if strings.Contains(val, "debian") || strings.Contains(val, "ubuntu") {
				return "debian"
			}
			if strings.Contains(val, "rhel") || strings.Contains(val, "fedora") || strings.Contains(val, "centos") {
				return "rhel"
			}
			if strings.Contains(val, "alpine") {
				return "alpine"
			}
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id=") {
			val := strings.Trim(strings.TrimPrefix(line, "id="), "\"")
			switch val {
			case "debian", "ubuntu", "linuxmint", "pop", "kali", "deepin":
				return "debian"
			case "rhel", "centos", "fedora", "rocky", "almalinux", "ol", "amzn":
				return "rhel"
			case "alpine":
				return "alpine"
			}
		}
	}
	return "unknown"
}
