package rhino

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxCapturePixels = int64(16_000_000)

func stringSlice(value interface{}) []string {
	values, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func captureDimension(arguments map[string]interface{}, name string, fallback int) (int, error) {
	value, exists := arguments[name]
	if !exists {
		return fallback, nil
	}
	number, ok := value.(float64)
	if !ok || number < 1 || number > 4096 || number != float64(int(number)) {
		return 0, fmt.Errorf("capture %s must be an integer from 1 to 4096", name)
	}
	return int(number), nil
}

func formatCommand(command string, arguments []string, enter bool) string {
	parts := []string{command}
	for _, argument := range arguments {
		if strings.ContainsAny(argument, " \t\r\n") && !(strings.HasPrefix(argument, "\"") && strings.HasSuffix(argument, "\"")) {
			argument = `"` + argument + `"`
		}
		parts = append(parts, argument)
	}
	result := strings.Join(parts, " ")
	if enter {
		result += " _Enter"
	}
	return result
}

func firstNonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func prepareTool(name string, args map[string]interface{}) (rhinoRequest, error) {
	r := rhinoRequest{Params: map[string]interface{}{}}
	allowed := map[string]map[string]bool{"script": {"code": true, "lang": true}, "exec": {"cmd": true, "args": true, "enter": true}, "capture": {"width": true, "height": true}}
	fields, ok := allowed[name]
	if !ok {
		return r, errors.New("unknown tool")
	}
	for k := range args {
		if !fields[k] {
			return r, fmt.Errorf("unknown %s argument: %s", name, k)
		}
	}
	switch name {
	case "script":
		code, ok := args["code"].(string)
		if !ok || strings.TrimSpace(code) == "" {
			return r, errors.New("script requires nonempty code")
		}
		lang := "python"
		if value, exists := args["lang"]; exists {
			lang, ok = value.(string)
			if !ok || (lang != "python" && lang != "rhinoscript") {
				return r, errors.New("lang must be python or rhinoscript")
			}
		}
		r.Action = "run_script"
		r.Params = map[string]interface{}{"code": code, "lang": lang}
	case "exec":
		cmd, ok := args["cmd"].(string)
		if !ok || strings.TrimSpace(cmd) == "" {
			return r, errors.New("exec requires cmd")
		}
		var list []string
		if value, exists := args["args"]; exists {
			values, ok := value.([]interface{})
			if !ok {
				return r, errors.New("args must be a string array")
			}
			for _, v := range values {
				s, ok := v.(string)
				if !ok {
					return r, errors.New("args must contain only strings")
				}
				list = append(list, s)
			}
		}
		enter := true
		if v, exists := args["enter"]; exists {
			enter, ok = v.(bool)
			if !ok {
				return r, errors.New("enter must be boolean")
			}
		}
		r.Action = "run_command"
		r.Params["command"] = formatCommand(cmd, list, enter)
	case "capture":
		w, err := captureDimension(args, "width", 1200)
		if err != nil {
			return r, err
		}
		h, err := captureDimension(args, "height", 900)
		if err != nil {
			return r, err
		}
		if int64(w)*int64(h) > maxCapturePixels {
			return r, errors.New("capture exceeds pixel limit")
		}
		r.Action = "capture"
		r.Params = map[string]interface{}{"width": w, "height": h}
	}
	return r, nil
}
func toolDefinitions() interface{} {
	var tools interface{}
	_ = json.Unmarshal([]byte(`[
 {"name":"script","description":"Execute one Rhino Python/RhinoScript step and record it.","inputSchema":{"type":"object","properties":{"code":{"type":"string"},"lang":{"type":"string","enum":["python","rhinoscript"]}},"required":["code"],"additionalProperties":false}},
 {"name":"exec","description":"Execute a complete noninteractive Rhino macro and record it.","inputSchema":{"type":"object","properties":{"cmd":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"enter":{"type":"boolean"}},"required":["cmd"],"additionalProperties":false}},
 {"name":"capture","description":"Capture active viewport; PNG is stored beside the model journal.","inputSchema":{"type":"object","properties":{"width":{"type":"integer","minimum":1,"maximum":4096},"height":{"type":"integer","minimum":1,"maximum":4096}},"additionalProperties":false}}
 ]`), &tools)
	journal := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"model":    map[string]string{"type": "string"},
			"label":    map[string]string{"type": "string"},
			"kind":     map[string]interface{}{"type": "string", "enum": []string{"model", "check", "view"}},
			"note":     map[string]string{"type": "string"},
			"replaces": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
		},
		"additionalProperties": false,
	}
	for _, item := range tools.([]interface{}) {
		schema := item.(map[string]interface{})["inputSchema"].(map[string]interface{})
		schema["properties"].(map[string]interface{})["journal"] = journal
	}
	return tools
}
