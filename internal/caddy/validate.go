package caddy

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// domainRegex matches valid domain names (with optional wildcard prefix and port).
var domainRegex = regexp.MustCompile(`^(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*(\:\d{1,5})?$`)

// ValidateDomain checks if a domain name is safe for Caddyfile injection.
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	hostPart, port, hasPort := splitDomainPort(domain)
	if len(domain) > 255 {
		return fmt.Errorf("domain too long (max 255 chars including optional port)")
	}
	if len(hostPart) > 253 {
		return fmt.Errorf("domain too long (max 253 chars)")
	}
	// Reject characters that could break Caddyfile syntax
	if strings.ContainsAny(domain, " \t\n\r{}\"'`;#$\\") {
		return fmt.Errorf("domain contains invalid characters")
	}
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s", domain)
	}
	if hasPort {
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			return fmt.Errorf("invalid port number: %s", port)
		}
	}
	return nil
}

func splitDomainPort(domain string) (host, port string, hasPort bool) {
	idx := strings.LastIndex(domain, ":")
	if idx < 0 || idx == len(domain)-1 {
		return domain, "", false
	}
	port = domain[idx+1:]
	for _, ch := range port {
		if ch < '0' || ch > '9' {
			return domain, "", false
		}
	}
	return domain[:idx], port, true
}

func normalizeDomainAddress(address string) (exact, host string, hasPort bool, err error) {
	address = strings.ToLower(strings.TrimSpace(address))
	if strings.HasPrefix(address, "http://") {
		address = strings.TrimPrefix(address, "http://")
	} else if strings.HasPrefix(address, "https://") {
		address = strings.TrimPrefix(address, "https://")
	}
	if err := ValidateDomain(address); err != nil {
		return "", "", false, err
	}
	host, _, hasPort = splitDomainPort(address)
	if !hasPort {
		host = address
	}
	return address, host, hasPort, nil
}

// DomainHost returns the hostname part of a domain/address, removing an optional
// scheme and port. It is intended for DNS lookups, where "example.com:8080"
// must be resolved as "example.com".
func DomainHost(domain string) string {
	domain = strings.TrimSpace(domain)
	if strings.HasPrefix(domain, "http://") {
		domain = strings.TrimPrefix(domain, "http://")
	} else if strings.HasPrefix(domain, "https://") {
		domain = strings.TrimPrefix(domain, "https://")
	}
	host, _, hasPort := splitDomainPort(domain)
	if hasPort {
		return host
	}
	return domain
}

// SafeDomainFileName converts a validated domain/address into a filesystem-safe
// path segment for logs, cert directories and generated site roots. Normal
// domains are unchanged; wildcard/port characters are replaced with underscores.
func SafeDomainFileName(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "site"
	}
	var b strings.Builder
	b.Grow(len(domain))
	for _, ch := range domain {
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9',
			ch == '.', ch == '-', ch == '_':
			b.WriteRune(ch)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "site"
	}
	if len(name) > 180 {
		name = name[:180]
		name = strings.TrimRight(name, ".")
		if name == "" {
			name = "site"
		}
	}
	return name
}

// ValidateUpstream checks if an upstream address is safe for Caddyfile injection.
func ValidateUpstream(addr string) error {
	if addr == "" {
		return fmt.Errorf("upstream address cannot be empty")
	}
	// Reject Caddyfile-breaking characters
	if strings.ContainsAny(addr, " \t\n\r{}\"'`;#$\\") {
		return fmt.Errorf("upstream address contains invalid characters")
	}

	// Allow http:// or https:// prefixed URLs
	clean := addr
	if strings.HasPrefix(clean, "http://") {
		clean = strings.TrimPrefix(clean, "http://")
	} else if strings.HasPrefix(clean, "https://") {
		clean = strings.TrimPrefix(clean, "https://")
	}

	// Should be host:port or just host
	host, port, err := net.SplitHostPort(clean)
	if err != nil {
		// Might be host without port
		host = clean
		port = ""
	}

	// Validate host part
	if host == "" {
		return fmt.Errorf("upstream host cannot be empty")
	}
	if len(host) > 253 {
		return fmt.Errorf("upstream host too long")
	}

	// Validate port if present
	if port != "" {
		if len(port) > 5 {
			return fmt.Errorf("invalid port number")
		}
	}

	return nil
}

