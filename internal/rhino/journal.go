package rhino

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type journalEvent struct {
	V         int                    `json:"v"`
	Event     string                 `json:"event"`
	Model     string                 `json:"model"`
	ID        string                 `json:"id"`
	At        string                 `json:"at"`
	Label     string                 `json:"label,omitempty"`
	Kind      string                 `json:"kind,omitempty"`
	Tool      string                 `json:"tool,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Replaces  []string               `json:"replaces"`
	Note      string                 `json:"note,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Message   string                 `json:"message,omitempty"`
	Artifacts []artifact             `json:"artifacts,omitempty"`
}
type journalStep struct {
	Request journalEvent
	Result  *journalEvent
}

func logsDir(root string) string {
	if info, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil && !info.IsDir() {
		return filepath.Join(root, "models-logs")
	}
	return filepath.Join(root, "skills", "rhino-mcp-modeling", "models-logs")
}

func journalPath(root, model string) (string, error) {
	if !validSlug(model) {
		return "", errors.New("invalid model name; use letters, digits, underscores or hyphens (not Windows device names)")
	}
	return filepath.Join(logsDir(root), model+".jsonl"), nil
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func acquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot lock journal (inspect any existing .lock before removing): %w", err)
	}
	_, err = fmt.Fprintf(f, "%d\n", os.Getpid())
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(path + ".lock")
		return nil, errors.New("cannot write journal lock")
	}
	return func() { _ = os.Remove(path + ".lock") }, nil
}
func appendEvent(path string, e journalEvent) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func readJournal(path string) ([]journalStep, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 65536), 32<<20)
	var steps []journalStep
	seen := map[string]int{}
	model := ""
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var e journalEvent
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("invalid JSON at line %d", line)
		}
		if e.V != 1 || !validSlug(e.Model) || !validSlug(e.ID) {
			return nil, fmt.Errorf("invalid event at line %d", line)
		}
		if model == "" {
			model = e.Model
		}
		if e.Model != model {
			return nil, errors.New("one model per journal is required")
		}
		switch e.Event {
		case "request":
			if _, ok := seen[e.ID]; ok {
				return nil, errors.New("duplicate request id")
			}
			if strings.TrimSpace(e.Label) == "" || (e.Kind != "model" && e.Kind != "check" && e.Kind != "view") {
				return nil, errors.New("request needs label and valid kind")
			}
			if _, err := prepareTool(e.Tool, e.Arguments); err != nil {
				return nil, err
			}
			replaced := map[string]bool{}
			for _, id := range e.Replaces {
				if _, ok := seen[id]; !ok || replaced[id] {
					return nil, errors.New("replaces must reference distinct earlier requests")
				}
				replaced[id] = true
			}
			seen[e.ID] = len(steps)
			steps = append(steps, journalStep{Request: e})
		case "result":
			index, ok := seen[e.ID]
			if !ok || steps[index].Result != nil {
				return nil, errors.New("orphan or duplicate result")
			}
			if e.Status != "ok" && e.Status != "error" && e.Status != "uncertain" {
				return nil, errors.New("invalid result status")
			}
			copy := e
			steps[index].Result = &copy
		default:
			return nil, errors.New("unknown journal event")
		}
	}
	return steps, scanner.Err()
}
func effectivePlan(steps []journalStep, from string) ([]journalStep, error) {
	if len(steps) == 0 {
		return nil, errors.New("journal is empty")
	}
	replaced := map[string]bool{}
	for _, s := range steps {
		if s.Result != nil && s.Result.Status == "ok" {
			for _, id := range s.Request.Replaces {
				replaced[id] = true
			}
		}
	}
	var plan []journalStep
	for _, s := range steps {
		if replaced[s.Request.ID] {
			continue
		}
		if s.Result == nil {
			return nil, fmt.Errorf("unresolved step %s: pending", s.Request.ID)
		}
		if s.Result.Status != "ok" {
			return nil, fmt.Errorf("unresolved step %s: %s", s.Request.ID, s.Result.Status)
		}
		plan = append(plan, s)
	}
	if from != "" {
		for i, s := range plan {
			if s.Request.ID == from {
				return plan[i:], nil
			}
		}
		return nil, errors.New("--from is not an effective step")
	}
	return plan, nil
}
func saveImage(encoded, path, id string) (artifact, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return artifact{}, errors.New("invalid capture base64")
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return artifact{}, errors.New("capture is not PNG")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return artifact{}, err
	}
	filename := filepath.Join(path, id+".png")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return artifact{}, err
	}
	sum := sha256.Sum256(data)
	return artifact{Path: filename, SHA256: hex.EncodeToString(sum[:])}, nil
}

