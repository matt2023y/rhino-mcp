package rhino

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

type rhinoRequest struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params"`
}
type uncertainError struct{ err error }

func (e *uncertainError) Error() string {
	return "Rhino execution state is uncertain; inspect before retrying: " + e.err.Error()
}
func (e *uncertainError) Unwrap() error { return e.err }
func pluginCall(ctx context.Context, cfg settings, request rhinoRequest) (string, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		for _, ep := range cfg.Endpoints {
			url := "http://" + net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port)) + "/mcp"
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return "", err
			}
			req.Header.Set("Authorization", "Bearer "+ep.Token)
			req.Header.Set("Content-Type", "application/json")
			res, err := client.Do(req)
			if err != nil {
				var op *net.OpError
				if errors.As(err, &op) && op.Op == "dial" {
					last = errors.New("Rhino connection unavailable")
					continue
				}
				return "", &uncertainError{errors.New(redact(err.Error(), cfg))}
			}
			data, readErr := io.ReadAll(io.LimitReader(res.Body, 24<<20))
			res.Body.Close()
			if readErr != nil {
				return "", &uncertainError{errors.New("response body interrupted")}
			}
			switch res.StatusCode {
			case 401, 404, 503:
				last = fmt.Errorf("Rhino endpoint unavailable (HTTP %d)", res.StatusCode)
				continue
			}
			var result struct {
				Success bool   `json:"success"`
				Data    string `json:"data"`
				Error   string `json:"error"`
			}
			if json.Unmarshal(data, &result) != nil {
				return "", &uncertainError{errors.New("invalid Rhino response")}
			}
			if res.StatusCode != 200 || !result.Success {
				if result.Error == "" {
					result.Error = fmt.Sprintf("Rhino returned HTTP %d", res.StatusCode)
				}
				return "", errors.New(redact(result.Error, cfg))
			}
			return result.Data, nil
		}
	}
	if last == nil {
		last = errors.New("no Rhino endpoint")
	}
	return "", last
}
