package firewall

// FirewallStatus represents the overall iptables status.
type FirewallStatus struct {
	Installed   bool     `json:"installed"`
	Running     bool     `json:"running"`
	DefaultZone string   `json:"default_zone"`
	Version     string   `json:"version"`
	Zones       []string `json:"zones"`
}

// ZoneInfo represents a virtual iptables zone with its rules.
type ZoneInfo struct {
	Name       string     `json:"name"`
	Target     string     `json:"target"`
	Interfaces []string   `json:"interfaces"`
	Sources    []string   `json:"sources"`
	Services   []string   `json:"services"`
	Ports      []PortRule `json:"ports"`
	RichRules  []string   `json:"rich_rules"`
	Active     bool       `json:"active"`
}

// PortRule represents one display row in the firewall port table.
type PortRule struct {
	Port      string   `json:"port"`
	Protocol  string   `json:"protocol"`
	Protocols []string `json:"protocols"`
	Comment   string   `json:"comment"`
	Value     string   `json:"value"`
}

// AddPortRequest is the input for adding a port rule.
type AddPortRequest struct {
	Zone     string `json:"zone"`
	Port     string `json:"port" binding:"required"`
	Protocol string `json:"protocol" binding:"required"`
	Comment  string `json:"comment"`
}

// UpdatePortRequest is the input for editing an existing port rule.
type UpdatePortRequest struct {
	Zone        string `json:"zone"`
	OldPort     string `json:"old_port" binding:"required"`
	OldProtocol string `json:"old_protocol" binding:"required"`
	Port        string `json:"port" binding:"required"`
	Protocol    string `json:"protocol" binding:"required"`
	Comment     string `json:"comment"`
}

// AddServiceRequest is the input for adding a service rule.
type AddServiceRequest struct {
	Zone    string `json:"zone"`
	Service string `json:"service" binding:"required"`
}

// AddRichRuleRequest is the input for adding a rich rule.
type AddRichRuleRequest struct {
	Zone string `json:"zone"`
	Rule string `json:"rule" binding:"required"`
}
