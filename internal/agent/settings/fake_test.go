package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mdm/internal/agent/exec"
)

var ctx = context.Background()

// fakeSystem simula órdenes y ficheros. Las respuestas se buscan por la
// orden completa («ufw status»); las llamadas quedan registradas.
type fakeSystem struct {
	t       *testing.T
	root    string
	tools   map[string]bool
	replies map[string]string
	fail    map[string]bool
	calls   []string
	onCall  func(cmd string)
}

func newFake(t *testing.T, tools ...string) *fakeSystem {
	f := &fakeSystem{t: t, root: t.TempDir(), tools: map[string]bool{}, replies: map[string]string{}, fail: map[string]bool{}}
	for _, tool := range tools {
		f.tools[tool] = true
	}
	return f
}

func (f *fakeSystem) deps() Deps {
	return Deps{
		Root: f.root,
		Has:  func(name string) bool { return f.tools[name] },
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
			f.calls = append(f.calls, cmd)
			if f.onCall != nil {
				f.onCall(cmd)
			}
			if f.fail[cmd] {
				return []byte(f.replies[cmd]), errors.New("salida distinta de cero")
			}
			return []byte(f.replies[cmd]), nil
		},
		Exec: func(_ context.Context, s exec.Spec) exec.Result {
			f.calls = append(f.calls, "exec "+s.Content)
			code := 0
			fmt.Sscanf(f.replies["exec "+s.Content], "%d", &code)
			return exec.Result{ExitCode: code, Stdout: "detalle de " + s.Content + "\n"}
		},
	}
}

func (f *fakeSystem) write(rel, content string) {
	p := filepath.Join(f.root, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeSystem) read(rel string) string {
	b, _ := os.ReadFile(filepath.Join(f.root, rel))
	return string(b)
}

func (f *fakeSystem) called(cmd string) bool {
	for _, c := range f.calls {
		if c == cmd {
			return true
		}
	}
	return false
}
