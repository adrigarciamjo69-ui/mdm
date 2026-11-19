package policy

import (
	"testing"

	"github.com/google/uuid"
)

var (
	gLaptops = uuid.New()
	gServers = uuid.New()
	gOther   = uuid.New()
)

func pol(prio int, platform string, assign []Assignment, settings ...Setting) AssignedPolicy {
	return AssignedPolicy{ID: uuid.New(), Priority: prio, Platform: platform, Settings: settings, Assignments: assign}
}

func inc(g uuid.UUID) Assignment { return Assignment{GroupID: &g} }
func exc(g uuid.UUID) Assignment { return Assignment{GroupID: &g, Exclude: true} }
func all() Assignment            { return Assignment{} }

func TestEffective_PriorityWins(t *testing.T) {
	high := pol(1, "windows", []Assignment{all()}, st("windows.screen_lock", "enforce", `{"minutes":5}`))
	low := pol(2, "windows", []Assignment{all()}, st("windows.screen_lock", "enforce", `{"minutes":15}`), st("windows.firewall", "enforce", `{}`))
	eff, conflicts := Effective([]AssignedPolicy{low, high}, nil, "windows", nil)
	if len(eff.Settings) != 2 {
		t.Fatalf("ajustes: %+v", eff.Settings)
	}
	for _, s := range eff.Settings {
		if s.Key == "windows.screen_lock" && (s.PolicyID != high.ID.String() || string(s.Params) != `{"minutes":5}`) {
			t.Fatalf("ganó la política equivocada: %+v", s)
		}
	}
	if len(conflicts) != 1 || conflicts[0].Key != "windows.screen_lock" || conflicts[0].Winner != high.ID || conflicts[0].Loser != low.ID {
		t.Fatalf("conflictos: %+v", conflicts)
	}
	if eff.EvaluateIntervalS != 900 || eff.Hash == "" {
		t.Fatalf("efectiva: %+v", eff)
	}
}

func TestEffective_ExcludeBeatsInclude(t *testing.T) {
	p := pol(1, "windows", []Assignment{inc(gLaptops), exc(gServers)}, st("windows.firewall", "enforce", `{}`))
	eff, _ := Effective([]AssignedPolicy{p}, []uuid.UUID{gLaptops, gServers}, "windows", nil)
	if len(eff.Settings) != 0 {
		t.Fatal("la exclusión debe ganar a la inclusión")
	}
	eff, _ = Effective([]AssignedPolicy{p}, []uuid.UUID{gLaptops}, "windows", nil)
	if len(eff.Settings) != 1 {
		t.Fatal("incluido y no excluido debe recibirla")
	}
	p2 := pol(1, "windows", []Assignment{all(), exc(gServers)}, st("windows.firewall", "enforce", `{}`))
	if eff, _ := Effective([]AssignedPolicy{p2}, []uuid.UUID{gServers}, "windows", nil); len(eff.Settings) != 0 {
		t.Fatal("todos menos los excluidos")
	}
}

func TestEffective_GroupTargeting(t *testing.T) {
	p := pol(1, "linux", []Assignment{inc(gServers)}, st("linux.firewall", "enforce", `{}`))
	if eff, _ := Effective([]AssignedPolicy{p}, []uuid.UUID{gOther}, "linux", nil); len(eff.Settings) != 0 {
		t.Fatal("fuera del grupo no debe recibirla")
	}
	if eff, _ := Effective([]AssignedPolicy{p}, nil, "linux", nil); len(eff.Settings) != 0 {
		t.Fatal("sin grupos no debe recibirla")
	}
	unassigned := pol(2, "linux", nil, st("linux.ssh", "enforce", `{}`))
	if eff, _ := Effective([]AssignedPolicy{unassigned}, []uuid.UUID{gServers}, "linux", nil); len(eff.Settings) != 0 {
		t.Fatal("una política sin asignar no se aplica a nadie")
	}
}

func TestEffective_OtherPlatformIgnored(t *testing.T) {
	p := pol(1, "windows", []Assignment{all()}, st("windows.firewall", "enforce", `{}`))
	if eff, _ := Effective([]AssignedPolicy{p}, nil, "linux", nil); len(eff.Settings) != 0 {
		t.Fatal("una política de Windows no aplica a Linux")
	}
}

func TestEffective_CustomScriptsFromDifferentPoliciesCoexist(t *testing.T) {
	a := pol(1, "linux", []Assignment{all()}, st("custom.script", "audit", `{"interpreter":"sh","detect":"exit 0"}`))
	b := pol(2, "linux", []Assignment{all()}, st("custom.script", "audit", `{"interpreter":"sh","detect":"exit 1"}`))
	eff, conflicts := Effective([]AssignedPolicy{a, b}, nil, "linux", nil)
	if len(eff.Settings) != 2 || len(conflicts) != 0 {
		t.Fatalf("los scripts propios de políticas distintas no compiten: %+v %+v", eff.Settings, conflicts)
	}
}

func TestEffective_HashStable(t *testing.T) {
	a := pol(1, "windows", []Assignment{all()}, st("windows.firewall", "enforce", `{}`))
	b := pol(2, "windows", []Assignment{inc(gLaptops)}, st("windows.smb1", "enforce", `{}`))
	e1, _ := Effective([]AssignedPolicy{a, b}, []uuid.UUID{gLaptops}, "windows", []string{"k1"})
	b.Assignments = []Assignment{inc(gOther), inc(gLaptops)}
	e2, _ := Effective([]AssignedPolicy{b, a}, []uuid.UUID{gLaptops}, "windows", []string{"k1"})
	if e1.Hash != e2.Hash {
		t.Fatal("la huella depende del orden de políticas o asignaciones")
	}
	e3, _ := Effective([]AssignedPolicy{a, b}, []uuid.UUID{gLaptops}, "windows", []string{"k1", "k2"})
	if e3.Hash == e1.Hash || len(e3.EscrowedKeyIDs) != 2 {
		t.Fatal("custodiar una clave debe cambiar la huella")
	}
}

func TestEffective_EmptyIsNotNil(t *testing.T) {
	eff, conflicts := Effective(nil, nil, "linux", nil)
	if eff.Settings == nil || eff.EscrowedKeyIDs == nil || conflicts == nil {
		t.Fatal("las listas vacías deben serializarse como [], no null")
	}
}
