package caddy

import "testing"

func TestValidateDomainAllowsPort(t *testing.T) {
	valid := []string{
		"example.com:8080",
		"localhost:3000",
		"127.0.0.1:8080",
		"*.example.com:8443",
	}
	for _, domain := range valid {
		if err := ValidateDomain(domain); err != nil {
			t.Fatalf("ValidateDomain(%q) returned error: %v", domain, err)
		}
	}
}

func TestValidateDomainRejectsInvalidPort(t *testing.T) {
	invalid := []string{
		"example.com:0",
		"example.com:65536",
		"example.com:99999",
		"example.com:",
	}
	for _, domain := range invalid {
		if err := ValidateDomain(domain); err == nil {
			t.Fatalf("ValidateDomain(%q) returned nil, want error", domain)
		}
	}
}

func TestDomainHostRemovesSchemeAndPort(t *testing.T) {
	cases := map[string]string{
		"example.com:8080":        "example.com",
		"http://example.com:8080": "example.com",
		"https://localhost:3000":  "localhost",
		"127.0.0.1:9000":          "127.0.0.1",
		"example.com":             "example.com",
	}
	for input, want := range cases {
		if got := DomainHost(input); got != want {
			t.Fatalf("DomainHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafeDomainFileName(t *testing.T) {
	cases := map[string]string{
		"example.com":      "example.com",
		"example.com:8080": "example.com_8080",
		"*.example.com":    "_.example.com",
		"":                 "site",
	}
	for input, want := range cases {
		if got := SafeDomainFileName(input); got != want {
			t.Fatalf("SafeDomainFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateFullCaddyBlockAllowsDomainWithPort(t *testing.T) {
	cases := []string{
		"example.com:8080 {\n\trespond \"ok\"\n}",
		"http://example.com:8080 {\n\trespond \"ok\"\n}",
		"https://example.com:8080 {\n\trespond \"ok\"\n}",
	}
	for _, block := range cases {
		if err := ValidateFullCaddyBlockForDomains([]string{"example.com:8080"}, block); err != nil {
			t.Fatalf("ValidateFullCaddyBlockForDomains returned error for %q: %v", block, err)
		}
	}
}

func TestValidateFullCaddyBlockAllowsPortWhenHostHasNoPort(t *testing.T) {
	block := "example.com:8080 {\n\trespond \"ok\"\n}"
	if err := ValidateFullCaddyBlockForDomains([]string{"example.com"}, block); err != nil {
		t.Fatalf("ValidateFullCaddyBlockForDomains returned error: %v", err)
	}
}

func TestValidateFullCaddyBlockRejectsDifferentPort(t *testing.T) {
	block := "example.com:9090 {\n\trespond \"ok\"\n}"
	if err := ValidateFullCaddyBlockForDomains([]string{"example.com:8080"}, block); err == nil {
		t.Fatalf("ValidateFullCaddyBlockForDomains returned nil for mismatched port")
	}
}

func TestValidateFullCaddyBlockRejectsMissingPortWhenHostHasPort(t *testing.T) {
	block := "example.com {\n\trespond \"ok\"\n}"
	if err := ValidateFullCaddyBlockForDomains([]string{"example.com:8080"}, block); err == nil {
		t.Fatalf("ValidateFullCaddyBlockForDomains returned nil for missing port")
	}
}
