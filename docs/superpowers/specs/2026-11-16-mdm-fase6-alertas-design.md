# MDM — Fase 6: alertas, informes, automatizaciones y paneles (diseño)

Fecha: 2026-11-16 · Estado: pendiente de revisión · Depende de: fases 2, 3 y 4

## 1. Objetivo

Que el equipo de IT se entere de lo importante sin mirar la consola, obtenga informes periódicos sin trabajo manual y automatice tareas repetitivas con salvaguardas.

Criterios de éxito:

- Cuando un servidor lleva 30 minutos sin conectar, llega un correo; cuando vuelve, llega otro diciendo que se ha resuelto.
- Un corte de red en una oficina con 23 equipos genera un único aviso agrupado, no 23.
- Cada lunes a las 8:00 llega por correo el informe de parches pendientes en CSV.
- Una automatización «cada noche a las 2:00, limpiar temporales en Contabilidad» se puede simular antes de activarla y deja historial de cada ejecución.

## 2. Alcance

Dentro: reglas de alerta del catálogo, ciclo de vida de alertas, canales de correo (SMTP) y webhook, agrupación de avisos, informes predefinidos con exportación CSV y envío programado, automatizaciones con disparadores programados y por suceso, instantáneas diarias y gráficas de tendencia, personalización del resumen por administrador.

Fuera: Teams y Slack nativos (se pueden conectar por webhook), informes con consultas libres, PDF, paneles de construcción libre.

## 3. Sucesos internos

Paquete `internal/server/events`: el resto del servidor publica sucesos tipados (`device.enrolled`, `device.reenrolled`, `compliance.changed`, `deployment.failed`, `command.failed`, `login.failed`, `android.integrity_failed`, `alert.opened`). La publicación inserta una tarea `events.dispatch` en la misma transacción que el cambio que lo origina, de modo que un suceso nunca se pierde ni se emite por un cambio que no se guardó. Alertas y automatizaciones se suscriben a esos sucesos.

## 4. Alertas

### 4.1 Reglas

`alert_rules`: `id`, `name`, `type`, `params` (JSON validado), `severity` (`info`, `warning`, `critical`), `scope` (asignaciones a grupos con inclusión y exclusión, o todos), `channels` (lista de ids de canal), `emails` (lista de direcciones adicionales para el canal de correo), `notify_resolved` (bool), `enabled`, `created_by`.

| Tipo | Parámetros | Clase |
|---|---|---|
| `device.offline` | `hours` (1–720) | Estado |
| `disk.low` | `percent` (1–50) | Estado |
| `compliance.noncompliant` | `policy_id` (opcional) | Estado |
| `android.integrity` | — | Estado |
| `patches.security_pending` | `days` (1–90) | Estado |
| `bitlocker.key_missing` | — | Estado |
| `deployment.failed` | `deployment_id` (opcional) | Suceso |
| `command.failed` | `script_id` (opcional) | Suceso |
| `device.enrolled` | — | Suceso |
| `device.clone_suspected` | — | Suceso |
| `console.login_failures` | `count` (3–100) en 15 min | Suceso global (sin equipo) |

Las de **estado** se evalúan cada 5 minutos (`alerts.evaluate`) para los equipos del alcance; las de **suceso**, al recibir el suceso.

### 4.2 Ciclo de vida

`alerts`: `id`, `rule_id`, `device_id` (nulo en las globales), `state` (`open`, `acknowledged`, `resolved`), `severity`, `title`, `details` (JSON), `opened_at`, `acknowledged_at`, `acknowledged_by`, `resolved_at`, `resolved_by` (nulo si se resolvió sola).

- Índice único parcial en (`rule_id`, `device_id`) donde `state <> 'resolved'`: como mucho una alerta abierta por regla y equipo.
- Las de estado se resuelven solas cuando la condición deja de cumplirse en una evaluación. Las de suceso se resuelven a mano o automáticamente a los 7 días.
- Reconocer indica que alguien se está ocupando; no detiene la resolución automática.
- Si se desactiva o borra una regla, sus alertas abiertas se resuelven.

### 4.3 Avisos y agrupación

Al abrirse (y al resolverse, si `notify_resolved`) se encola `alerts.notify`. Antes de enviar, se esperan 2 minutos y se agrupan por regla: si en ese tramo hay más de 5 alertas nuevas de la misma regla, se envía **un aviso agrupado** («23 equipos sin conexión: PC-CONTA-01, PC-CONTA-02… y 18 más», con enlace a la consola filtrada). Si hay 5 o menos, un aviso por alerta.

`alert_notifications`: `alert_ids`, `channel_id`, `kind` (`opened`, `resolved`), `sent_at`, `status`, `error`, `attempts`.

## 5. Canales

### 5.1 Correo

