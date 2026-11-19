// Catálogo de ajustes de ejemplo, como lo devuelve GET /policy-catalog.
export const windowsCatalog = [
  { key: "windows.bitlocker", platform: "windows", title: "BitLocker obligatorio", description: "Cifra la unidad.", audit_only: false,
    fields: [{ name: "escrow_key", label: "Custodiar la clave", type: "bool", required: false, default: true }] },
  { key: "windows.firewall", platform: "windows", title: "Cortafuegos activado", description: "Tres perfiles.", audit_only: false, fields: [] },
  { key: "windows.screen_lock", platform: "windows", title: "Bloqueo de pantalla por inactividad", description: "Bloqueo.", audit_only: false,
    fields: [{ name: "minutes", label: "Minutos", type: "int", required: true, min: 1, max: 60 }] },
];
export const linuxCatalog = [
  { key: "linux.firewall", platform: "linux", title: "Cortafuegos activado", description: "ufw o firewalld.", audit_only: false, fields: [] },
  { key: "linux.disk_encryption", platform: "linux", title: "Cifrado de disco", description: "LUKS.", audit_only: true, fields: [] },
  { key: "linux.forbidden_packages", platform: "linux", title: "Paquetes prohibidos", description: "Lista.", audit_only: true,
    fields: [{ name: "names", label: "Paquetes", type: "string_list", required: true, min: 1, max: 50 }] },
];
