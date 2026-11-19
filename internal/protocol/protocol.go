// Package protocol define los mensajes JSON que intercambian el agente y el
// servidor. Es el único paquete que ambos comparten.
package protocol

import (
	"encoding/json"
	"time"
)

const (
	DefaultCheckinIntervalS   = 60
	DefaultInventoryIntervalS = 21600

	PlatformWindows = "windows"
	PlatformLinux   = "linux"

	SectionSystem   = "system"
	SectionHardware = "hardware"
	SectionDisks    = "disks"
	SectionNetwork  = "network"
	SectionSecurity = "security"
	SectionSoftware = "software"

	StateOn      = "on"
	StateOff     = "off"
	StatePartial = "partial"
	StateUnknown = "unknown"
)

type EnrollRequest struct {
	Token        string `json:"token"`
	HardwareID   string `json:"hardware_id"`
	Hostname     string `json:"hostname"`
	Platform     string `json:"platform"`
	OSName       string `json:"os_name"`
	OSVersion    string `json:"os_version"`
	Arch         string `json:"arch"`
	Serial       string `json:"serial"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	AgentVersion string `json:"agent_version"`
}

type EnrollResponse struct {
	DeviceID           string `json:"device_id"`
	Credential         string `json:"credential"`
	CheckinIntervalS   int    `json:"checkin_interval_s"`
	InventoryIntervalS int    `json:"inventory_interval_s"`
}

type CheckinRequest struct {
	AgentVersion  string `json:"agent_version"`
	UptimeS       int64  `json:"uptime_s"`
	InventoryHash string `json:"inventory_hash"`
	// Capabilities son las funciones que esta versión del agente soporta.
	Capabilities []string `json:"capabilities"`
	// PolicyHash es la huella de la política que el agente tiene evaluada.
	PolicyHash string `json:"policy_hash"`
}

type AgentConfig struct {
	CheckinIntervalS   int `json:"checkin_interval_s"`
	InventoryIntervalS int `json:"inventory_interval_s"`
}

// Command es una orden puntual que el servidor entrega en el check-in.
type Command struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	ExpiresAt time.Time       `json:"expires_at"`
}

type CheckinResponse struct {
	Config        AgentConfig `json:"config"`
	SendInventory bool        `json:"send_inventory"`
	Commands      []Command   `json:"commands"`
	// PolicyHash es la huella de la política efectiva vigente del equipo.
	PolicyHash string `json:"policy_hash"`
	// AgentUpdate indica que hay una versión del agente que instalar.
	AgentUpdate *AgentUpdate `json:"agent_update,omitempty"`
}

type AgentUpdate struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// MarshalJSON garantiza que commands se emite como [] y nunca como null.
func (r CheckinResponse) MarshalJSON() ([]byte, error) {
	type plain CheckinResponse
	p := plain(r)
	if p.Commands == nil {
		p.Commands = []Command{}
	}
	return json.Marshal(p)
}

type Inventory struct {
	CollectedAt time.Time  `json:"collected_at"`
	Hash        string     `json:"hash"`
	Unavailable []string   `json:"unavailable"`
	System      System     `json:"system"`
	Hardware    Hardware   `json:"hardware"`
	Disks       []Disk     `json:"disks"`
	Network     []NetIf    `json:"network"`
	Security    Security   `json:"security"`
	Software    []Software `json:"software"`
	// Mobile solo existe en los móviles (Android).
	Mobile *Mobile `json:"mobile,omitempty"`
}

// Mobile son los datos propios de un móvil gestionado por Google.
type Mobile struct {
	State              string `json:"state"`
	IMEI               string `json:"imei"`
	MEID               string `json:"meid"`
	PhoneNumber        string `json:"phone_number"`
	Carrier            string `json:"carrier"`
	SecurityPatchLevel string `json:"security_patch_level"`
	BuildNumber        string `json:"build_number"`
	Integrity          string `json:"integrity"` // SECURE, AT_RISK, POTENTIALLY_COMPROMISED…
	DeviceSecure       bool   `json:"device_secure"`
	DeveloperOptions   bool   `json:"developer_options"`
	UnknownSources     bool   `json:"unknown_sources"`
	StorageBytes       uint64 `json:"storage_bytes"`
	PolicyApplied      bool   `json:"policy_applied"`
	LastStatusReport   string `json:"last_status_report"`
}

type System struct {
	Hostname     string `json:"hostname"`
	OSName       string `json:"os_name"`
	OSVersion    string `json:"os_version"`
	Kernel       string `json:"kernel"`
	Arch         string `json:"arch"`
	BootTime     string `json:"boot_time"`
	Timezone     string `json:"timezone"`
	Domain       string `json:"domain"`
	LoggedInUser string `json:"logged_in_user"`
}

type Hardware struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Serial       string `json:"serial"`
	BIOSVendor   string `json:"bios_vendor"`
	BIOSVersion  string `json:"bios_version"`
	BIOSDate     string `json:"bios_date"`
	CPUModel     string `json:"cpu_model"`
	CPUCores     int    `json:"cpu_cores"`
	CPUThreads   int    `json:"cpu_threads"`
	MemoryBytes  uint64 `json:"memory_bytes"`
}

type Disk struct {
	Device     string `json:"device"`
	Mount      string `json:"mount"`
	Filesystem string `json:"filesystem"`
	SizeBytes  uint64 `json:"size_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	Encrypted  bool   `json:"encrypted"`
}

type NetIf struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

type Security struct {
	DiskEncryption   string `json:"disk_encryption"`
	Firewall         string `json:"firewall"`
	Antivirus        string `json:"antivirus"`
	AntivirusProduct string `json:"antivirus_product"`
	SecureBoot       string `json:"secure_boot"`
}

type Software struct {
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Publisher   string     `json:"publisher"`
	InstalledAt *time.Time `json:"installed_at"`
}
