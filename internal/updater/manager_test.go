package updater

import "testing"

func TestIsUpdateAvailableTimestampBuildToSemverRelease(t *testing.T) {
	if !isUpdateAvailable("2605162347", "1.0.3") {
		t.Fatal("expected timestamp build to update to semver release")
	}
}

func TestIsUpdateAvailableSemver(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "older semver", current: "1.0.2", latest: "1.0.3", want: true},
		{name: "same semver", current: "1.0.3", latest: "1.0.3", want: false},
		{name: "newer semver", current: "1.1.0", latest: "1.0.3", want: false},
		{name: "v prefix", current: "v1.0.2", latest: "v1.0.3", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUpdateAvailable(tt.current, tt.latest); got != tt.want {
				t.Fatalf("isUpdateAvailable(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestIsTimestampBuildVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "2605162347", want: true},
		{version: "v2605162347", want: true},
		{version: "1.0.3", want: false},
		{version: "dev", want: false},
		{version: "123", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := isTimestampBuildVersion(tt.version); got != tt.want {
				t.Fatalf("isTimestampBuildVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
