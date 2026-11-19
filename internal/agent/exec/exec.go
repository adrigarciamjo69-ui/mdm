// Package exec ejecuta scripts del agente con tiempo límite, capturando la
// salida con memoria acotada y matando el árbol de procesos completo si se
// agota el tiempo o se cancela.
package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"time"
)

// MaxOutput es lo que se conserva de cada flujo de salida.
const MaxOutput = 64 * 1024

type Spec struct {
	// Interpreter es powershell, sh, bash o python3.
	Interpreter string
	Content     string
	// Env se añade al entorno del sistema.
	Env     map[string]string
	Timeout time.Duration
	// Dir es el directorio de trabajo; vacío, el del agente.
	Dir string
	// ScriptDir es donde se escribe el script temporal; vacío, el temporal del sistema.
	ScriptDir string
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	TimedOut bool
	// Err indica que el script no llegó a ejecutarse o se canceló.
	Err      error
	Started  time.Time
	Finished time.Time
}

var extensions = map[string]string{"powershell": ".ps1", "sh": ".sh", "bash": ".sh", "python3": ".py", "cmd": ".cmd"}

func command(interpreter, file string) ([]string, error) {
	switch interpreter {
	case "powershell":
		return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", file}, nil
	case "sh", "bash", "python3":
		return []string{interpreter, file}, nil
	case "cmd":
		return []string{"cmd.exe", "/d", "/c", file}, nil
	}
	return nil, fmt.Errorf("intérprete no admitido: %q", interpreter)
}

// Run ejecuta el script y espera a que termine.
func Run(ctx context.Context, s Spec) Result {
	res := Result{Started: time.Now()}
	finish := func(err error) Result {
		res.Err = err
		res.Finished = time.Now()
		return res
	}
	ext, ok := extensions[s.Interpreter]
	if !ok {
		return finish(fmt.Errorf("intérprete no admitido: %q", s.Interpreter))
	}
	f, err := os.CreateTemp(s.ScriptDir, "mdm-script-*"+ext)
	if err != nil {
		return finish(fmt.Errorf("crear el script temporal: %w", err))
	}
	path := f.Name()
	defer os.Remove(path)
	_ = f.Chmod(0o700)
	_, werr := f.WriteString(s.Content)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return finish(fmt.Errorf("escribir el script temporal: %w", werr))
	}

	args, err := command(s.Interpreter, path)
	if err != nil {
		return finish(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	cmd := osexec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = s.Dir
	cmd.Env = os.Environ()
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdout, stderr := newCapture(MaxOutput), newCapture(MaxOutput)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Si un nieto hereda las tuberías y sigue vivo, no se espera indefinidamente.
	cmd.WaitDelay = 2 * time.Second
	tree := prepareTree(cmd)

	if err := cmd.Start(); err != nil {
		return finish(fmt.Errorf("ejecutar %s: %w", args[0], err))
	}
	tree.attach(cmd)
	err = cmd.Wait()
	tree.release(runCtx.Err() != nil)

	res.Stdout, res.Stderr = stdout.String(), stderr.String()
	res.ExitCode = cmd.ProcessState.ExitCode()
	switch {
	case ctx.Err() != nil:
		return finish(fmt.Errorf("cancelado: %w", ctx.Err()))
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		return finish(nil)
	}
	var exitErr *osexec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !errors.Is(err, osexec.ErrWaitDelay) {
		return finish(err)
	}
	return finish(nil)
}
