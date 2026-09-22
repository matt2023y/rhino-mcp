package rhino

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

var testBinary, serverBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rhino-go-tests-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(dir, "rhino-tool.exe")

	serverBinary = filepath.Join(dir, "rhino-mcp-server.exe")
	for _, target := range []struct{ output, path string }{{testBinary, "../../cmd/rhino-tool"}, {serverBinary, "../../cmd/rhino-mcp-server"}} {
		cmd := exec.Command("go", "build", "-o", target.output, target.path)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintln(os.Stderr, string(out), err)
			os.RemoveAll(dir)
			os.Exit(1)
		}
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func commandIn(root, home string, args ...string) *exec.Cmd {
	binary := testBinary
	if len(args) > 0 && args[0] == "server" {
		binary = serverBinary
		args = args[1:]
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if k != "HOME" && k != "USERPROFILE" && k != "RHINO_ROOT" {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "USERPROFILE="+home, "RHINO_ROOT="+root)
	return cmd
}
func TestServerRecordAndIndependentToolReplay(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Rhino 中文 package")
	home := t.TempDir()
	writeTestFile(t, filepath.Join(root, "skills", "rhino-mcp-modeling", "SKILL.md"), "fixture")
	var count atomic.Int32
	token := strings.Repeat("t", 32)
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII="
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing auth")
		}
		var request rhinoRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if n == 1 {
			p, _ := journalPath(root, "demo")
			steps, err := readJournal(p)
			if err != nil || len(steps) != 1 || steps[0].Result != nil {
				t.Error("request was not durably recorded before execution")
			}
		}
		data := "OK"
		if request.Action == "capture" {
			data = png
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": data})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	writeTestFile(t, filepath.Join(root, ".env"), "RHINO_HOST="+u.Hostname()+"\nRHINO_PORT="+u.Port()+"\nRHINO_TOKEN="+token)

	for _, call := range []struct {
		tool string
		args map[string]interface{}
	}{{"script", map[string]interface{}{"code": "assert True", "lang": "python"}}, {"capture", map[string]interface{}{}}} {
		call.args["journal"] = map[string]interface{}{"model": "demo", "label": "check", "kind": "check"}
		message := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": call.tool, "arguments": call.args}}
		data, _ := json.Marshal(message)
		cmd := commandIn(root, home, "server")
		cmd.Stdin = bytes.NewReader(append(data, '\n'))
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("server call: %s %v", out, err)
		}
		var response struct {
			Result toolResult `json:"result"`
		}
		if json.Unmarshal(out, &response) != nil || response.Result.IsError {
			t.Fatalf("server error: %s", out)
		}
		if bytes.Contains(out, []byte(token)) {
			t.Fatal("server leaked token")
		}
	}

	path, _ := journalPath(root, "demo")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var first map[string]interface{}
	json.Unmarshal(bytes.Split(before, []byte("\n"))[0], &first)
	if _, ok := first["replaces"].([]interface{}); !ok {
		t.Fatal("request is not compatible with original JSONL schema")
	}
	steps, err := readJournal(path)
	if err != nil || len(steps) != 2 || len(steps[1].Result.Artifacts) != 1 {
		t.Fatal("recording failed", err)
	}
	out, err := commandIn(root, home, "replay", "--model", "demo", "--dry-run").CombinedOutput()
	if err != nil {
		t.Fatalf("dry run: %s %v", out, err)
	}
	if count.Load() != 2 {
		t.Fatal("dry run called Rhino")
	}
	// External journals remain untouched; all new outputs belong beside SKILL.md.
	external := filepath.Join(t.TempDir(), "source.jsonl")
	writeTestFile(t, external, string(before))

	if err := os.Rename(serverBinary, serverBinary+".disabled"); err != nil {
		t.Fatal(err)
	}
	out, err = commandIn(root, home, "replay", "--log", external, "--step", "0").CombinedOutput()
	restoreErr := os.Rename(serverBinary+".disabled", serverBinary)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}

	if err != nil {
		t.Fatalf("replay: %s %v", out, err)
	}
	if count.Load() != 4 {
		t.Fatalf("unexpected calls: %d", count.Load())
	}
	after, _ := os.ReadFile(path)
	externalAfter, _ := os.ReadFile(external)
	if !bytes.Equal(before, after) || !bytes.Equal(before, externalAfter) {
		t.Fatal("replay rewrote source journal")
	}
	matches, _ := filepath.Glob(path + ".replay-artifacts/*.png")
	if len(matches) != 1 {
		t.Fatal("replay capture missing from models-logs")
	}
	receipt, err := os.ReadFile(path + ".last-replay.json")
	if err != nil || !bytes.Contains(receipt, []byte(`"status": "ok"`)) {
		t.Fatal("missing replay receipt", err)
	}
	cfg, err := commandIn(root, home, "config").CombinedOutput()
	if err != nil || bytes.Contains(cfg, []byte(token)) {
		t.Fatal("config failed or leaked token")
	}
}
func TestPortableExecutableRootFromDifferentWorkingDirectory(t *testing.T) {
	for _, layout := range []string{"source", "packaged", "build"} {
		t.Run(layout, func(t *testing.T) {
			testPortableRoot(t, layout)
		})
	}
}

