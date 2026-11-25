# Unir al dominio, energía y administrador temporal — diseño

Fecha: 2026-11-25. Alcance acordado con Adri: dos ajustes de política de Windows y
una orden de administrador temporal. El control remoto queda fuera (lo hará él).

## 1. Ajuste «Unir al dominio» (`windows.domain_join`)

Campos:

| Campo | Tipo | Notas |
|---|---|---|
| `domain` | texto, obligatorio | FQDN, p. ej. `empresa.local` |
| `ou` | texto, opcional | DN de la OU, p. ej. `OU=Equipos,DC=empresa,DC=local` |
| `username` | texto, obligatorio | Cuenta con permiso para unir equipos (`EMPRESA\alta` o `alta@empresa.local`) |
| `password` | secreto, obligatorio | Se guarda cifrada (como la Wi-Fi de Android); la consola nunca la recibe |
| `restart` | enum | `warn_5min` (reinicia con aviso de 5 min) o `manual` |

- **Comprobar**: `Win32_ComputerSystem.PartOfDomain` y `Domain` = dominio (sin
  distinguir mayúsculas). Si se unió y falta reiniciar (marca en
  `HKLM:\SOFTWARE\MDM\DomainJoin`), cumple con «pendiente de reinicio».
  Unido a otro dominio → no cumple.
- **Aplicar**: si está en otro dominio, error (no se cambia de dominio solo).
  Si `nltest /dsgetdc:<dominio>` falla → error «no se encuentra ningún
  controlador…»; el motor lo reintenta en la siguiente evaluación (15 min), así
  un portátil nuevo se une en cuanto entra por VPN o en la oficina.
  `Add-Computer -DomainName -Credential [-OUPath] -Force`, marca y, según
  `restart`, `shutdown /r /t 300` con mensaje.
- **Secreto hacia el agente**: `GET /agent/v1/policy` descifra en el servidor
  los campos `TypeSecret` de los ajustes de Windows y Linux (solo por TLS y al
  propio equipo). Android sigue descifrando en su servicio.

## 2. Ajuste «Energía» (`windows.power`)

Todos los campos son opcionales; lo que no se indica no se toca.

- `plan`: `balanced` / `high_performance` / `power_saver` (GUID de Windows).
- `monitor_ac`, `monitor_dc`, `sleep_ac`, `sleep_dc`: minutos, 0 = nunca (0–600).
- `hibernate`: `off` / `on`.

Comprobar con `powercfg /getactivescheme`, `powercfg /query SCHEME_CURRENT
SUB_VIDEO VIDEOIDLE` / `SUB_SLEEP STANDBYIDLE` (los dos últimos valores
hexadecimales son CA y CC, independiente del idioma) y `HibernateEnabled`.
Aplicar con `powercfg /setactive`, `/change …` y `/hibernate`.

Para que las opciones del enum se lean en castellano se añade
`Field.OptionLabels` (opcional) y la consola lo usa; también se muestra
`Field.Description` como ayuda.

## 3. Orden «Administrador temporal» (`temp_admin`, `temp_admin_revoke`)

- Rol **admin**. Parámetros: `user` (opcional: vacío = usuario con sesión
  iniciada), `minutes` 15–480, `reason` obligatorio (3–500). La revocación
  admite `user` (vacío = todos los temporales del equipo).
- Capacidad nueva `tempadmin.v1`: los agentes viejos no reciben la orden.
- Auditoría: `batch.created` incluye usuario, minutos y motivo.
- Agente (`internal/agent/tempadmin`): guarda las concesiones en
  `tempadmin.json` **antes** de añadir al grupo, revisa cada minuto y al
  arrancar, y quita al usuario al caducar aunque no haya red o tras reiniciar.
  Si el usuario ya era administrador (no por nosotros) la orden falla: así nunca
  se quita a un administrador permanente. Repetir la orden amplía el plazo.
- Windows: grupo por SID `S-1-5-32-544` (nombre según idioma), `net localgroup`.
  Linux: grupo `sudo` o `wheel`, `usermod -aG` / `gpasswd -d`.
- Consola: botón «Administrador temporal» (admin, no Android) con usuario,
  duración y motivo; «Retirar administrador temporal». En el equipo, un aviso
  con las concesiones vigentes calculadas de su actividad.

Limitaciones a documentar: en Windows el usuario eleva con su propia contraseña
en UAC; los procesos ya elevados y una sesión iniciada durante el periodo
conservan el permiso hasta que se cierran.