// ValidateIPRange checks if an IP range is safe for Caddyfile injection.
func ValidateIPRange(ipRange string) error {
	if ipRange == "" {
		return fmt.Errorf("IP range cannot be empty")
	}
	// Reject Caddyfile-breaking characters
	if strings.ContainsAny(ipRange, " \t\n\r{}\"'`;#$\\") {
		return fmt.Errorf("IP range contains invalid characters")
	}

	// Try parsing as CIDR
	if strings.Contains(ipRange, "/") {
		_, _, err := net.ParseCIDR(ipRange)
		if err != nil {
			return fmt.Errorf("invalid CIDR notation: %s", ipRange)
		}
		return nil
	}

	// Try parsing as plain IP
	if net.ParseIP(ipRange) == nil {
		return fmt.Errorf("invalid IP address: %s", ipRange)
	}
	return nil
}

// ValidateCaddyValue checks that a string is safe for embedding in a Caddyfile.
// It rejects newlines, braces, quotes, and backslashes that could alter structure
// or break quoted directives (e.g. header values rendered as "...").
func ValidateCaddyValue(label, value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, "\n\r{}\"\\") {
		return fmt.Errorf("%s contains characters that could break Caddyfile syntax", label)
	}
	return nil
}

// SanitizeCustomDirectives validates custom directives to prevent Caddyfile injection.
// It rejects lines that could close/open blocks unexpectedly.
func SanitizeCustomDirectives(directives string) error {
	if directives == "" {
		return nil
	}

	lines := strings.Split(directives, "\n")
	braceDepth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Count braces to detect block manipulation.
		// Check after EACH closing brace so that "} {" on the same line
		// (which nets to zero) is still caught.
		for _, ch := range trimmed {
			switch ch {
			case '{':
				braceDepth++
			case '}':
				braceDepth--
				if braceDepth < 0 {
					return fmt.Errorf("line %d: unbalanced closing brace — cannot close parent block", i+1)
				}
			}
		}
	}

	// All opened braces must be closed
	if braceDepth != 0 {
		return fmt.Errorf("unbalanced braces in custom directives (depth: %d)", braceDepth)
	}

	return nil
}

// ValidateFullCaddyBlock validates a complete single-site Caddy block override.
func ValidateFullCaddyBlock(domain, block string) error {
	return ValidateFullCaddyBlockForDomains([]string{domain}, block)
}

// ValidateFullCaddyBlockForDomains validates a complete single-site Caddy block override
// whose site addresses must all belong to the given host domains.
func ValidateFullCaddyBlockForDomains(domains []string, block string) error {
	block = strings.TrimSpace(block)
	if block == "" {
		return nil
	}
	allowedExact := make(map[string]bool, len(domains))
	allowedHostWithoutPort := make(map[string]bool, len(domains))
	primary := ""
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		exact, hostOnly, hasPort, err := normalizeDomainAddress(domain)
		if err != nil {
			return err
		}
		if primary == "" {
			primary = exact
		}
		allowedExact[exact] = true
		if !hasPort {
			allowedHostWithoutPort[hostOnly] = true
		}
	}
	if primary == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	if strings.ContainsAny(block, "\x00") {
		return fmt.Errorf("full Caddy block contains invalid null byte")
	}

	lines := strings.Split(block, "\n")
	firstLine := strings.TrimSpace(lines[0])
	if !strings.HasSuffix(firstLine, "{") {
		return fmt.Errorf("full Caddy block must start with '%s {'", primary)
	}

	addresses := strings.Split(strings.TrimSpace(strings.TrimSuffix(firstLine, "{")), ",")
	if len(addresses) == 0 {
		return fmt.Errorf("full Caddy block must start with '%s {'", primary)
	}
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			return fmt.Errorf("full Caddy block contains an empty site address")
		}

		exact, hostOnly, hasPort, err := normalizeDomainAddress(address)
		if err != nil {
			return fmt.Errorf("invalid full Caddy block site address '%s': %w", address, err)
		}
		if !allowedExact[exact] && !(hasPort && allowedHostWithoutPort[hostOnly]) {
			return fmt.Errorf("full Caddy block domain '%s' must match one of the host domains", exact)
		}
	}

	braceDepth := 0
	seenOpen := false
	closedRoot := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if closedRoot {
			return fmt.Errorf("line %d: no content is allowed after the closing site block", i+1)
		}
		for _, ch := range trimmed {
			switch ch {
			case '{':
				braceDepth++
				seenOpen = true
			case '}':
				braceDepth--
				if braceDepth < 0 {
					return fmt.Errorf("line %d: unbalanced closing brace", i+1)
				}
				if seenOpen && braceDepth == 0 {
					closedRoot = true
				}
			}
		}
	}
	if !seenOpen {
		return fmt.Errorf("full Caddy block must contain an opening brace")
	}
	if braceDepth != 0 {
		return fmt.Errorf("unbalanced braces in full Caddy block (depth: %d)", braceDepth)
	}
	return nil
}
