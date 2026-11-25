package protocol

import "time"

// Tipos de orden.
const (
	CmdReboot           = "reboot"
	CmdShutdown         = "shutdown"
	CmdRefreshInventory = "refresh_inventory"
	CmdEvaluatePolicies = "evaluate_policies"
	CmdRunScript        = "run_script"
	CmdRetire           = "retire"
	// Fase 3.
	CmdScanUpdates       = "scan_updates"
	CmdInstallUpdatesNow = "install_updates_now"
	// Administrador temporal: el agente da y quita el permiso él mismo.
	CmdTempAdmin       = "temp_admin"
	CmdTempAdminRevoke = "temp_admin_revoke"
	// Android (los ejecuta Google, no el agente).
	CmdLock          = "lock"
	CmdResetPassword = "reset_password"
	CmdLostModeStart = "lost_mode_start"
	CmdLostModeStop  = "lost_mode_stop"
	CmdWipe          = "wipe"
)

// AndroidCommands son las órdenes que admite un móvil Android.
var AndroidCommands = map[string]bool{CmdReboot: true, CmdLock: true, CmdResetPassword: true, CmdLostModeStart: true, CmdLostModeStop: true, CmdWipe: true}

// Estados de una orden.
const (
	CommandPending   = "pending"
	CommandSent      = "sent"
	CommandRunning   = "running"
	CommandSucceeded = "succeeded"
	CommandFailed    = "failed"
	CommandExpired   = "expired"
	CommandCancelled = "cancelled"
)

// Capacidades que declara el agente en el check-in.
const (
	CapPolicy     = "policy.v1"
	CapCommands   = "commands.v1"
	CapScripts    = "scripts.v1"
	CapBitLocker  = "bitlocker.v1"
	CapSelfUpdate = "selfupdate.v1"
	CapApps       = "apps.v1"
	CapPatches    = "patches.v1"
	CapTempAdmin  = "tempadmin.v1"
)

// RequiredCapability devuelve la capacidad que necesita el agente para ejecutar el tipo de orden.
func RequiredCapability(cmdType string) string {
	switch cmdType {
	case CmdRunScript:
		return CapScripts
	case CmdScanUpdates, CmdInstallUpdatesNow:
		return CapPatches
	case CmdTempAdmin, CmdTempAdminRevoke:
		return CapTempAdmin
	}
	return CapCommands
}

type PowerPayload struct {
	DelayS  int    `json:"delay_s"`
	Message string `json:"message"`
}

type ScriptPayload struct {
	Interpreter string            `json:"interpreter"`
	Content     string            `json:"content"`
	TimeoutS    int               `json:"timeout_s"`
	Env         map[string]string `json:"env"`
	ScriptName  string            `json:"script_name"`
	Version     int               `json:"version"`
}

// Límites del administrador temporal.
const (
	TempAdminMinMinutes = 15
	TempAdminMaxMinutes = 480
)

// TempAdminPayload da permisos de administrador local durante unos minutos.
// User vacío es el usuario con la sesión iniciada.
type TempAdminPayload struct {
	User    string `json:"user"`
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
}

// TempAdminRevokePayload quita el permiso antes de tiempo. User vacío quita
// todos los permisos temporales del equipo.
type TempAdminRevokePayload struct {
	User string `json:"user"`
}

type CommandResult struct {
	Status     string    `json:"status"`
	ExitCode   *int      `json:"exit_code"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	Error      string    `json:"error"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}
