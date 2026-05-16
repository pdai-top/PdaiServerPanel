package database

import "testing"

func TestMySQLDumpArgsManagedMySQL(t *testing.T) {
	args := MySQLDumpArgs(&Instance{Engine: EngineMySQL}, []string{"app"})

	if !containsArg(args, "--skip-events") {
		t.Fatalf("expected --skip-events in %v", args)
	}
	if !containsArg(args, "--set-gtid-purged=OFF") {
		t.Fatalf("expected --set-gtid-purged=OFF in %v", args)
	}
	if got := args[len(args)-1]; got != "app" {
		t.Fatalf("expected database name at the end, got %q in %v", got, args)
	}
}

func TestMySQLDumpArgsMariaDB(t *testing.T) {
	args := MySQLDumpArgs(&Instance{Engine: EngineMariaDB}, []string{"app"})

	if !containsArg(args, "--skip-events") {
		t.Fatalf("expected --skip-events in %v", args)
	}
	if containsArg(args, "--set-gtid-purged=OFF") {
		t.Fatalf("did not expect MySQL-only GTID flag in %v", args)
	}
}

func TestIsSystemDatabaseName(t *testing.T) {
	for _, name := range []string{"information_schema", "mysql", "performance_schema", "sys", "__recycle_bin__"} {
		if !isSystemDatabaseName(name) {
			t.Fatalf("expected %q to be filtered", name)
		}
	}
	if isSystemDatabaseName("hais_server_main") {
		t.Fatalf("did not expect a user database to be filtered")
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
