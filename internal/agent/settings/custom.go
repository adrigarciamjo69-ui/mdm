package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

type customParams struct {
	Interpreter string `json:"interpreter"`
	Detect      string `json:"detect"`
	Remediate   string `json:"remediate"`
	TimeoutS    int    `json:"timeout_s"`
}

type customScript struct {
	p customParams
	d Deps
}

func init() {
	b := func(params json.RawMessage, d Deps) (Setting, error) {
		var p customParams
		if err := json.Unmarshal(params, &p); err != nil || p.Detect == "" {
			return nil, errors.New("script personalizado sin detección")
		}
		if p.TimeoutS <= 0 {
			p.TimeoutS = 300
		}
		return &customScript{p: p, d: d}, nil
	}
	register(protocol.PlatformLinux, protocol.CustomScriptKey, b)
	register(protocol.PlatformWindows, protocol.CustomScriptKey, b)
}

func (c *customScript) run(ctx context.Context, content string) exec.Result {
	return c.d.Exec(ctx, exec.Spec{Interpreter: c.p.Interpreter, Content: content,
		Timeout: time.Duration(c.p.TimeoutS) * time.Second, ScriptDir: c.d.ScriptDir})
}

func (c *customScript) Check(ctx context.Context) (bool, string, error) {
	r := c.run(ctx, c.p.Detect)
	if r.Err != nil {
		return false, "", r.Err
	}
	if r.TimedOut {
		return false, "", fmt.Errorf("la detección superó %d s", c.p.TimeoutS)
	}
	detail := lastLine(r.Stdout)
	if detail == "" {
		detail = lastLine(r.Stderr)
	}
	return r.ExitCode == 0, detail, nil
}

func (c *customScript) Apply(ctx context.Context) error {
	if c.p.Remediate == "" {
		return errors.New("no hay script de corrección")
	}
	r := c.run(ctx, c.p.Remediate)
	switch {
	case r.Err != nil:
		return r.Err
	case r.TimedOut:
		return fmt.Errorf("la corrección superó %d s", c.p.TimeoutS)
	case r.ExitCode != 0:
		return fmt.Errorf("la corrección terminó con código %d: %s", r.ExitCode, lastLine(r.Stderr+"\n"+r.Stdout))
	}
	return nil
}
