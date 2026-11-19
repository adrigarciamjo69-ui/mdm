import type { Device, DeviceDetail, Inventory, Page, Summary } from "../api/types";

export const me = (role: "admin" | "operator" | "readonly" = "admin") => ({ id: "a1", email: "ana@ejemplo.es", role, csrf_token: "csrf" });

export function device(over: Partial<Device> = {}): Device {
  return {
    id: "d1", hostname: "PC-CONTA-07", platform: "windows", os_name: "Windows 11 Pro", os_version: "10.0.26100",
    arch: "amd64", serial: "PF3K9X", manufacturer: "Lenovo", model: "ThinkPad T14", agent_version: "1.0.0",
    status: "online", group_id: "g1", group_name: "Contabilidad", assigned_user: "ana.garcia", tags: ["planta-2"],
    enrolled_at: "2026-10-01T08:00:00Z", last_seen_at: new Date(Date.now() - 40_000).toISOString(),
    last_ip: "10.0.4.17", inventory_at: "2026-11-13T09:00:00Z",
    ...over,
  };
}

export function page<T>(items: T[], over: Partial<Page<T>> = {}): Page<T> {
  return { items, total: items.length, page: 1, per_page: 25, ...over };
}

export function inventory(over: Partial<Inventory> = {}): Inventory {
  return {
    unavailable: [],
    system: { hostname: "PC-CONTA-07", os_name: "Windows 11 Pro", os_version: "10.0.26100", kernel: "10.0.26100", arch: "amd64",
      boot_time: "2026-11-13T07:58:00Z", timezone: "CET", domain: "EMPRESA", logged_in_user: "EMPRESA\\ana.garcia" },
    hardware: { manufacturer: "Lenovo", model: "ThinkPad T14", serial: "PF3K9X", bios_vendor: "LENOVO", bios_version: "R2EET41W",
      bios_date: "2025-03-11", cpu_model: "AMD Ryzen 7 PRO 7840U", cpu_cores: 8, cpu_threads: 16, memory_bytes: 32 * 2 ** 30 },
    disks: [
      { device: "C:", mount: "C:", filesystem: "NTFS", size_bytes: 512 * 2 ** 30, free_bytes: 200 * 2 ** 30, encrypted: true },
      { device: "D:", mount: "D:", filesystem: "NTFS", size_bytes: 100 * 2 ** 30, free_bytes: 5 * 2 ** 30, encrypted: false },
    ],
    network: [{ name: "Ethernet", mac: "3c:52:82:aa:bb:cc", ipv4: ["10.0.4.17"], ipv6: [] }],
    security: { disk_encryption: "partial", firewall: "on", antivirus: "on", antivirus_product: "Microsoft Defender", secure_boot: "on" },
    ...over,
  };
}

export function detail(over: Partial<DeviceDetail> = {}): DeviceDetail {
  return { ...device(), inventory: inventory(), inventory_collected_at: "2026-11-13T09:00:00Z", ...over };
}

export function summary(over: Partial<Summary> = {}): Summary {
  return {
    total: 412,
    by_status: { online: 351, offline: 38, inactive: 17, revoked: 6 },
    by_platform: { windows: 301, linux: 111 },
    os_versions: [
      { platform: "windows", os_name: "Windows 11 Pro", os_version: "10.0.26100", count: 214 },
      { platform: "linux", os_name: "Ubuntu", os_version: "24.04", count: 87 },
    ],
    low_disk: 9, unencrypted: 0, stale: 23,
    recent_enrollments: [device({ id: "d9", hostname: "PC-NUEVO-01" })],
    ...over,
  };
}
