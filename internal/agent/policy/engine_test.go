package policy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mdm/internal/agent/settings"
	"mdm/internal/protocol"
)

type fakeSetting struct {
	compliant  bool
	applyFixes bool
	applyErr   error
	checkErr   error
	sleep      time.Duration
	detail     string
	applied    int
}

func (f *fakeSetting) Check(ctx context.Context) (bool, string, error) {
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
			return false, "", ctx.Err()
		}
	}
	return f.compliant, f.detail, f.checkErr
}

func (f *fakeSetting) Apply(context.Context) error {
	f.applied++
	if f.applyFixes {
		f.compliant = true
	}
	return f.applyErr
}

type fakeAPI struct {
	mu      sync.Mutex
	policy  protocol.EffectivePolicy
	getErr  error
	gets    int
	reports []protocol.ComplianceReport
	keys    []protocol.RecoveryKey
}

func (f *fakeAPI) GetPolicy(context.Context) (protocol.EffectivePolicy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	return f.policy, f.getErr
}

func (f *fakeAPI) SendCompliance(_ context.Context, r protocol.ComplianceReport) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, r)
	return nil
}

func (f *fakeAPI) SendRecoveryKey(_ context.Context, k protocol.RecoveryKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, k)
	return nil
}

func policyWith(hash string, keys ...string) protocol.EffectivePolicy {
	var s []protocol.PolicySetting
	for _, k := range keys {
		mode := "enforce"
		if strings.HasPrefix(k, "audit:") {
			k, mode = strings.TrimPrefix(k, "audit:"), "audit"
		}
		s = append(s, protocol.PolicySetting{PolicyID: "p1", Key: k, Mode: mode, Params: json.RawMessage(`{}`)})
	}
	return protocol.EffectivePolicy{Hash: hash, EvaluateIntervalS: 900, Settings: s}
}

func newEngine(api *fakeAPI, impls map[string]*fakeSetting) *Engine {
	return New(Deps{
		API: api,
		Build: func(s protocol.PolicySetting, _ []string) (settings.Setting, error) {
			f, ok := impls[s.Key]
			if !ok {
				return nil, errors.New("clave desconocida")
			}
			return f, nil
		},
		SettingTimeout: 200 * time.Millisecond,
		Now:            func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) },
	})
}

func resultFor(r protocol.ComplianceReport, key string) protocol.SettingResult {
	for _, x := range r.Results {
		if x.Key == key {
			return x
		}
	}
	return protocol.SettingResult{}
}

func TestEngine_EvaluatesOnHashChange(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h2", "a")}
	e := newEngine(api, map[string]*fakeSetting{"a": {compliant: true}})
	e.Wanted("h2")
	if !e.pending() {
		t.Fatal("una huella distinta debe pedir evaluación")
	}
	e.Evaluate(context.Background())
	if len(api.reports) != 1 || api.reports[0].PolicyHash != "h2" || e.Hash() != "h2" || resultFor(api.reports[0], "a").Status != "compliant" {
		t.Fatalf("informe: %+v", api.reports)
	}
	e.Wanted("h2")
	if e.pending() {
		t.Fatal("la misma huella no debe pedir otra evaluación")
	}
}

func TestEngine_EnforceAppliesAndRechecks(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "a")}
	a := &fakeSetting{applyFixes: true}
	e := newEngine(api, map[string]*fakeSetting{"a": a})
	e.Wanted("h")
	e.Evaluate(context.Background())
	r := resultFor(api.reports[0], "a")
	if r.Status != "compliant" || !r.Applied || a.applied != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestEngine_AuditDoesNotApply(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "audit:a")}
	a := &fakeSetting{applyFixes: true}
	e := newEngine(api, map[string]*fakeSetting{"a": a})
	e.Wanted("h")
	e.Evaluate(context.Background())
	if r := resultFor(api.reports[0], "a"); r.Status != "noncompliant" || a.applied != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestEngine_ApplyErrorIsError(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "a")}
	e := newEngine(api, map[string]*fakeSetting{"a": {applyErr: errors.New("sin permisos")}})
	e.Wanted("h")
	e.Evaluate(context.Background())
	if r := resultFor(api.reports[0], "a"); r.Status != "error" || !strings.Contains(r.Message, "sin permisos") {
		t.Fatalf("%+v", r)
	}
}