func testPortableRoot(t *testing.T, layout string) {
	root := t.TempDir()
	exe := filepath.Join(root, "rhino-tool.exe")
	wantLogs := filepath.Join(root, "skills", "rhino-mcp-modeling", "models-logs")
	if layout == "packaged" {
		wantLogs = filepath.Join(root, "models-logs")
		writeTestFile(t, filepath.Join(root, "SKILL.md"), "fixture")
		if err := os.Mkdir(filepath.Join(root, "bin"), 0755); err != nil {
			t.Fatal(err)
		}
		exe = filepath.Join(root, "bin", "rhino-tool.exe")
	} else {
		writeTestFile(t, filepath.Join(root, "skills", "rhino-mcp-modeling", "SKILL.md"), "fixture")
		if layout == "build" {
			exe = filepath.Join(root, ".build", "macos", "rhino-tool")
			if err := os.MkdirAll(filepath.Dir(exe), 0755); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := os.ReadFile(testBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, data, 0700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, ".env"), "RHINO_HOST=127.0.0.1\nRHINO_PORT=1234\nRHINO_TOKEN="+strings.Repeat("t", 32))
	cmd := exec.Command(exe, "config")
	cmd.Dir = t.TempDir()
	home := t.TempDir()
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if k != "RHINO_ROOT" && k != "HOME" && k != "USERPROFILE" {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "USERPROFILE="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("portable root: %s %v", out, err)
	}
	var result struct {
		Logs string `json:"logs"`
	}
	if json.Unmarshal(out, &result) != nil || result.Logs != wantLogs {
		t.Fatal("used cwd instead of executable root")
	}
}

func TestReplayStopsAtFirstFailedOperation(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(root, "skills", "rhino-mcp-modeling", "SKILL.md"), "fixture")
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "geometry assertion failed"})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	writeTestFile(t, filepath.Join(root, ".env"), "RHINO_HOST="+u.Hostname()+"\nRHINO_PORT="+u.Port()+"\nRHINO_TOKEN="+strings.Repeat("t", 32))
	events := fixtureEvents()
	next := fixtureEvents()
	next[0].ID = "two"
	next[1].ID = "two"
	events = append(events, next...)
	path := eventFile(t, events)
	before, _ := os.ReadFile(path)
	out, err := commandIn(root, home, "replay", "--log", path, "--step", "0").CombinedOutput()
	if err == nil || count.Load() != 1 || !bytes.Contains(out, []byte("geometry assertion failed")) {
		t.Fatalf("replay did not stop: %d %s", count.Load(), out)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed replay modified source")
	}
	receiptPath, _ := journalPath(root, "demo")
	data, err := os.ReadFile(receiptPath + ".last-replay.json")
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]interface{}
	json.Unmarshal(data, &receipt)
	if receipt["completed"] != float64(0) || receipt["lastStep"] != "one" || receipt["status"] != "error" {
		t.Fatal("failure receipt wrong")
	}
}

func TestDirectMCPAutomaticallyRecordsSession(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(root, "skills", "rhino-mcp-modeling", "SKILL.md"), "fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": "OK"})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	writeTestFile(t, filepath.Join(root, ".env"), "RHINO_HOST="+u.Hostname()+"\nRHINO_PORT="+u.Port()+"\nRHINO_TOKEN="+strings.Repeat("t", 32))
	cmd := commandIn(root, home, "server")
	cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"script\",\"arguments\":{\"code\":\"pass\"}}}\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("stdout is not clean JSON-RPC: %s", out)
	}
	var last struct {
		ID     int        `json:"id"`
		Result toolResult `json:"result"`
	}
	if json.Unmarshal(lines[2], &last) != nil || last.ID != 3 || last.Result.IsError {
		t.Fatalf("MCP failed: %s", out)
	}
	paths, _ := filepath.Glob(filepath.Join(root, "skills", "rhino-mcp-modeling", "models-logs", "session-*.jsonl"))
	if len(paths) != 1 {
		t.Fatal("session journal missing")
	}
	steps, err := readJournal(paths[0])
	if err != nil || len(steps) != 1 || steps[0].Result.Status != "ok" {
		t.Fatal("direct MCP call was not recorded", err)
	}
}
