// Package audit escribe el registro de auditoría: quién hizo qué, sobre qué y cuándo.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"mdm/internal/server/store"
)

// Acciones registradas.
const (
	LoginOK         = "login.ok"
	LoginFailed     = "login.failed"
	Logout          = "logout"
	TOTPEnrolled    = "totp.enrolled"
	AdminCreated    = "admin.created"
	AdminUpdated    = "admin.updated"
	PasswordChanged = "password.changed"
	PasswordReset   = "password.reset"
	TokenCreated    = "token.created"
	TokenRevoked    = "token.revoked"
	DeviceEnrolled  = "device.enrolled"
	DeviceReenroll  = "device.reenrolled"
	DeviceUpdated   = "device.updated"
	DeviceRevoked   = "device.revoked"
	DeviceDeleted   = "device.deleted"
	GroupCreated    = "group.created"
	GroupUpdated    = "group.updated"
	GroupDeleted    = "group.deleted"
	DevicesExport   = "devices.exported"

	SettingsUpdated       = "settings.updated"
	PolicyCreated         = "policy.created"
	PolicyUpdated         = "policy.updated"
	PolicyDeleted         = "policy.deleted"
	PolicyAssigned        = "policy.assigned"
	BatchCreated          = "batch.created"
	CommandCancel         = "command.cancelled"
	DeviceRetired         = "device.retired"
	ScriptCreated         = "script.created"
	ScriptUpdated         = "script.updated"
	ScriptArchived        = "script.archived"
	RecoveryKeyViewed     = "recovery_key.viewed"
	AgentUpdated          = "agent.updated"
	AgentPinned           = "agent.pinned"
	FileUploaded          = "file.uploaded"
	AppCreated            = "app.created"
	AppUpdated            = "app.updated"
	AppArchived           = "app.archived"
	DeploymentCreated     = "deployment.created"
	DeploymentUpdated     = "deployment.updated"
	DeploymentDeleted     = "deployment.deleted"
	DeploymentRetried     = "deployment.retried"
	PatchPolicyCreated    = "patch_policy.created"
	PatchPolicyUpdated    = "patch_policy.updated"
	PatchPolicyDeleted    = "patch_policy.deleted"
	UpdateApproved        = "update.approved"
	UpdateDeclined        = "update.declined"
	UpdateReset           = "update.reset"
	AndroidConfigured     = "android.configured"
	AndroidLinked         = "android.linked"
	AndroidUnlinked       = "android.unlinked"
	AndroidTokenCreated   = "android.token_created"
	AndroidTokenRevoked   = "android.token_revoked"
	EmailConfigured       = "email.configured"
	ChannelCreated        = "channel.created"
	ChannelUpdated        = "channel.updated"
	ChannelDeleted        = "channel.deleted"
	AlertRuleCreated      = "alert_rule.created"
	AlertRuleUpdated      = "alert_rule.updated"
	AlertRuleDeleted      = "alert_rule.deleted"
	AlertAcknowledged     = "alert.acknowledged"
	AlertResolved         = "alert.resolved"
	ReportExported        = "report.exported"
	ReportScheduleCreated = "report_schedule.created"
	ReportScheduleUpdated = "report_schedule.updated"
	ReportScheduleDeleted = "report_schedule.deleted"
	AutomationCreated     = "automation.created"
	AutomationUpdated     = "automation.updated"
	AutomationDeleted     = "automation.deleted"

	AssetCreated          = "asset.created"
	AssetUpdated          = "asset.updated"
	AssetTrashed          = "asset.trashed"
	AssetRestored         = "asset.restored"
	AssetPurged           = "asset.purged"
	AssetLinked           = "asset.linked"
	AssetUnlinked         = "asset.unlinked"
	AssetsBulkUpdated     = "assets.bulk_updated"
	AssetsImported        = "assets.imported"
	AssetsExported        = "assets.exported"
	AssetDocumentAdded    = "asset_document.added"
	AssetDocumentDeleted  = "asset_document.deleted"
	CategoryCreated       = "asset_category.created"
	CategoryUpdated       = "asset_category.updated"
	CategoryDeleted       = "asset_category.deleted"
	DirectoryConfigured   = "directory.configured"
	DirectorySynced       = "directory.synced"
	DirectoryUserUpdated  = "directory_user.updated"
	LicenseCreated        = "license.created"
	LicenseUpdated        = "license.updated"
	LicenseDeleted        = "license.deleted"
	LicenseKeyViewed      = "license.key_viewed"
	LicenseAssigned       = "license.assigned"
	LicenseUnassigned     = "license.unassigned"
	DeliveryCreated       = "delivery.created"
	DeliveryUpdated       = "delivery.updated"
	DeliveryDeleted       = "delivery.deleted"
	DeliveryConfirmed     = "delivery.confirmed"
	DeliveryCancelled     = "delivery.cancelled"
	DeliverySettingsSaved = "delivery.settings_updated"
)