func TestEngine_NotApplicableAndUnknownKey(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "a", "desconocida")}
	e := newEngine(api, map[string]*fakeSetting{"a": {checkErr: settings.ErrNotApplicable, detail: "sin TPM"}})
	e.Wanted("h")
	e.Evaluate(context.Background())
	if r := resultFor(api.reports[0], "a"); r.Status != "not_applicable" {
		t.Fatalf("no aplicable: %+v", r)
	}
	if r := resultFor(api.reports[0], "desconocida"); r.Status != "error" {
		t.Fatalf("clave desconocida: %+v", r)
	}
}

func TestEngine_SettingTimeout(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "lento", "b")}
	e := newEngine(api, map[string]*fakeSetting{"lento": {sleep: 5 * time.Second}, "b": {compliant: true}})
	e.Wanted("h")
	start := time.Now()
	e.Evaluate(context.Background())
	if time.Since(start) > 2*time.Second {
		t.Fatal("un ajuste lento no debe bloquear la evaluación")
	}
	if r := resultFor(api.reports[0], "lento"); r.Status != "error" || r.Message != "Tiempo agotado." {
		t.Fatalf("%+v", r)
	}
	if resultFor(api.reports[0], "b").Status != "compliant" {
		t.Fatal("el resto debe evaluarse")
	}
}

func TestEngine_PolicyDownloadFailsKeepsLast(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h1", "a")}
	e := newEngine(api, map[string]*fakeSetting{"a": {compliant: true}})
	e.Wanted("h1")
	e.Evaluate(context.Background())
	api.getErr = errors.New("sin red")
	e.Wanted("h2")
	e.Evaluate(context.Background())
	if e.Hash() != "h1" || len(api.reports) != 2 || api.reports[1].PolicyHash != "h1" {
		t.Fatalf("debe seguir con la última política: hash=%s informes=%d", e.Hash(), len(api.reports))
	}
	if !e.pending() {
		t.Fatal("debe volver a intentarlo")
	}
}

func TestEngine_MessageTruncated(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "audit:a")}
	e := newEngine(api, map[string]*fakeSetting{"a": {detail: strings.Repeat("ñ", 3000)}})
	e.Wanted("h")
	e.Evaluate(context.Background())
	if n := len([]rune(resultFor(api.reports[0], "a").Message)); n != 1024 {
		t.Fatalf("mensaje: %d runas", n)
	}
}

func TestEngine_RecoveryKeySentBeforeReport(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "a")}
	e := newEngine(api, map[string]*fakeSetting{"a": {compliant: true}})
	e.d.Build = func(protocol.PolicySetting, []string) (settings.Setting, error) {
		return escrowSetting{}, nil
	}
	e.Wanted("h")
	e.Evaluate(context.Background())
	if len(api.keys) != 1 || api.keys[0].KeyID != "{K1}" {
		t.Fatalf("claves: %+v", api.keys)
	}
}

type escrowSetting struct{}

func (escrowSetting) Check(context.Context) (bool, string, error) { return true, "", nil }
func (escrowSetting) Apply(context.Context) error                 { return nil }
func (escrowSetting) Recovery() *protocol.RecoveryKey {
	return &protocol.RecoveryKey{Volume: "C:", KeyID: "{K1}", RecoveryPassword: "x"}
}

func TestEngine_RunReevaluatesOnInterval(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "a")}
	e := newEngine(api, map[string]*fakeSetting{"a": {compliant: true}})
	tick := make(chan time.Time)
	e.d.After = func(time.Duration) <-chan time.Time { return tick }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	e.Wanted("h")
	waitReports(t, api, 1)
	tick <- time.Now()
	waitReports(t, api, 2)
	e.Trigger()
	waitReports(t, api, 3)
	cancel()
	<-done
}

func waitReports(t *testing.T, api *fakeAPI, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		got := len(api.reports)
		api.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no llegaron %d informes", n)
}

func TestEngine_OnPolicyReceivesApps(t *testing.T) {
	api := &fakeAPI{policy: policyWith("h", "a")}
	api.policy.Apps = []protocol.AppAssignment{{DeploymentID: "d1"}}
	e := newEngine(api, map[string]*fakeSetting{"a": {compliant: true}})
	var got []protocol.AppAssignment
	e.d.OnPolicy = func(p protocol.EffectivePolicy) { got = p.Apps }
	e.Wanted("h")
	e.Evaluate(context.Background())
	if len(got) != 1 || e.Policy().Hash != "h" {
		t.Fatalf("OnPolicy: %+v", got)
	}
}