Configuración única en **Integraciones › Correo** (`smtp_config`): `host`, `port`, `security` (`starttls`, `tls`, `none`), `username`, `password` (cifrado), `from_address`, `from_name`. Botón «Enviar correo de prueba». Envío con `net/smtp` (STARTTLS) o `crypto/tls` (TLS implícito), tiempo límite de 30 s, mensajes en texto y HTML sencillo, en español.

Canales de tipo correo (`notification_channels` con `type = 'email'`): nombre y lista de destinatarios.

### 5.2 Webhook

`notification_channels` con `type = 'webhook'`: `url` (https; se admite http solo para direcciones privadas, con aviso), `secret` (cifrado, generado al crear). Petición `POST` JSON con `Content-Type: application/json`, `User-Agent: mdm-server/<versión>`, `X-MDM-Event` (`alert.opened`, `alert.resolved`, `alerts.digest`, `report.ready`, `automation.notify`), `X-MDM-Delivery` (uuid) y `X-MDM-Signature: sha256=<hex>` (HMAC-SHA256 del cuerpo con el secreto). Éxito: código 2xx en menos de 10 s. Reintentos: 1, 5, 15, 60 y 240 minutos. Botón «Enviar prueba». Formato documentado en `docs/webhooks.md`:

```json
{
  "event": "alert.opened",
  "sent_at": "2026-11-16T10:00:00Z",
  "alert": {
    "id": 812, "rule": "Servidores sin conexión", "severity": "critical",
    "title": "srv-db-01 lleva 30 minutos sin conectar",
    "device": { "id": "…", "hostname": "srv-db-01", "platform": "linux" },
    "opened_at": "…", "url": "https://mdm.empresa.es/alertas/812"
  }
}
```

## 6. Informes

Informes predefinidos, cada uno con sus filtros (grupo, plataforma y los propios) y columnas fijas:

| Informe | Contenido |
|---|---|
| `hardware` | Una fila por equipo: fabricante, modelo, serie, CPU, memoria, disco, sistema, usuario, grupo |
| `software_by_app` | Una fila por programa y versión: número de equipos |
| `software_by_device` | Una fila por programa instalado y equipo (filtrable por nombre) |
| `compliance` | Una fila por equipo y ajuste no cumplido, con política y desde cuándo |
| `patches_by_device` | Equipos con actualizaciones pendientes: número, de seguridad, la más antigua |
| `patches_by_update` | Actualizaciones con número de equipos que la necesitan |
| `deployments` | Una fila por despliegue y equipo con su estado |
| `inactive` | Equipos sin conexión desde hace N días |
| `android_patch_level` | Móviles con su nivel de parche, ordenados del más antiguo |

En la consola: página **Informes** con la vista paginada y «Exportar CSV» (mismo formato que la fase 1: UTF-8 con BOM y celdas protegidas).

Envío programado (`report_schedules`): `report`, `filters`, `frequency` (`daily`, `weekly` con día, `monthly` con día del mes), `time` (en la zona horaria de las programaciones, §8), `channel_id` (correo: adjunta el CSV, hasta 10 MB, y si lo supera envía un enlace a la consola; webhook: evento `report.ready` con la URL), `enabled`, `last_run_at`, `last_status`. Lo ejecuta la tarea `reports.run`.

## 7. Automatizaciones

`automations`: `id`, `name`, `enabled`, `trigger`, `scope` (asignaciones a grupos), `actions` (lista ordenada), `max_devices` (por defecto 500, máximo 1.000), `created_by`.

Disparadores:

- `schedule`: `frequency` (`hourly`, `daily`, `weekly`, `monthly`), `time`, `day`. Actúa sobre los equipos del alcance en ese momento.
- `event`: `event` (`device.enrolled`, `compliance.changed` a no cumple, `alert.opened` de una regla) con filtro opcional. Actúa sobre el equipo del suceso, si está en el alcance.

Acciones (en orden; si una falla al crearse, se detiene la ejecución):

| Acción | Efecto |
|---|---|
| `run_script` (`script_id`, `version`: vigente o fija, `params`) | Lote de órdenes de la fase 2 |
| `reboot`, `refresh_inventory`, `scan_updates`, `evaluate_policies` | Lote de órdenes |
| `add_tag`, `remove_tag` (`tag`) | Cambia las etiquetas del equipo |
| `set_group` (`group_id`, estático) | Cambia el grupo principal |
| `notify` (`channel_id`, `message`) | Aviso con el resumen de la ejecución |

Salvaguardas:

- **Simular** (`POST /automations/{id}/simulate`): devuelve cuántos y qué equipos se verían afectados ahora y qué acciones se crearían, sin ejecutar nada.
- Si el alcance supera `max_devices` en una ejecución, no se ejecuta nada, la ejecución queda `blocked` y se abre una alerta informativa.
- Una automatización por suceso no se dispara dos veces para el mismo equipo y suceso en 10 minutos.
- Crear o editar una automatización con `run_script`, `reboot` o `set_group` exige rol de administrador.

