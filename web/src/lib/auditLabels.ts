// Frase en español para cada acción del registro de auditoría.
const LABELS: Record<string, string> = {
  "login.ok": "Inició sesión",
  "login.failed": "Intento de inicio de sesión fallido",
  logout: "Cerró sesión",
  "totp.enrolled": "Configuró el segundo factor",
  "admin.created": "Creó una cuenta de administrador",
  "admin.updated": "Modificó una cuenta de administrador",
  "password.changed": "Cambió su contraseña",
  "password.reset": "Restableció la contraseña de una cuenta",
  "token.created": "Creó un token de inscripción",
  "token.revoked": "Revocó un token de inscripción",
  "device.enrolled": "Se inscribió un dispositivo",
  "device.reenrolled": "Se reinscribió un dispositivo",
  "device.updated": "Modificó un dispositivo",
  "device.revoked": "Revocó un dispositivo",
  "device.deleted": "Eliminó un dispositivo",
  "group.created": "Creó un grupo",
  "group.updated": "Modificó un grupo",
  "group.deleted": "Eliminó un grupo",
  "devices.exported": "Exportó el listado de dispositivos",
  "settings.updated": "Cambió los ajustes globales",
  "policy.created": "Creó una política",
  "policy.updated": "Modificó una política",
  "policy.deleted": "Eliminó una política",
  "policy.assigned": "Cambió la asignación de una política",
  "batch.created": "Envió una orden a equipos",
  "command.cancelled": "Canceló una orden",
  "device.retired": "Equipo retirado",
  "script.created": "Creó un script",
  "script.updated": "Modificó un script",
  "script.archived": "Archivó un script",
  "recovery_key.viewed": "Consultó una clave de recuperación de BitLocker",
  "agent.updated": "Agente actualizado",
  "agent.pinned": "Congeló o descongeló la versión del agente en un grupo",
};

export const AUDIT_ACTIONS = Object.keys(LABELS);

export function auditLabel(action: string): string {
  return LABELS[action] ?? action;
}
