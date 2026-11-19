# MDM — Fase 4: Android, móviles de empresa (diseño)

Fecha: 2026-11-16 · Estado: pendiente de revisión · Depende de: fases 2 y 3

## 1. Objetivo

Gestionar móviles y tabletas Android **propiedad de la empresa** (totalmente gestionados) desde la misma consola que los PC: inscripción por QR, inventario, políticas, cumplimiento, aplicaciones de Google Play y acciones remotas, a través de la Android Management API (AMAPI) de Google.

Criterios de éxito:

- Un administrador vincula la empresa con Google en menos de 15 minutos siguiendo la consola.
- Un móvil recién restablecido se inscribe escaneando un QR y aparece en el listado en menos de 5 minutos, en su grupo y con su política.
- Una política Android (contraseña, bloqueo, cámara desactivada) y una aplicación obligatoria se aplican y su cumplimiento se ve en la ficha.
- Bloquear y borrar un móvil desde la consola funciona y queda en la auditoría.

## 2. Alcance

Dentro: vinculación con Google, inscripción por QR, sincronización de dispositivos, inventario Android, políticas Android en el modelo de la fase 2, cumplimiento, aplicaciones de Google Play en el catálogo de la fase 3, órdenes (bloquear, reiniciar, restablecer código, modo perdido, borrar), interfaz de proveedor móvil preparada para iOS.

Fuera: perfil de trabajo en móviles personales, iOS, APK privadas, inscripción automática (*zero-touch*, compatible con el modelo pero no implementada), notificaciones Pub/Sub, ubicación.

## 3. Arquitectura

```
Consola ── mdm-server ── (HTTPS, cuenta de servicio) ── Android Management API ── Android Device Policy ── móvil
```

- Paquete `internal/server/mobile`: interfaz `Provider` con `Enroll`, `Sync`, `ApplyPolicy`, `SendCommand`, `Delete`. Implementación `mobile/android` sobre AMAPI. Un futuro `mobile/ios` implementaría la misma interfaz.
- Cliente de AMAPI: HTTP directo a `androidmanagement.googleapis.com/v1` con un token OAuth2 de la cuenta de servicio (`golang.org/x/oauth2/google`, alcance `https://www.googleapis.com/auth/androidmanagement`). Se evita la biblioteca generada completa; solo se modelan los recursos usados.
- Todas las llamadas salen de la cola de tareas (fase 2): reintentos, espera ante `429`/`5xx` respetando `Retry-After`, y ninguna llamada a Google dentro de una petición de la consola salvo la vinculación y la creación de tokens de inscripción (que el administrador espera ver).

## 4. Vinculación con Google

Pantalla **Integraciones › Android** con los pasos y su estado:

1. Instrucciones para crear el proyecto de Google Cloud, activar la API y crear una cuenta de servicio con su clave JSON.
2. Subir la clave JSON. Se valida (formato, `project_id`, que pueda obtener un token) y se guarda cifrada (`secrets.Seal`). Tabla `android_config` (fila única): `service_account` (cifrado), `project_id`, `enterprise_name`, `enterprise_display_name`, `linked_at`, `linked_by`.
3. **Vincular empresa**: el servidor llama a `signupUrls.create` con `callbackUrl = MDM_PUBLIC_URL/api/v1/integrations/android/callback` y guarda el `signupUrl.name` asociado a la sesión. El administrador completa el registro en Google y vuelve con `enterpriseToken`; el servidor llama a `enterprises.create` y guarda `enterprise_name`.
4. Estado final con el nombre de la empresa, y **Desvincular** (borra la empresa en Google tras confirmar escribiendo su nombre; los dispositivos dejan de estar gestionados).

La consola avisa de que Google aplica límites a los proyectos nuevos y puede exigir una revisión antes del uso en producción, y enlaza a la documentación de Google.

## 5. Inscripción

**Inscripción › Android** (con la empresa vinculada): crear token con `name`, `group_id` (grupo principal de destino), `policy_ids` iniciales opcionales (se suman a las asignadas por grupo), `duration` (1 h, 1 día, 7 días, 30 días), `one_time` (bool).

- El servidor llama a `enterprises.enrollmentTokens.create` con `policyName` de la política Android efectiva del grupo de destino (§7), `duration`, `oneTimeOnly` y `additionalData` = id interno del token. Guarda `android_enrollment_tokens`: `id`, `name`, `amapi_name`, `qr_code` (JSON devuelto por Google), `group_id`, `expires_at`, `one_time`, `revoked_at`, `created_by`.
- La consola dibuja el QR a partir de `qr_code` y explica el procedimiento: restablecer de fábrica, pulsar seis veces en la pantalla de bienvenida, conectar a una red, escanear.
- Revocar llama a `enrollmentTokens.delete`.
- Cuando la sincronización encuentra un dispositivo nuevo, lee `enrollmentTokenData` para saber su grupo y lo crea en `devices` con `platform = 'android'`. Auditoría `device.enrolled` con el token.