// Actions lista todas las acciones, para validar filtros y poblar la consola.
var Actions = []string{LoginOK, LoginFailed, Logout, TOTPEnrolled, AdminCreated, AdminUpdated, PasswordChanged, PasswordReset,
	TokenCreated, TokenRevoked, DeviceEnrolled, DeviceReenroll, DeviceUpdated, DeviceRevoked,
	DeviceDeleted, GroupCreated, GroupUpdated, GroupDeleted, DevicesExport, SettingsUpdated,
	PolicyCreated, PolicyUpdated, PolicyDeleted, PolicyAssigned, BatchCreated, CommandCancel, DeviceRetired,
	ScriptCreated, ScriptUpdated, ScriptArchived, RecoveryKeyViewed, AgentUpdated, AgentPinned, FileUploaded, AppCreated, AppUpdated, AppArchived,
	DeploymentCreated, DeploymentUpdated, DeploymentDeleted, DeploymentRetried,
	PatchPolicyCreated, PatchPolicyUpdated, PatchPolicyDeleted, UpdateApproved, UpdateDeclined, UpdateReset,
	AndroidConfigured, AndroidLinked, AndroidUnlinked, AndroidTokenCreated, AndroidTokenRevoked,
	EmailConfigured, ChannelCreated, ChannelUpdated, ChannelDeleted,
	AlertRuleCreated, AlertRuleUpdated, AlertRuleDeleted, AlertAcknowledged, AlertResolved,
	ReportExported, ReportScheduleCreated, ReportScheduleUpdated, ReportScheduleDeleted,
	AutomationCreated, AutomationUpdated, AutomationDeleted,
	AssetCreated, AssetUpdated, AssetTrashed, AssetRestored, AssetPurged, AssetLinked, AssetUnlinked,
	AssetsBulkUpdated, AssetsImported, AssetsExported, AssetDocumentAdded, AssetDocumentDeleted,
	CategoryCreated, CategoryUpdated, CategoryDeleted, DirectoryConfigured, DirectorySynced, DirectoryUserUpdated,
	LicenseCreated, LicenseUpdated, LicenseDeleted, LicenseKeyViewed, LicenseAssigned, LicenseUnassigned,
	DeliveryCreated, DeliveryUpdated, DeliveryDeleted, DeliveryConfirmed, DeliveryCancelled, DeliverySettingsSaved,
}

type Entry struct {
	// Actor es el administrador que actúa; nil cuando actúa el sistema o un agente.
	Actor *store.Admin
	// ActorEmail se usa cuando no hay Actor (por ejemplo, un inicio de sesión
	// fallido con un correo que no existe).
	ActorEmail string
	Action     string
	TargetType string
	TargetID   string
	Details    map[string]any
	IP         string
}

type Logger struct {
	store *store.Store
	now   func() time.Time
}

func New(s *store.Store) *Logger { return &Logger{store: s, now: time.Now} }

// WithClock devuelve un Logger que fecha las entradas con el reloj dado.
func (l *Logger) WithClock(now func() time.Time) *Logger { return &Logger{store: l.store, now: now} }

// Log escribe la entrada. Un fallo al escribir se anota en el registro del
// servidor pero no interrumpe la petición que lo originó.
func (l *Logger) Log(ctx context.Context, e Entry) {
	row := store.AuditEntry{
		At:         l.now().UTC(),
		ActorEmail: e.ActorEmail,
		Action:     e.Action,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
		IP:         e.IP,
	}
	if e.Actor != nil {
		id := e.Actor.ID
		row.ActorAdminID = &id
		row.ActorEmail = e.Actor.Email
	}
	if len(e.Details) > 0 {
		b, err := json.Marshal(e.Details)
		if err != nil {
			slog.Error("auditoría: detalles no serializables", "action", e.Action, "err", err)
		} else {
			row.Details = b
		}
	}
	// La auditoría se escribe aunque el cliente haya cortado la conexión.
	if err := l.store.InsertAudit(context.WithoutCancel(ctx), row); err != nil {
		slog.Error("auditoría: no se pudo escribir", "action", e.Action, "err", err)
	}
}
