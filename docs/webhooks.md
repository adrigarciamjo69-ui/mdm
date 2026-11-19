# Webhooks

Un canal de tipo **webhook** recibe los avisos como una petición `POST` con un cuerpo JSON. Sirve para Microsoft Teams (con un flujo de Power Automate o un «workflow» que acepte JSON), Slack (a través de un pequeño adaptador) o cualquier sistema propio.

## La petición

Cabeceras:

| Cabecera | Contenido |
|---|---|
| `Content-Type` | `application/json` |
| `User-Agent` | `mdm-server/<versión>` |
| `X-MDM-Event` | el tipo de aviso (ver abajo) |
| `X-MDM-Delivery` | identificador único de la entrega (UUID) |
| `X-MDM-Signature` | `sha256=<HMAC-SHA256 del cuerpo, en hexadecimal>` |

El receptor debe responder con un código 2xx en menos de 10 segundos. Cualquier otra respuesta, o ninguna, cuenta como fallo y se reintenta a los 1, 5, 15, 60 y 240 minutos. Un reintento lleva un `X-MDM-Delivery` distinto, pero el mismo contenido.

Por seguridad, la dirección tiene que ser `https://`. Solo se admite `http://` hacia una dirección IP privada de la red interna.

## Comprobar la firma

El secreto se muestra **una sola vez**, al crear el canal. Calcula el HMAC-SHA256 del **cuerpo tal como llega** (sin volver a serializarlo) con ese secreto y compáralo en tiempo constante con la cabecera.

Python:

```python
import hmac, hashlib

def firma_valida(cuerpo: bytes, cabecera: str, secreto: str) -> bool:
    esperada = "sha256=" + hmac.new(secreto.encode(), cuerpo, hashlib.sha256).hexdigest()
    return hmac.compare_digest(esperada, cabecera)
```

PowerShell:

```powershell
$hmac = New-Object System.Security.Cryptography.HMACSHA256 (,[Text.Encoding]::UTF8.GetBytes($secreto))
$esperada = 'sha256=' + (-join ($hmac.ComputeHash($cuerpoBytes) | ForEach-Object { $_.ToString('x2') }))
$firmaValida = $esperada -eq $cabecera
```

Si pierdes el secreto, borra el canal y créalo de nuevo (las reglas que lo usaban tendrán que elegir el nuevo).

## Tipos de aviso

### `alert.opened` y `alert.resolved`

```json
{
  "event": "alert.opened",
  "sent_at": "2026-11-18T10:02:00Z",
  "alert": {
    "id": 812,
    "rule": "Servidores sin conexión",
    "severity": "critical",
    "title": "srv-db-01 lleva más de 1 h sin conectar",
    "device": { "id": "…", "hostname": "srv-db-01", "platform": "linux" },
    "opened_at": "2026-11-18T10:00:00Z",
    "url": "https://mdm.empresa.es/alertas/812"
  }
}
```

`severity` es `critical`, `warning` o `info`. Las alertas globales (inicios de sesión fallidos) no llevan `device`. En `alert.resolved` se añade `resolved_at`.

### `alerts.digest`

Cuando una regla abre más de 5 alertas en 2 minutos:

```json
{
  "event": "alerts.digest",
  "sent_at": "2026-11-18T10:02:00Z",
  "rule": "Equipos sin conexión",
  "severity": "warning",
  "count": 23,
  "devices": ["PC-CONTA-01", "PC-CONTA-02", "…"],
  "url": "https://mdm.empresa.es/alertas?regla=…"
}
```

### `report.ready`

Un informe programado enviado a un webhook:

```json
{ "event": "report.ready", "sent_at": "…", "report": "patches_by_device", "title": "Parches pendientes por equipo", "rows": 37,
  "url": "https://mdm.empresa.es/informes/patches_by_device" }
```

### `automation.notify`

La acción «Avisar» de una automatización:

```json
{ "event": "automation.notify", "sent_at": "…", "automation": "Limpieza nocturna", "devices": 41,
  "actions": ["Ejecutar script «Limpiar temporales» (versión vigente)"], "message": "texto opcional" }
```

### `test`

El botón **Enviar prueba** del canal:

```json
{ "event": "test", "sent_at": "…", "channel": "Teams de sistemas" }
```

Pueden aparecer campos nuevos en versiones futuras: ignora los que no conozcas.