## 6. Dispositivos e inventario

`devices` gana `amapi_name` (único, nulo para PC). `platform` admite `android`. `credential_hash` pasa a ser nulo para Android (no hay agente propio); la API de agentes rechaza siempre un dispositivo Android.

Sincronización (`android.sync`, cada 5 min y tras cada orden): `enterprises.devices.list` paginado. Para cada dispositivo, se actualiza `devices` y se guarda el inventario con la misma estructura de la fase 1 más una sección `mobile`:

| AMAPI | Inventario |
|---|---|
| `hardwareInfo.brand`, `model`, `serialNumber` | `hardware.manufacturer`, `model`, `serial` |
| `softwareInfo.androidVersion`, `securityPatchLevel`, `androidBuildNumber` | `system.os_name` = «Android», `os_version`, `mobile.security_patch_level`, `system.kernel` |
| `memoryInfo.totalRam`, `totalInternalStorage` | `hardware.memory_bytes`, `disks` (un volumen «Almacenamiento interno») |
| `networkInfo.imei`, `meid`, `wifiMacAddress`, `telephonyInfos` | `mobile.imei`, `mobile.meid`, `network` (Wi-Fi), `mobile.carriers` |
| `securityPosture.devicePosture`, `postureDetails` | `mobile.integrity` (`secure`, `at_risk`, `compromised`), `security` |
| `deviceSettings.isEncrypted`, `isDeviceSecure`, `developmentSettingsEnabled`, `unknownSourcesEnabled` | `security.disk_encryption`, `mobile.screen_lock`, `mobile.developer_options`, `mobile.unknown_sources` |
| `applicationReports` | `software` |
| `lastStatusReportTime` | `last_seen_at` |
| `state` (`ACTIVE`, `DISABLED`, `DELETED`, `PROVISIONING`, `LOST`) | `mobile.state` |

La política de AMAPI activa `statusReportingSettings` (aplicaciones, hardware, software, red, memoria, ajustes, postura de seguridad).

Estado en línea para Android: en línea si informó en las últimas 24 h; desconectado si más; inactivo a partir de 30 días; revocado si fue borrado. `StatusOf` recibe la plataforma.

## 7. Políticas Android

Catálogo de ajustes de la fase 2 con `platform = 'android'`. Todos son de modo `enforce` (Android los aplica él mismo; «auditar» se rechaza).

| Clave | Parámetros | Campo de AMAPI |
|---|---|---|
| `android.password` | `quality` (`numeric`, `alphanumeric`, `complex`), `min_length` (4–16) | `passwordPolicies` |
| `android.screen_lock` | `minutes` (1–60) | `maximumTimeToLock` |
| `android.camera` | `disabled` (bool) | `cameraAccess` |
| `android.screen_capture` | `disabled` | `screenCaptureDisabled` |
| `android.usb_file_transfer` | `disabled` | `usbDataAccess` |
| `android.factory_reset` | `disabled` | `factoryResetDisabled` |
| `android.developer_options` | `disabled` | `advancedSecurityOverrides.developerSettings` |
| `android.unknown_sources` | `disabled` | `advancedSecurityOverrides.untrustedAppsPolicy` |
| `android.wifi` | `ssid`, `security` (`wpa_psk`, `wpa_eap`), `passphrase` (cifrado en el servidor) | `openNetworkConfiguration` |
| `android.system_updates` | `type` (`automatic`, `windowed`, `postpone`), `window_start`, `window_end` | `systemUpdate` |
| `android.kiosk` | `package` | `kioskCustomLauncherEnabled` o aplicación única con `installType: KIOSK` |

Aplicación: el servidor construye la **política AMAPI efectiva** del dispositivo (ajustes Android + aplicaciones de §8 + `statusReportingSettings`), calcula su huella y la publica como `enterprises/<e>/policies/p-<huella[:16]>` si no existe (`policies.patch`). Después, si el dispositivo usa otra, `devices.patch` con `policyName`. Dispositivos con la misma combinación comparten política. Las políticas sin dispositivos se borran en `cleanup`.

Cumplimiento: `nonComplianceDetails` de cada dispositivo se traduce al ajuste correspondiente (`settingName` → clave del catálogo) en `device_setting_results`. Reglas del servidor para Android: nivel de parche de seguridad mínimo (`compliance.min_android_patch_level`, `AAAA-MM-DD`) y verificación de integridad (no cumple si `compromised`).