`automation_runs`: `id`, `automation_id`, `trigger_info`, `started_at`, `finished_at`, `status` (`succeeded`, `failed`, `blocked`, `partial`), `device_count`, `batch_ids`, `error`. Se conservan 90 días.

## 8. Paneles

- Zona horaria de las programaciones: ajuste global `schedule.timezone` (por defecto `Europe/Madrid`). El servidor incluye la base de datos de zonas (`time/tzdata`), porque el contenedor corre en UTC; sin esto, «a las 8:00» llegaría una o dos horas tarde según la época del año. Los informes, las automatizaciones y las instantáneas usan esta zona; las ventanas de parches siguen usando la hora local de cada equipo (fase 3).
- Tarea diaria `metrics.snapshot` (00:05 en esa zona): `metrics_daily` (`day`, `key`, `value`) con equipos por estado y plataforma, porcentaje de cumplimiento, equipos con parches de seguridad pendientes, despliegues fallidos y alertas abiertas por gravedad. Se conservan 400 días.
- El **Resumen** de la fase 1 se amplía con secciones: estado del parque (existente), pendiente de revisar (existente, ampliado con cumplimiento y parches), alertas abiertas, cumplimiento por política, despliegues con fallos, parches de seguridad pendientes, Android, y gráficas de tendencia de 90 días (líneas SVG dibujadas por la consola, sin biblioteca de gráficos).
- Personalización: cada administrador elige qué secciones ve y en qué orden (`admin_preferences`: `admin_id`, `key`, `value`). Botón «Restablecer».
- El número de alertas abiertas aparece junto a «Alertas» en el menú.

## 9. API (novedades)

Consola: `/alert-rules`, `/alerts` (filtros por estado, gravedad, regla, equipo), `/alerts/{id}/acknowledge`, `/alerts/{id}/resolve`, `/channels`, `/channels/{id}/test`, `/integrations/email`, `/reports/{report}` (con `?format=csv`), `/report-schedules`, `/automations`, `/automations/{id}/simulate`, `/automations/{id}/runs`, `/metrics?keys=&days=`, `/me/preferences`.

Auditoría: `alert_rule.*`, `alert.acknowledged|resolved`, `channel.*`, `email.configured`, `report_schedule.*`, `automation.*`, `automation.run_blocked`.

## 10. Roles

| Acción | Solo lectura | Operador | Administrador |
|---|:-:|:-:|:-:|
| Ver alertas, informes, paneles e historial de automatizaciones; exportar | ✓ | ✓ | ✓ |
| Reconocer y resolver alertas; programar informes | | ✓ | ✓ |
| Reglas de alerta, canales, correo, automatizaciones | | | ✓ |

## 11. Comportamiento ante fallos

| Situación | Comportamiento |
|---|---|
| El servidor SMTP no responde | Reintentos como los webhooks; tras el último, `alert_notifications.status = failed` y aviso en Integraciones |
| Un webhook responde 4xx | Se reintenta (el receptor puede estar mal configurado temporalmente); tras el último intento, fallo visible en el canal |
| El servidor estuvo parado | Al arrancar, la tarea periódica recupera: las alertas de estado se evalúan; los informes y automatizaciones programados que no se ejecutaron se ejecutan una vez (no una por cada hora perdida) |
| Una regla de estado afecta a 500 equipos de golpe | Se abren 500 alertas y se envía un único aviso agrupado |
| El alcance de una automatización supera el límite | No se ejecuta; queda `blocked` con alerta |

## 12. Pruebas

- Ciclo de vida de alertas por cada tipo (apertura, deduplicación, resolución automática, cierre a los 7 días), agrupación de avisos con reloj simulado, firma de webhooks (comprobada con un receptor de prueba), reintentos, correo contra un servidor SMTP de prueba en memoria.
- Informes: contenido de cada uno con datos sembrados y exportación CSV.
- Automatizaciones: cálculo de la próxima ejecución (incluido el cambio de horario), simulación, límite de equipos, antirrebote de sucesos, creación de lotes.
- Instantáneas y endpoint de métricas.
- Consola: páginas de alertas, reglas, canales, informes, automatizaciones (con simulación) y el resumen personalizable.

## 13. Decisiones

- **Sucesos dentro de la transacción** que los origina, mediante la cola de tareas: sin pérdidas y sin un bus de mensajes externo.
- **Catálogo de reglas** en lugar de expresiones libres: más fácil de usar y de probar; se amplía añadiendo tipos.
- **Webhook firmado** como integración universal; Teams y Slack se conectan a través de él.
- **Gráficas propias en SVG** para no añadir una biblioteca pesada a la consola.