type callMeta struct {
	Mode     string   `json:"mode"`
	Model    string   `json:"model"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Replaces []string `json:"replaces"`
	Note     string   `json:"note"`
}
type toolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}
type toolResult struct {
	Content    []toolContent          `json:"content"`
	IsError    bool                   `json:"isError,omitempty"`
	Structured map[string]interface{} `json:"structuredContent,omitempty"`
}

func resultError(err error) toolResult {
	return toolResult{IsError: true, Content: []toolContent{{Type: "text", Text: err.Error()}}}
}
func (r toolResult) text() string {
	var s []string
	for _, c := range r.Content {
		if c.Type == "text" {
			s = append(s, c.Text)
		}
	}
	return strings.Join(s, "\n")
}

type runtime struct {
	root    string
	session string
}

func (rt *runtime) invoke(tool string, args map[string]interface{}, meta callMeta) toolResult {
	if args == nil {
		args = map[string]interface{}{}
	}
	if value, exists := args["journal"]; exists {
		if meta.Model != "" || meta.Label != "" || meta.Kind != "" || meta.Mode != "" || len(meta.Replaces) > 0 || meta.Note != "" {
			return resultError(errors.New("provide journal arguments or journal metadata, not both"))
		}
		raw, err := json.Marshal(value)
		if err != nil || string(raw) == "null" || json.Unmarshal(raw, &meta) != nil || meta.Mode != "" {
			return resultError(errors.New("invalid journal arguments"))
		}
		clean := make(map[string]interface{}, len(args)-1)
		for key, item := range args {
			if key != "journal" {
				clean[key] = item
			}
		}
		args = clean
	}
	request, err := prepareTool(tool, args)
	if err != nil {
		return resultError(err)
	}
	cfg, err := currentSettings(rt.root)
	if err != nil {
		return resultError(err)
	}
	raw, _ := json.Marshal(struct {
		Args interface{}
		Meta callMeta
	}{args, meta})
	for _, ep := range cfg.Endpoints {
		if strings.Contains(string(raw), ep.Token) {
			return resultError(errors.New("request must not contain the Rhino token"))
		}
	}
	if meta.Mode != "" && meta.Mode != "replay" {
		return resultError(errors.New("unsupported journal mode"))
	}
	if meta.Mode == "replay" {
		value, err := pluginCall(context.Background(), cfg, request)
		if err != nil {
			out := resultError(err)
			var uncertain *uncertainError
			if errors.As(err, &uncertain) {
				out.Structured = map[string]interface{}{"status": "uncertain"}
			}
			return out
		}
		if tool == "capture" {
			return toolResult{Content: []toolContent{{Type: "image", MimeType: "image/png", Data: value}}}
		}
		return toolResult{Content: []toolContent{{Type: "text", Text: firstNonEmpty(redact(value, cfg), "OK")}}}
	}
	if meta.Model == "" {
		meta.Model = rt.session
	}
	if meta.Label == "" {
		meta.Label = tool
	}
	if meta.Kind == "" {
		meta.Kind = "model"
		if tool == "capture" {
			meta.Kind = "check"
		}
	}
	if meta.Kind != "model" && meta.Kind != "check" && meta.Kind != "view" {
		return resultError(errors.New("kind must be model, check or view"))
	}
	path, err := journalPath(rt.root, meta.Model)
	if err != nil {
		return resultError(err)
	}
	unlock, err := acquireLock(path)
	if err != nil {
		return resultError(err)
	}
	defer unlock()
	steps, err := readJournal(path)
	if err != nil && !os.IsNotExist(err) {
		return resultError(err)
	}
	ids := map[string]bool{}
	for _, s := range steps {
		if s.Request.Model != meta.Model {
			return resultError(errors.New("journal model mismatch"))
		}
		ids[s.Request.ID] = true
	}
	used := map[string]bool{}
	for _, id := range meta.Replaces {
		if !ids[id] || used[id] {
			return resultError(errors.New("replaces needs distinct earlier request ids"))
		}
		used[id] = true
	}
	if meta.Replaces == nil {
		meta.Replaces = []string{}
	}
	id := newID()
	event := journalEvent{V: 1, Event: "request", Model: meta.Model, ID: id, At: now(), Label: meta.Label, Kind: meta.Kind, Tool: tool, Arguments: args, Replaces: meta.Replaces, Note: meta.Note}
	if err := appendEvent(path, event); err != nil {
		return resultError(err)
	}
	value, callErr := pluginCall(context.Background(), cfg, request)
	result := journalEvent{V: 1, Event: "result", Model: meta.Model, ID: id, At: now(), Status: "ok", Message: firstNonEmpty(redact(value, cfg), "OK")}
	if callErr == nil && tool == "capture" {
		result.Message = "PNG captured"
		a, err := saveImage(value, path+".artifacts", id)
		if err != nil {
			callErr = err
		} else {
			result.Artifacts = []artifact{a}
		}
	}
	if callErr != nil {
		result.Status = "error"
		var uncertain *uncertainError
		if errors.As(callErr, &uncertain) {
			result.Status = "uncertain"
		}
		result.Message = redact(callErr.Error(), cfg)
	}
	if len(result.Message) > 4000 {
		result.Message = result.Message[:4000]
	}
	if err := appendEvent(path, result); err != nil {
		return resultError(fmt.Errorf("operation finished but journal result could not be saved; inspect before retrying: %w", err))
	}
	out := toolResult{IsError: callErr != nil, Content: []toolContent{{Type: "text", Text: result.Message}}, Structured: map[string]interface{}{"id": id, "journal": path, "status": result.Status, "artifacts": result.Artifacts}}
	if tool == "capture" && callErr == nil {
		out.Content = append(out.Content, toolContent{Type: "image", MimeType: "image/png", Data: value})
	}
	return out
}
