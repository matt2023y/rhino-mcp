package rhino

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureEvents() []journalEvent {
	return []journalEvent{
		{V: 1, Event: "request", Model: "demo", ID: "one", Label: "first", Kind: "model", Tool: "script", Arguments: map[string]interface{}{"code": "pass", "lang": "python"}},
		{V: 1, Event: "result", Model: "demo", ID: "one", Status: "ok"},
	}
}
func eventFile(t *testing.T, events []journalEvent) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "log.jsonl")
	for _, e := range events {
		if err := appendEvent(p, e); err != nil {
			t.Fatal(err)
		}
	}
	return p
}
func TestJournalRejectsUnsafeAndInconsistentHistory(t *testing.T) {
	cases := map[string]func([]journalEvent) []journalEvent{
		"version":            func(e []journalEvent) []journalEvent { e[0].V = 2; return e },
		"path id":            func(e []journalEvent) []journalEvent { e[0].ID = "../bad"; return e },
		"mixed models":       func(e []journalEvent) []journalEvent { e[1].Model = "other"; return e },
		"duplicate result":   func(e []journalEvent) []journalEvent { return append(e, e[1]) },
		"future replacement": func(e []journalEvent) []journalEvent { e[0].Replaces = []string{"future"}; return e },
		"unknown tool":       func(e []journalEvent) []journalEvent { e[0].Tool = "delete-all"; return e },
		"unknown arg":        func(e []journalEvent) []journalEvent { e[0].Arguments["bogus"] = 1; return e },
		"invalid status":     func(e []journalEvent) []journalEvent { e[1].Status = "maybe"; return e },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := readJournal(eventFile(t, edit(fixtureEvents()))); err == nil {
				t.Fatal("accepted invalid history")
			}
		})
	}
}
func TestFailedStepCannotBeSkippedWithFrom(t *testing.T) {
	events := fixtureEvents()
	events[1].Status = "uncertain"
	next := fixtureEvents()
	next[0].ID = "two"
	next[1].ID = "two"
	events = append(events, next...)
	steps, err := readJournal(eventFile(t, events))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := effectivePlan(steps, "two"); err == nil {
		t.Fatal("--from bypassed unresolved operation")
	}
	events[2].Replaces = []string{"one"}
	steps, err = readJournal(eventFile(t, events))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := effectivePlan(steps, "")
	if err != nil || len(plan) != 1 || plan[0].Request.ID != "two" {
		t.Fatal("successful explicit correction failed", err)
	}
}
func TestPendingAndLock(t *testing.T) {
	p := eventFile(t, fixtureEvents()[:1])
	s, err := readJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := effectivePlan(s, ""); err == nil {
		t.Fatal("accepted pending request")
	}
	unlock, err := acquireLock(p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if other, err := acquireLock(p); err == nil {
		other()
		t.Fatal("concurrent lock acquired")
	}
}
func testSettings(t *testing.T, server *httptest.Server) settings {
	t.Helper()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	return settings{Timeout: time.Second, Endpoints: []endpoint{{Host: u.Hostname(), Port: port, Token: strings.Repeat("t", 32)}}}
}
func TestRetryOnlyKnownUnavailableResponses(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": "OK"})
	}))
	defer server.Close()
	cfg := testSettings(t, server)
	if _, err := pluginCall(context.Background(), cfg, rhinoRequest{Action: "run_script"}); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal("expected one safe retry")
	}
}
func TestTimeoutRecordsUncertainWithoutRetry(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": "OK"})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	writeTestFile(t, filepath.Join(root, ".env"), "RHINO_HOST="+u.Hostname()+"\nRHINO_PORT="+u.Port()+"\nRHINO_TOKEN="+strings.Repeat("t", 32)+"\nRHINO_REQUEST_TIMEOUT_MS=20\n")
	rt := runtime{root: root, session: "demo"}
	result := rt.invoke("script", map[string]interface{}{"code": "mutation()"}, callMeta{Model: "demo", Label: "one change"})
	if !result.IsError || count.Load() != 1 {
		t.Fatal("uncertain mutation was retried or marked successful")
	}
	path, _ := journalPath(root, "demo")
	steps, err := readJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Result.Status != "uncertain" {
		t.Fatal("uncertain status not recorded")
	}
}
func TestScriptErrorNoRetryAndRedaction(t *testing.T) {
	var count atomic.Int32
	token := strings.Repeat("t", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "failed with " + token})
	}))
	defer server.Close()
	_, err := pluginCall(context.Background(), testSettings(t, server), rhinoRequest{Action: "run_script"})
	if err == nil || strings.Contains(err.Error(), token) || count.Load() != 1 {
		t.Fatal("wrong mutation-error handling")
	}
}
func TestToolArgumentsAndDelay(t *testing.T) {
	for _, args := range []map[string]interface{}{{"width": -1.0}, {"width": 1.5}, {"width": 4096.0, "height": 4096.0}, {"width": "12"}} {
		if _, err := prepareTool("capture", args); err == nil {
			t.Fatal("accepted invalid capture")
		}
	}
	if _, err := prepareTool("exec", map[string]interface{}{"cmd": "_Point", "args": []interface{}{1.0}}); err == nil {
		t.Fatal("accepted non-string macro args")
	}
	for _, s := range []string{"NaN", "Inf", "-1", "3601"} {
		if _, err := replayDelay(s); err == nil {
			t.Fatal("accepted invalid delay")
		}
	}
	if d, err := replayDelay("0.5s"); err != nil || d != 500*time.Millisecond {
		t.Fatal("delay parsing")
	}
	if got := formatCommand("_-Import", []string{`C:\my model.3dm`}, true); got != `_-Import "C:\my model.3dm" _Enter` {
		t.Fatal("Windows path quoting", got)
	}
}
func TestTokenCannotEnterRequestJournal(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	token := strings.Repeat("t", 32)
	writeTestFile(t, filepath.Join(root, ".env"), "RHINO_HOST=127.0.0.1\nRHINO_PORT=1234\nRHINO_TOKEN="+token)
	rt := runtime{root: root, session: "demo"}
	res := rt.invoke("script", map[string]interface{}{"code": token}, callMeta{Model: "demo"})
	if !res.IsError || strings.Contains(res.text(), token) {
		t.Fatal("token request accepted or echoed")
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "rhino-mcp-modeling", "models-logs", "demo.jsonl")); !os.IsNotExist(err) {
		t.Fatal("token request was journaled")
	}
}
