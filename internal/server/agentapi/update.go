package agentapi

import (
	"context"
	"slices"

	"mdm/internal/protocol"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
)

// Releases indica qué versión del agente se distribuye y dónde descargarla.
type Releases interface {
	Version() string
	For(platform, arch string) (protocol.AgentUpdate, bool)
}

// updateFor decide si ofrecer una actualización al equipo en este check-in.
func (a *api) updateFor(ctx context.Context, dev store.Device, agentVersion string, caps []string) (*protocol.AgentUpdate, error) {
	if a.d.Releases == nil || !slices.Contains(caps, protocol.CapSelfUpdate) {
		return nil, nil
	}
	u, ok := a.d.Releases.For(dev.Platform, dev.Arch)
	if !ok || u.Version == agentVersion {
		return nil, nil
	}
	v, err := settings.Load(ctx, a.d.Store)
	if err != nil || !v.AgentAutoUpdate {
		return nil, err
	}
	pinned, err := a.d.Store.DevicePinned(ctx, dev.ID)
	if err != nil || pinned {
		return nil, err
	}
	return &u, nil
}
