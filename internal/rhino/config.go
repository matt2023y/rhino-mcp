package rhino

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type endpoint struct {
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	ProcessID int       `json:"processId"`
	Source    string    `json:"-"`
	Modified  time.Time `json:"-"`
}
type settings struct {
	Endpoints []endpoint
	Source    string
	Timeout   time.Duration
}

var endpointPattern = regexp.MustCompile(`^plugin-port-[0-9]+-[0-9a-f]+\.json$`)
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}$`)

func validSlug(s string) bool {
	if !slugPattern.MatchString(s) {
		return false
	}
	switch strings.ToUpper(s) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}
func packageRoot() (string, error) {
	if root := os.Getenv("RHINO_ROOT"); root != "" {
		return checkRoot(root)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if root, err := checkRoot(filepath.Dir(exe)); err == nil {
		return root, nil
	}
	if root, err := checkRoot(filepath.Dir(filepath.Dir(exe))); err == nil {
		return root, nil
	}
	// Development outputs live in <root>/.build/<platform>.
	buildDir := filepath.Dir(filepath.Dir(exe))
	if filepath.Base(buildDir) == ".build" {
		if root, err := checkRoot(filepath.Dir(buildDir)); err == nil {
			return root, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if root, err := checkRoot(dir); err == nil {
			return root, nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", errors.New("package root not found; keep the executable in the skill's bin directory, or set RHINO_ROOT")
}
func checkRoot(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil && !info.IsDir() {
		if bin, err := os.Stat(filepath.Join(root, "bin")); err == nil && bin.IsDir() {
			return root, nil
		}
	}
	if info, err := os.Stat(filepath.Join(root, "skills", "rhino-mcp-modeling", "SKILL.md")); err != nil || info.IsDir() {
		return "", errors.New("package root must contain SKILL.md and bin, or skills/rhino-mcp-modeling/SKILL.md")
	}
	return root, nil
}
func loadSettings(root, home string) (settings, error) {
	cfg := settings{Timeout: 120 * time.Second}
	directory := filepath.Join(home, ".rhino-mcp")
	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		return cfg, err
	}
	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !endpointPattern.MatchString(entry.Name()) {
			continue
		}
		found++
		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var ep endpoint
		if json.Unmarshal(data, &ep) != nil || ep.Port < 1 || ep.Port > 65535 || len(ep.Token) < 32 || ep.ProcessID < 1 {
			continue
		}
		// Local plugin files always describe this computer, never a remote host.
		ep.Host = "127.0.0.1"
		ep.Source = path
		if info, err := entry.Info(); err == nil {
			ep.Modified = info.ModTime()
		}
		cfg.Endpoints = append(cfg.Endpoints, ep)
	}
	if len(cfg.Endpoints) > 0 {
		sort.SliceStable(cfg.Endpoints, func(i, j int) bool { return cfg.Endpoints[i].Modified.After(cfg.Endpoints[j].Modified) })
		cfg.Source = directory
		return cfg, nil
	}
	persistent := filepath.Join(directory, "config.json")
	if data, err := os.ReadFile(persistent); err == nil {
		var ep endpoint
		if json.Unmarshal(data, &ep) != nil || ep.Port < 1 || ep.Port > 65535 || len(ep.Token) < 32 {
			return cfg, errors.New("invalid local .rhino-mcp/config.json (no .env fallback)")
		}
		ep.Host, ep.Source = "127.0.0.1", persistent
		cfg.Source, cfg.Endpoints = persistent, []endpoint{ep}
		return cfg, nil
	} else if !os.IsNotExist(err) {
		return cfg, err
	}
	if found > 0 {
		return cfg, errors.New("local Rhino endpoint files exist but are invalid; restart Rhino to refresh them (no .env fallback)")
	}
	path := filepath.Join(root, ".env")
	values, err := readEnv(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, errors.New("no local Rhino endpoint or package .env found")
		}
		return cfg, err
	}
	port, err := strconv.Atoi(values["RHINO_PORT"])
	host, token := values["RHINO_HOST"], values["RHINO_TOKEN"]
	if err != nil || port < 1 || port > 65535 || host == "" || strings.ContainsAny(host, " /\\\t\r\n?#@") || len(token) < 32 {
		return cfg, errors.New(".env requires valid RHINO_HOST, RHINO_PORT and RHINO_TOKEN (at least 32 characters)")
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return cfg, errors.New("invalid RHINO_HOST")
	}
	if text := values["RHINO_REQUEST_TIMEOUT_MS"]; text != "" {
		ms, err := strconv.Atoi(text)
		if err != nil || ms < 1 || ms > 600000 {
			return cfg, errors.New("RHINO_REQUEST_TIMEOUT_MS must be 1..600000")
		}
		cfg.Timeout = time.Duration(ms) * time.Millisecond
	}
	cfg.Source = path
	cfg.Endpoints = []endpoint{{Host: host, Port: port, Token: token, Source: path}}
	return cfg, nil
}

// Deliberately no shell evaluation, variable interpolation or process-env merging.
func readEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		s := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimSpace(strings.TrimPrefix(s, "export "))
		k, v, ok := strings.Cut(s, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid .env assignment at line %d", line)
		}
		if len(v) > 0 && (v[0] == '\'' || v[0] == '"') {
			q := v[0]
			end := strings.IndexByte(v[1:], q)
			if end < 0 {
				return nil, fmt.Errorf("unclosed .env quote at line %d", line)
			}
			end++
			rest := strings.TrimSpace(v[end+1:])
			if rest != "" && !strings.HasPrefix(rest, "#") {
				return nil, fmt.Errorf("unexpected .env suffix at line %d", line)
			}
			v = v[1:end]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		values[k] = v
	}
	return values, scanner.Err()
}
func currentSettings(root string) (settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return settings{}, err
	}
	return loadSettings(root, home)
}
func redact(s string, cfg settings) string {
	for _, ep := range cfg.Endpoints {
		if ep.Token != "" {
			s = strings.ReplaceAll(s, ep.Token, "[REDACTED]")
		}
	}
	return s
}
