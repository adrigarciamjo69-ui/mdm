package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Admin struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	TOTPSecret   *string
	Role         string
	Disabled     bool
	CreatedAt    time.Time
	LastLoginAt  *time.Time
}

// Session representa una sesión de administrador. TokenHash es el SHA-256 del
// identificador que viaja en la cookie.
type Session struct {
	TokenHash string
	AdminID   uuid.UUID
	CSRFToken string
	Verified  bool
	// PendingTOTP es el secreto propuesto durante el alta del segundo factor.
	PendingTOTP *string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	IP          string
}

type Group struct {
	ID          uuid.UUID
	Name        string
	Description string
	// Kind es GroupStatic (grupo principal, elegido a mano) o GroupDynamic (calculado por Rule).
	Kind      string
	Rule      json.RawMessage
	CreatedAt time.Time
}

const (
	GroupStatic  = "static"
	GroupDynamic = "dynamic"
)

type EnrollmentToken struct {
	ID        uuid.UUID
	Name      string
	TokenHash string
	GroupID   *uuid.UUID
	ExpiresAt *time.Time
	MaxUses   *int
	Uses      int
	RevokedAt *time.Time
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

type Device struct {
	ID             uuid.UUID
	HardwareID     string
	Hostname       string
	Platform       string
	OSName         string
	OSVersion      string
	Arch           string
	Serial         string
	Manufacturer   string
	Model          string
	AgentVersion   string
	CredentialHash string
	RevokedAt      *time.Time
	GroupID        *uuid.UUID
	AssignedUser   string
	Tags           []string
	EnrolledAt     time.Time
	LastSeenAt     *time.Time
	LastIP         string
	InventoryHash  string
	InventoryAt    *time.Time
	Capabilities   []string
}

type AuditEntry struct {
	ID           int64
	At           time.Time
	ActorAdminID *uuid.UUID
	ActorEmail   string
	Action       string
	TargetType   string
	TargetID     string
	Details      json.RawMessage
	IP           string
}
