package agentapi

import (
	"testing"

	"mdm/internal/protocol"
)

type fakeReleases struct{ version string }

func (f fakeReleases) Version() string { return f.version }
func (f fakeReleases) For(platform, arch string) (protocol.AgentUpdate, bool) {
	if platform == "windows" && arch == "amd64" {
		return protocol.AgentUpdate{Version: f.version, URL: "/downloads/mdm-agent-windows-amd64.exe", SHA256: "abc"}, true
	}
	return protocol.AgentUpdate{}, false
}

var updCaps = []string{protocol.CapCommands, protocol.CapSelfUpdate}

func TestCheckin_OffersUpdate(t *testing.T) {
	a := newAgentAPI(t, withReleases(fakeReleases{"1.1.0"}))
	c := a.enrolled() // windows amd64, versión 1.0.0
	_, out := c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.0", Capabilities: updCaps})
	if out.AgentUpdate == nil || out.AgentUpdate.Version != "1.1.0" || out.AgentUpdate.SHA256 != "abc" {
		t.Fatalf("actualización: %+v", out.AgentUpdate)
	}
}

func TestCheckin_NoUpdateWhen(t *testing.T) {
	a := newAgentAPI(t, withReleases(fakeReleases{"1.1.0"}))
	c := a.enrolled()
	cases := map[string]func(){
		"misma versión":   func() {},
		"sin capacidad":   func() {},
		"desactivado":     func() { a.setAutoUpdate(false) },
		"grupo congelado": func() { a.setAutoUpdate(true); a.pinDeviceGroup(c, "1.0.0") },
	}
	if _, out := c.checkin(protocol.CheckinRequest{AgentVersion: "1.1.0", Capabilities: updCaps}); out.AgentUpdate != nil {
		t.Error("misma versión")
	}
	if _, out := c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.0", Capabilities: []string{protocol.CapCommands}}); out.AgentUpdate != nil {
		t.Error("un agente sin selfupdate.v1 no puede actualizarse")
	}
	cases["desactivado"]()
	if _, out := c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.0", Capabilities: updCaps}); out.AgentUpdate != nil {
		t.Error("actualización automática desactivada")
	}
	cases["grupo congelado"]()
	if _, out := c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.0", Capabilities: updCaps}); out.AgentUpdate != nil {
		t.Error("grupo con la versión congelada")
	}
	linux := a.enrolledAs("linux", "amd64")
	if _, out := linux.checkin(protocol.CheckinRequest{AgentVersion: "1.0.0", Capabilities: updCaps}); out.AgentUpdate != nil {
		t.Error("sin binario para su plataforma no se ofrece nada")
	}
}

func TestCheckin_AuditsVersionChange(t *testing.T) {
	a := newAgentAPI(t, withReleases(fakeReleases{"1.1.0"}))
	c := a.enrolled()
	c.checkin(protocol.CheckinRequest{AgentVersion: "1.0.0"})
	if len(a.audit("agent.updated")) != 0 {
		t.Fatal("misma versión que la inscripción: no hay cambio")
	}
	c.checkin(protocol.CheckinRequest{AgentVersion: "1.1.0"})
	es := a.audit("agent.updated")
	if len(es) != 1 || es[0].TargetID != c.id.String() {
		t.Fatalf("auditoría: %+v", es)
	}
}
