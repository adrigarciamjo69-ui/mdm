package patches

import (
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func days(n int) *time.Time { t := now.Add(-time.Duration(n) * 24 * time.Hour); return &t }

func upd(id, class string, published *time.Time) Missing {
	return Missing{ID: id, KB: "KB" + id, Classification: class, Security: class == "security", PublishedAt: published, FirstSeenAt: now.Add(-time.Hour)}
}

func TestApproved_AutoByClassification(t *testing.T) {
	pol := Policy{Platform: "windows", AutoApprove: []string{"security", "critical"}}
	got := Approved([]Missing{upd("1", "security", days(1)), upd("2", "driver", days(1)), upd("3", "critical", days(1))}, pol, nil, now)
	if !slices.Equal(got, []string{"1", "3"}) {
		t.Fatalf("%v", got)
	}
}

func TestApproved_DeferDays(t *testing.T) {
	pol := Policy{Platform: "windows", AutoApprove: []string{"security"}, DeferDays: 7}
	m := []Missing{upd("seis", "security", days(6)), upd("ocho", "security", days(8))}
	noDate := upd("sin-fecha", "security", nil)
	noDate.FirstSeenAt = now.Add(-8 * 24 * time.Hour)
	recentNoDate := upd("sin-fecha-reciente", "security", nil)
	got := Approved(append(m, noDate, recentNoDate), pol, nil, now)
	if !slices.Equal(got, []string{"ocho", "sin-fecha"}) {
		t.Fatalf("%v", got)
	}
}

func TestApproved_ManualOverrides(t *testing.T) {
	pol := Policy{Platform: "windows", AutoApprove: []string{"security"}, DeferDays: 7}
	m := []Missing{upd("rechazada", "security", days(30)), upd("aprobada", "driver", days(30)), upd("aprobada-nueva", "driver", days(2))}
	got := Approved(m, pol, map[string]string{"rechazada": "declined", "aprobada": "approved", "aprobada-nueva": "approved"}, now)
	if !slices.Equal(got, []string{"aprobada"}) {
		t.Fatalf("rechazar a mano gana a la regla; aprobar a mano respeta el aplazamiento: %v", got)
	}
}

func TestApproved_ExcludedKBs(t *testing.T) {
	pol := Policy{Platform: "windows", AutoApprove: []string{"security"}, ExcludedKBs: []string{"KB1", "kb3"}}
	got := Approved([]Missing{upd("1", "security", days(1)), upd("2", "security", days(1)), upd("3", "security", days(1))}, pol, map[string]string{"1": "approved"}, now)
	if !slices.Equal(got, []string{"2"}) {
		t.Fatalf("las KB excluidas nunca se aprueban, ni a mano: %v", got)
	}
}

func TestApproved_LinuxScope(t *testing.T) {
	m := []Missing{{ID: "openssl", Security: true, FirstSeenAt: now.Add(-time.Hour)}, {ID: "vim", FirstSeenAt: now.Add(-time.Hour)}}
	sec := Approved(m, Policy{Platform: "linux", LinuxScope: "security"}, map[string]string{"vim": "approved", "openssl": "declined"}, now)
	if !slices.Equal(sec, []string{"openssl"}) {
		t.Fatalf("Linux en modo seguridad (sin decisiones manuales): %v", sec)
	}
	all := Approved(m, Policy{Platform: "linux", LinuxScope: "all"}, nil, now)
	if len(all) != 2 {
		t.Fatalf("Linux en modo todo: %v", all)
	}
}

func TestApproved_NoPolicyNothing(t *testing.T) {
	if got := Approved([]Missing{upd("1", "security", days(9))}, Policy{Platform: "windows"}, nil, now); len(got) != 0 {
		t.Fatalf("sin clasificaciones autoaprobadas ni decisiones, nada: %v", got)
	}
}
