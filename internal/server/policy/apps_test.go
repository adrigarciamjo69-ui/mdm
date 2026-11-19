package policy

import (
	"testing"

	"github.com/google/uuid"
)

func dep(app string, intent string, assign ...Assignment) AssignedDeployment {
	return AssignedDeployment{ID: uuid.New(), AppID: app, Platform: "linux", Intent: intent, Source: "apt", PackageID: app, Assignments: assign}
}

func TestEffectiveApps_IncludeExcludeAndConflict(t *testing.T) {
	g1, g2 := uuid.New(), uuid.New()
	nginx := dep("nginx", "required", Assignment{GroupID: &g1})
	telnet := dep("telnetd", "required", Assignment{}, Assignment{GroupID: &g2, Exclude: true})
	nginxOff := dep("nginx", "uninstall", Assignment{})
	other := AssignedDeployment{ID: uuid.New(), AppID: "firefox", Platform: "windows", Intent: "required", Source: "winget", Assignments: []Assignment{{}}}

	apps, conflicts := EffectiveApps([]AssignedDeployment{nginx, telnet, other}, []uuid.UUID{g1}, "linux", nil)
	if len(apps) != 2 || len(conflicts) != 0 {
		t.Fatalf("apps: %+v conflictos: %+v", apps, conflicts)
	}
	apps, _ = EffectiveApps([]AssignedDeployment{nginx, telnet}, []uuid.UUID{g1, g2}, "linux", nil)
	if len(apps) != 1 || apps[0].PackageID != "nginx" {
		t.Fatalf("la exclusión debe ganar: %+v", apps)
	}
	apps, conflicts = EffectiveApps([]AssignedDeployment{nginx, nginxOff}, []uuid.UUID{g1}, "linux", nil)
	if len(apps) != 1 || apps[0].Intent != "uninstall" || len(conflicts) != 1 {
		t.Fatalf("desinstalar gana a instalar: %+v %+v", apps, conflicts)
	}
	epochs := map[uuid.UUID]int{nginx.ID: 3}
	apps, _ = EffectiveApps([]AssignedDeployment{nginx}, []uuid.UUID{g1}, "linux", epochs)
	if apps[0].RetryEpoch != 3 {
		t.Fatalf("época de reintento: %+v", apps[0])
	}
}
