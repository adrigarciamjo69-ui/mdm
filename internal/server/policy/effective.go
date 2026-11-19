package policy

import (
	"sort"

	"github.com/google/uuid"

	"mdm/internal/protocol"
)

// Assignment es un destino de una política: un grupo, o todos los equipos si
// GroupID es nulo. Exclude lo convierte en exclusión.
type Assignment struct {
	GroupID *uuid.UUID
	Exclude bool
}

type AssignedPolicy struct {
	ID          uuid.UUID
	Name        string
	Priority    int
	Platform    string
	Settings    []Setting
	Assignments []Assignment
}

// Conflict indica que dos políticas fijan el mismo ajuste en un equipo.
type Conflict struct {
	Key    string    `json:"key"`
	Winner uuid.UUID `json:"winner"`
	Loser  uuid.UUID `json:"loser"`
}

// Applies informa de si la política aplica a un equipo de la plataforma y
// grupos dados. La exclusión siempre gana a la inclusión.
func Applies(p AssignedPolicy, platform string, groups []uuid.UUID) bool {
	if p.Platform != platform {
		return false
	}
	in := map[uuid.UUID]bool{}
	for _, g := range groups {
		in[g] = true
	}
	included := false
	for _, a := range p.Assignments {
		hit := a.GroupID == nil || in[*a.GroupID]
		if !hit {
			continue
		}
		if a.Exclude {
			return false
		}
		included = true
	}
	return included
}

// Effective calcula la política efectiva de un equipo. Si dos políticas fijan
// el mismo ajuste, gana la de menor número de prioridad. Los scripts
// personalizados de políticas distintas no compiten entre sí.
func Effective(policies []AssignedPolicy, deviceGroups []uuid.UUID, platform string, escrowed []string) (protocol.EffectivePolicy, []Conflict) {
	applicable := []AssignedPolicy{}
	for _, p := range policies {
		if Applies(p, platform, deviceGroups) {
			applicable = append(applicable, p)
		}
	}
	sort.SliceStable(applicable, func(i, j int) bool { return applicable[i].Priority < applicable[j].Priority })

	settings := []protocol.PolicySetting{}
	conflicts := []Conflict{}
	winner := map[string]uuid.UUID{}
	for _, p := range applicable {
		for _, s := range p.Settings {
			if s.Key != protocol.CustomScriptKey {
				if w, taken := winner[s.Key]; taken {
					conflicts = append(conflicts, Conflict{Key: s.Key, Winner: w, Loser: p.ID})
					continue
				}
				winner[s.Key] = p.ID
			}
			settings = append(settings, protocol.PolicySetting{PolicyID: p.ID.String(), Key: s.Key, Mode: s.Mode, Params: s.Params})
		}
	}
	sort.Slice(settings, func(i, j int) bool {
		if settings[i].Key != settings[j].Key {
			return settings[i].Key < settings[j].Key
		}
		return settings[i].PolicyID < settings[j].PolicyID
	})
	esc := append([]string{}, escrowed...)
	sort.Strings(esc)
	return protocol.EffectivePolicy{
		Hash:              protocol.PolicyHash(settings, esc),
		EvaluateIntervalS: protocol.DefaultEvaluateIntervalS,
		Settings:          settings,
		EscrowedKeyIDs:    esc,
	}, conflicts
}
