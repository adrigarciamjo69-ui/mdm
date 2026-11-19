//go:build linux

package exec

import (
	"context"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

func TestRun_ExitCodeAndOutput(t *testing.T) {
	r := Run(ctx, Spec{Interpreter: "sh", Content: "echo hola; echo error >&2; exit 3", Timeout: 10 * time.Second})
	if r.Err != nil || r.ExitCode != 3 || r.Stdout != "hola\n" || r.Stderr != "error\n" || r.TimedOut {
		t.Fatalf("%+v", r)
	}
	if !r.Finished.After(r.Started) && !r.Finished.Equal(r.Started) {
		t.Fatal("fechas")
	}
}

func TestRun_EnvParams(t *testing.T) {
	v := `a b; rm -rf / $(whoami) "x"`
	r := Run(ctx, Spec{Interpreter: "bash", Content: `printf '%s' "$MDM_PARAM_X"`, Env: map[string]string{"MDM_PARAM_X": v}, Timeout: 10 * time.Second})
	if r.Stdout != v {
		t.Fatalf("el parámetro debe llegar literal: %q", r.Stdout)
	}
}

func TestRun_PythonAndDir(t *testing.T) {
	if _, err := osexec.LookPath("python3"); err != nil {
		t.Skip("sin python3")
	}
	dir := t.TempDir()
	r := Run(ctx, Spec{Interpreter: "python3", Content: "import os; print(os.getcwd())", Dir: dir, Timeout: 10 * time.Second})
	if strings.TrimSpace(r.Stdout) != dir {
		t.Fatalf("%+v", r)
	}
}

func TestRun_TimeoutKillsTree(t *testing.T) {
	marker := "mdm-test-" + time.Now().Format("150405.000000")
	start := time.Now()
	r := Run(ctx, Spec{Interpreter: "sh", Content: "sleep 300 # " + marker + " &\nsleep 301 # " + marker + "\n", Timeout: time.Second})
	if !r.TimedOut || time.Since(start) > 10*time.Second {
		t.Fatalf("no se cortó a tiempo: %+v (%v)", r, time.Since(start))
	}
	time.Sleep(200 * time.Millisecond)
	out, _ := osexec.Command("pgrep", "-f", "sleep 30[01]").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		_ = osexec.Command("pkill", "-f", "sleep 30[01]").Run()
		t.Fatalf("quedan procesos hijos vivos: %s", out)
	}
}

func TestRun_ContextCancel(t *testing.T) {
	c, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	start := time.Now()
	r := Run(c, Spec{Interpreter: "sh", Content: "sleep 300", Timeout: time.Minute})
	if time.Since(start) > 5*time.Second || r.Err == nil {
		t.Fatalf("cancelar debe matar el proceso: %+v", r)
	}
}

func TestRun_TempFileRemoved(t *testing.T) {
	dir := t.TempDir()
	r := Run(ctx, Spec{Interpreter: "sh", Content: `printf '%s' "$0"`, Timeout: 10 * time.Second, ScriptDir: dir})
	if r.Stdout == "" {
		t.Fatalf("%+v", r)
	}
	if !strings.HasPrefix(r.Stdout, dir) {
		t.Fatalf("el script debe escribirse en ScriptDir: %s", r.Stdout)
	}
	if _, err := os.Stat(r.Stdout); !os.IsNotExist(err) {
		t.Fatal("el fichero temporal no se borró")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("quedan ficheros: %v", entries)
	}
}

func TestRun_UnknownInterpreter(t *testing.T) {
	r := Run(ctx, Spec{Interpreter: "cobol", Content: "x", Timeout: time.Second})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "intérprete") {
		t.Fatalf("%+v", r)
	}
}
