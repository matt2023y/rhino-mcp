package rhino

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLocalDiscoveryPrecedesDotEnvAndProcessEnvironment(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	token := strings.Repeat("a", 32)
	for i, name := range []string{"plugin-port-11-aa.json", "plugin-port-22-bb.json"} {
		p := filepath.Join(home, ".rhino-mcp", name)
		data, _ := json.Marshal(endpoint{Host: "untrusted.example", Port: 5000 + i, Token: token, ProcessID: 11 + i})
		writeTestFile(t, p, string(data))
		stamp := time.Unix(100+int64(i), 0)
		os.Chtimes(p, stamp, stamp)
	}
	writeTestFile(t, filepath.Join(root, ".env"), "invalid fallback must not even be parsed")
	t.Setenv("RHINO_HOST", "other-host")
	t.Setenv("RHINO_PORT", "9999")
	t.Setenv("RHINO_TOKEN", strings.Repeat("z", 32))
	cfg, err := loadSettings(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Endpoints) != 2 || cfg.Endpoints[0].Port != 5001 || cfg.Endpoints[0].Host != "127.0.0.1" || cfg.Endpoints[0].Token != token {
		t.Fatalf("wrong local precedence: ports %v", cfg.Endpoints[0].Port)
	}
}
func TestEnvFallbackWithWindowsText(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	token := strings.Repeat("a", 32)
	writeTestFile(t, filepath.Join(root, ".env"), "\ufeff# comment\r\nexport RHINO_HOST = '127.0.0.1' # local\r\nRHINO_PORT=1234 # port\r\nRHINO_TOKEN=\""+token+"\"\r\nRHINO_REQUEST_TIMEOUT_MS=456\r\nIGNORED=C:\\Rhino\\file\r\n")
	cfg, err := loadSettings(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoints[0].Port != 1234 || cfg.Timeout != 456*time.Millisecond || cfg.Source != filepath.Join(root, ".env") {
		t.Fatal("wrong dotenv settings")
	}
}
func TestInvalidLocalFileDoesNotRouteToRemote(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(home, ".rhino-mcp", "plugin-port-1-aa.json"), "bad JSON")
	writeTestFile(t, filepath.Join(root, ".env"), "RHINO_HOST=remote.example\nRHINO_PORT=1234\nRHINO_TOKEN="+strings.Repeat("a", 32))
	if _, err := loadSettings(root, home); err == nil || !strings.Contains(err.Error(), "no .env fallback") {
		t.Fatalf("expected local config error, got %v", err)
	}
}
func TestInvalidEnvAndNoSecretsInErrors(t *testing.T) {
	for _, input := range []string{"RHINO_HOST=127.0.0.1", "RHINO_HOST='unterminated", "RHINO_HOST=127.0.0.1\nRHINO_PORT=70000\nRHINO_TOKEN=secret-value"} {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, ".env"), input)
		_, err := loadSettings(root, t.TempDir())
		if err == nil {
			t.Fatal("accepted invalid config")
		}
		if strings.Contains(err.Error(), "secret-value") {
			t.Fatal("secret leaked")
		}
	}
}
func TestWindowsSafeNames(t *testing.T) {
	for _, s := range []string{"..", "../other", "C:\\other", "CON", "nul", "LPT1", "a/b", "a.b", ""} {
		if validSlug(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"epaper-frame", "A_1", "92bf-11"} {
		if !validSlug(s) {
			t.Fatalf("rejected %q", s)
		}
	}
}

func TestPersistentLocalConfigBeforeEnv(t *testing.T) {
	root, profile := t.TempDir(), t.TempDir()
	token := strings.Repeat("a", 32)
	data, _ := json.Marshal(endpoint{Port: 55540, Token: token})
	path := filepath.Join(profile, ".rhino-mcp", "config.json")
	writeTestFile(t, path, string(data))
	writeTestFile(t, filepath.Join(root, ".env"), "invalid fallback")
	cfg, err := loadSettings(root, profile)
	if err != nil || cfg.Source != path || cfg.Endpoints[0].Host != "127.0.0.1" || cfg.Endpoints[0].Port != 55540 {
		t.Fatalf("persistent local config: %v", err)
	}
	active := filepath.Join(profile, ".rhino-mcp", "plugin-port-99-aa.json")
	data, _ = json.Marshal(endpoint{Port: 55541, Token: strings.Repeat("b", 32), ProcessID: 99})
	writeTestFile(t, active, string(data))
	cfg, err = loadSettings(root, profile)
	if err != nil || cfg.Endpoints[0].Port != 55541 {
		t.Fatal("active instance should override persistent config")
	}
}
