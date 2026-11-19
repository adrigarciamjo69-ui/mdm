package protocol

import "time"

func sampleInventory() Inventory {
	inst := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	return Inventory{
		CollectedAt: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC),
		Hash:        "sha256:x",
		System:      System{Hostname: "pc-1", OSName: "Ubuntu", OSVersion: "24.04", BootTime: "2026-11-13T08:00:00Z"},
		Hardware:    Hardware{Manufacturer: "Dell", CPUCores: 8, MemoryBytes: 16 << 30},
		Disks: []Disk{
			{Device: "/dev/sda1", Mount: "/", SizeBytes: 500 << 30, FreeBytes: 200 << 30, Encrypted: true},
			{Device: "/dev/sdb1", Mount: "/data", SizeBytes: 1000 << 30, FreeBytes: 900 << 30},
		},
		Network:  []NetIf{{Name: "eth0", MAC: "aa:bb", IPv4: []string{"10.0.0.5"}}, {Name: "wlan0", MAC: "cc:dd"}},
		Security: Security{DiskEncryption: StatePartial, Firewall: StateOn, Antivirus: StateUnknown, SecureBoot: StateOn},
		Software: []Software{{Name: "curl", Version: "8.5"}, {Name: "bash", Version: "5.2", InstalledAt: &inst}},
	}
}