## 8. Aplicaciones

Origen nuevo `google_play` en el catálogo de la fase 3: `package_id` = nombre del paquete. Intenciones: `required` → `FORCE_INSTALLED`, `uninstall` → `BLOCKED`, `available` → `AVAILABLE` (solo Android). Se incluyen en la política AMAPI efectiva; el estado por dispositivo sale de `applicationReports` (instalada, versión) y de `nonComplianceDetails` (por ejemplo `APP_NOT_INSTALLED`).

## 9. Órdenes

| Orden de la consola | AMAPI | Rol |
|---|---|---|
| `lock` | `issueCommand` `LOCK` | Operador |
| `reboot` | `issueCommand` `REBOOT` | Operador |
| `sync` | sincronización inmediata de ese dispositivo | Operador |
| `reset_password` (`new_password`, que se muestra una vez y no se guarda) | `issueCommand` `RESET_PASSWORD` | Administrador |
| `lost_mode_start` (`message`, `phone`), `lost_mode_stop` | `START_LOST_MODE`, `STOP_LOST_MODE` (Android 11+) | Administrador |
| `wipe` | `devices.delete` con `wipeDataFlags` | Administrador, confirmando con el nombre |

Usan `commands` y lotes de la fase 2. Una orden Android pasa a `sent` al llamar a Google, guarda el nombre de la operación devuelta y se resuelve consultando `operations.get` en la tarea de sincronización (`succeeded`, `failed` con el error de Google, o `expired`). `wipe` marca el dispositivo como revocado y registra `device.wiped`.

## 10. Consola

- Integraciones › Android (§4), Inscripción › Android con QR (§5).
- Listado y filtros admiten Android; el icono y la columna de plataforma lo muestran.
- Ficha de un móvil: la etiqueta de inventario muestra IMEI y nivel de parche; pestañas General, Hardware, Red, Aplicaciones, Seguridad (integridad, cifrado, bloqueo de pantalla), Cumplimiento y Actividad. No aparecen Scripts, Actualizaciones ni BitLocker.
- Editor de políticas con la plataforma Android; catálogo de software con origen Google Play.

## 11. API (novedades)

Consola: `/integrations/android` (estado, `PUT` clave, `POST` vincular, `GET` callback, `DELETE` desvincular), `/android/enrollment-tokens`.

Auditoría: `android.configured`, `android.linked`, `android.unlinked`, `android_token.created|revoked`, `device.wiped`, más las de órdenes.

## 12. Comportamiento ante fallos

| Situación | Comportamiento |
|---|---|
| Google devuelve 429 o 5xx | La tarea se reintenta con espera; la consola muestra «Sincronización pendiente» si lleva más de 15 min sin éxito |
| Clave de la cuenta de servicio revocada | Las tareas fallan con «credenciales de Google no válidas»; aviso en Integraciones y alerta (fase 6) |
| Un dispositivo se borra desde Google o se restablece a mano | La sincronización lo ve como `DELETED` y lo marca revocado |
| Política que Google rechaza (`400`) | Se registra el error en la política y en Ajustes › Tareas; los dispositivos siguen con la anterior |
| Token de inscripción caducado | El móvil muestra el error de Google; la consola lo marca caducado |

## 13. Pruebas

- **Simulador de AMAPI** (`internal/server/mobile/android/fake`): servidor HTTP en memoria que implementa los recursos usados (empresas, tokens, políticas, dispositivos, órdenes, operaciones), con inyección de errores 429/5xx/400. Todas las pruebas del proveedor y de la consola usan el simulador.
- Traducción de cada ajuste a AMAPI y de cada `nonComplianceDetail` a la clave del catálogo; deduplicación de políticas por huella; traducción del inventario con respuestas reales de ejemplo de la documentación de Google.
- Flujo de vinculación con el simulador (incluida la vuelta con `enterpriseToken`).
- Prueba real con un móvil, en el despliegue, siguiendo una lista de comprobación nueva `docs/validacion-android.md`.

## 14. Decisiones

- **Sin agente propio en Android**: AMAPI y Android Device Policy cubren todo lo necesario para dispositivos de empresa.
- **Sondeo cada 5 min en vez de Pub/Sub**: no exige un punto de entrada público autenticado ni recursos de Pub/Sub; Pub/Sub puede añadirse sin cambiar el modelo.
- **Política AMAPI deduplicada por huella**: menos objetos en Google y menos llamadas al cambiar una política de grupo.
- **Interfaz de proveedor móvil**: iOS se añadirá como otra implementación.
