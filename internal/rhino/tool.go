package rhino

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const help = `Rhino tool (Windows / macOS, Go)
  rhino-tool[.exe] config
  rhino-tool[.exe] replay --model NAME [--dry-run] [--from ID] [--step 0.5]
  rhino-tool[.exe] replay --log FILE.jsonl [--dry-run] [--from ID] [--step 0.5]
Config: local Rhino endpoint/config first, package .env only if absent.
Replay talks directly to the Rhino plugin; no MCP client/server subprocess is needed.
Outputs: models-logs beside SKILL.md, in both source and packaged layouts.
`

func RunTool(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(help)
		return nil
	}
	root, err := packageRoot()
	if err != nil {
		return err
	}
	switch args[0] {
	case "config":
		if len(args) != 1 {
			return errors.New("config takes no flags")
		}
		cfg, err := currentSettings(root)
		if err != nil {
			return err
		}
		var candidates []map[string]interface{}
		for _, ep := range cfg.Endpoints {
			candidates = append(candidates, map[string]interface{}{"host": ep.Host, "port": ep.Port, "file": ep.Source})
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"source": cfg.Source, "endpoints": candidates, "requestTimeoutMs": cfg.Timeout.Milliseconds(), "logs": logsDir(root)})

	case "replay":
		return runReplay(root, args[1:])
	default:
		return fmt.Errorf("unknown tool command: %s", args[0])
	}
}
func replayDelay(s string) (time.Duration, error) {
	seconds, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "s"), 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 3600 {
		return 0, errors.New("--step must be a finite number of seconds from 0 to 3600")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
func runReplay(root string, argv []string) (runErr error) {
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	model := flags.String("model", "", "model journal in models-logs beside SKILL.md")
	input := flags.String("log", "", "existing JSONL to read")
	from := flags.String("from", "", "effective starting step")
	step := flags.String("step", "0.5", "seconds between completed steps")
	dry := flags.Bool("dry-run", false, "validate without connecting")
	if err := flags.Parse(argv); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if (*model == "") == (*input == "") {
		return errors.New("provide exactly one of --model or --log")
	}
	path := *input
	var err error
	if *model != "" {
		path, err = journalPath(root, *model)
		if err != nil {
			return err
		}
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	delay, err := replayDelay(*step)
	if err != nil {
		return err
	}
	unlock, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer unlock()
	steps, err := readJournal(path)
	if err != nil {
		return err
	}
	plan, err := effectivePlan(steps, *from)
	if err != nil {
		return err
	}
	if *dry {
		var out []map[string]string
		for _, s := range plan {
			e := s.Request
			out = append(out, map[string]string{"id": e.ID, "label": e.Label, "tool": e.Tool, "kind": e.Kind})
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	output, _ := journalPath(root, plan[0].Request.Model)
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return err
	}
	runID := newID()
	receipt := map[string]interface{}{"at": now(), "source": path, "status": "running", "completed": 0, "steps": len(plan), "delayMs": delay.Milliseconds()}
	saveReceipt := func() error {
		data, err := json.MarshalIndent(receipt, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(output+".last-replay.json", append(data, '\n'), 0600)
	}
	defer func() {
		if err := saveReceipt(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("cannot save replay receipt: %w", err))
		}
	}()
	for i, s := range plan {
		if i > 0 {
			time.Sleep(delay)
		}
		e := s.Request
		fmt.Fprintf(os.Stderr, "[%d/%d] %s %s\n", i+1, len(plan), e.ID, e.Label)

		receipt["lastStep"] = e.ID
		cfg, err := currentSettings(root)
		if err != nil {
			receipt["status"] = "config-error"
			return err
		}
		request, err := prepareTool(e.Tool, e.Arguments)
		if err != nil {
			receipt["status"] = "error"
			return err
		}
		value, err := pluginCall(context.Background(), cfg, request)
		if err != nil {
			receipt["status"] = "error"
			var uncertain *uncertainError
			if errors.As(err, &uncertain) {
				receipt["status"] = "uncertain"
			}
			return fmt.Errorf("replay stopped at %s: %w", e.ID, err)
		}
		if e.Tool == "capture" {
			if _, err := saveImage(value, output+".replay-artifacts", runID+"-"+e.ID); err != nil {
				receipt["status"] = "artifact-error"
				return err
			}
		}

		receipt["completed"] = i + 1
	}
	receipt["status"] = "ok"
	return json.NewEncoder(os.Stdout).Encode(receipt)
}
