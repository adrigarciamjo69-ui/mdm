package policy

import (
	"sort"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
)

// AssignedDeployment es un despliegue con su aplicación y su versión vigente,
// listo para calcular qué recibe cada equipo.
type AssignedDeployment struct {
	ID            uuid.UUID
	AppID         string
	AppName       string
	Platform      string
	Intent        string
	Source        string
	PackageID     string
	KeepUpdated   bool
	StartAt       *time.Time
	RespectWindow bool
	Reboot        string
	CreatedAt     time.Time
	// Version es la versión vigente (solo instaladores subidos); nil si no hay.
	Version     *protocol.AppAssignment
	Assignments []Assignment
}

// AppConflict indica que dos despliegues de la misma aplicación chocan en un equipo.
type AppConflict struct {
	AppName string    `json:"app_name"`
	Winner  uuid.UUID `json:"winner"`
	Loser   uuid.UUID `json:"loser"`
}

// EffectiveApps calcula los despliegues que recibe un equipo. Las reglas de
// asignación son las de las políticas. Si dos despliegues de la misma
// aplicación aplican, desinstalar gana a instalar (y, a igualdad, el más
// antiguo). epochs da la época de reintento de cada despliegue en el equipo.
func EffectiveApps(deps []AssignedDeployment, groups []uuid.UUID, platform string, epochs map[uuid.UUID]int) ([]protocol.AppAssignment, []AppConflict) {
	applicable := []AssignedDeployment{}
	for _, d := range deps {
		if d.Platform != platform {
			continue
		}
		if !Applies(AssignedPolicy{Platform: d.Platform, Assignments: d.Assignments}, platform, groups) {
			continue
		}
		if d.Source == "upload" && d.Version == nil {
			continue // sin versión vigente no hay nada que instalar ni desinstalar
		}
		applicable = append(applicable, d)
	}
	sort.SliceStable(applicable, func(i, j int) bool {
		a, b := applicable[i], applicable[j]
		if (a.Intent == "uninstall") != (b.Intent == "uninstall") {
			return a.Intent == "uninstall"
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	out := []protocol.AppAssignment{}
	conflicts := []AppConflict{}
	winner := map[string]uuid.UUID{}
	for _, d := range applicable {
		if w, taken := winner[d.AppID]; taken {
			conflicts = append(conflicts, AppConflict{AppName: d.AppName, Winner: w, Loser: d.ID})
			continue
		}
		winner[d.AppID] = d.ID
		a := protocol.AppAssignment{DeploymentID: d.ID.String(), AppName: d.AppName, Intent: d.Intent, Source: d.Source,
			PackageID: d.PackageID, KeepUpdated: d.KeepUpdated, StartAt: d.StartAt, RespectWindow: d.RespectWindow,
			Reboot: d.Reboot, RetryEpoch: epochs[d.ID]}
		if v := d.Version; v != nil {
			a.Version, a.FileSHA256, a.Size, a.InstallerType = v.Version, v.FileSHA256, v.Size, v.InstallerType
			a.InstallCommand, a.UninstallCommand, a.SuccessCodes, a.Detection = v.InstallCommand, v.UninstallCommand, v.SuccessCodes, v.Detection
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeploymentID < out[j].DeploymentID })
	return out, conflicts
}
