package rhino

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
}

func runServer(root string) error {
	rt := runtime{root: root, session: fmt.Sprintf("session-%s-%d", time.Now().UTC().Format("20060102T150405"), os.Getpid())}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 65536), 32<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var req rpcRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			if err := encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: map[string]interface{}{"code": -32700, "message": "invalid JSON"}}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		res := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			res.Result = map[string]interface{}{"protocolVersion": "2024-11-05", "capabilities": map[string]interface{}{"tools": map[string]interface{}{}}, "serverInfo": map[string]string{"name": "rhino-mcp-server-go", "version": "1.0.0"}}
		case "ping":
			res.Result = map[string]interface{}{}
		case "tools/list":
			res.Result = map[string]interface{}{"tools": toolDefinitions()}
		case "tools/call":
			var p struct {
				Name      string                     `json:"name"`
				Arguments map[string]interface{}     `json:"arguments"`
				Meta      map[string]json.RawMessage `json:"_meta"`
			}
			var meta callMeta
			if json.Unmarshal(req.Params, &p) != nil {
				res.Result = resultError(errors.New("invalid tool parameters"))
			} else if raw := p.Meta["rhino-mcp/journal"]; len(raw) > 0 && json.Unmarshal(raw, &meta) != nil {
				res.Result = resultError(errors.New("invalid journal metadata"))
			} else {
				res.Result = rt.invoke(p.Name, p.Arguments, meta)
			}
		default:
			res.Error = map[string]interface{}{"code": -32601, "message": "method not found"}
		}
		if err := encoder.Encode(res); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// RunServer serves MCP stdio without spawning any client process.
func RunServer() error {
	root, err := packageRoot()
	if err != nil {
		return err
	}
	return runServer(root)
}
