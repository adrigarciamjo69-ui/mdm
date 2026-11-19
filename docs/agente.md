# Agente

El agente es un único ejecutable que se instala en cada equipo como servicio. Inscribe el equipo con un token, se conecta al servidor cada minuto y envía el inventario cuando cambia. Solo abre conexiones salientes por HTTPS (puerto 443) y no ejecuta nada que le ordene el servidor.

Funciona en Windows 10, Windows 11 y Windows Server 2016 o posterior, y en Linux con systemd (Debian, Ubuntu, RHEL, Rocky, Alma y derivados), en procesadores amd64 y arm64.

## Instalación

En la consola, en **Inscripción**, crea un token. Al crearlo verás los comandos de instalación listos para copiar; el token solo se muestra en ese momento.

En Linux, como root:

```sh
curl -fsSL https://mdm.empresa.es/downloads/install.sh | sudo sh -s -- --server https://mdm.empresa.es --token <token>
```

En Windows, en PowerShell abierto como administrador:

```powershell
powershell -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create((irm https://mdm.empresa.es/downloads/install.ps1))) -Server https://mdm.empresa.es -Token <token>"
```

El instalador descarga el agente del propio servidor, comprueba su suma SHA-256 y lo inscribe. Si la suma no coincide no instala nada. El equipo aparece en la consola en uno o dos minutos.

Si el servidor usa un certificado de una autoridad interna, el equipo tiene que confiar en ella. En Linux, instala la autoridad en el sistema (`/usr/local/share/ca-certificates/` y `update-ca-certificates`) y pasa además `--cacert /ruta/autoridad.crt` al instalador. En Windows, la autoridad debe estar en el almacén de «Entidades de certificación raíz de confianza» del equipo, que es lo habitual si se distribuye por directiva de grupo.

### Despliegue masivo

Un token puede inscribir muchos equipos. Para un despliegue por directiva de grupo (GPO) en Windows, crea un token con un máximo de equipos igual al número de equipos de la unidad organizativa y una caducidad de una semana, y distribuye el comando como script de inicio del equipo. Para Linux, el mismo comando funciona desde Ansible, Salt o cualquier herramienta que ejecute órdenes como root.

Si todos los equipos de una oficina salen a Internet por la misma IP, no hay problema: el servidor solo limita los intentos con tokens incorrectos (10 por minuto y por IP), no las inscripciones válidas.

Inscribir dos veces el mismo equipo no lo duplica: el servidor lo reconoce por su identificador de hardware, conserva su grupo, usuario y etiquetas, y le da una credencial nueva.

## Estado

```sh
sudo mdm-agent status                 # Linux
& "C:\Program Files\MDMAgent\mdm-agent.exe" status   # Windows, como administrador
```

Muestra el servidor, el identificador del equipo, si está activo o revocado, la última conexión correcta y el último error.

## Desinstalación

```sh
sudo mdm-agent uninstall
```

Detiene y elimina el servicio, borra la configuración y el ejecutable. El equipo sigue apareciendo en la consola hasta que un administrador lo elimine.

## Ubicaciones

| | Windows | Linux |
|---|---|---|
| Ejecutable | `C:\Program Files\MDMAgent\mdm-agent.exe` | `/usr/local/bin/mdm-agent` |
| Configuración | `C:\ProgramData\MDMAgent\agent.json` | `/etc/mdm-agent/agent.json` |
| Registro | `C:\ProgramData\MDMAgent\agent.log` (rota a 5 MB, 3 copias) | `journalctl -u mdm-agent` |
| Servicio | `MDMAgent` («Agente MDM») | `mdm-agent.service` |

La configuración contiene la credencial del equipo: solo pueden leerla root (Linux) o SYSTEM y Administradores (Windows). En la misma carpeta están `state.json` (última conexión), `commands.json` (órdenes ya iniciadas y resultados pendientes de confirmar: impide repetir una orden tras un reinicio) y, durante unos minutos tras actualizarse, el binario anterior (`mdm-agent.old` o `mdm-agent.old.exe`).

## Actualización automática

Cuando el servidor distribuye una versión nueva del agente (la suya propia, con su `SHA256SUMS`), cada agente la descarga en su carpeta de configuración, comprueba la suma y ejecuta `mdm-agent selftest` con el binario nuevo: una petición autenticada al servidor que no registra nada. Solo si la prueba pasa sustituye el binario en uso y termina con el código 75 para que el gestor de servicios lo arranque de nuevo. Una versión que falla la suma o la prueba no se reintenta hasta 24 horas después. Se desactiva en **Ajustes › Agentes**, o por grupo congelando su versión.

`mdm-agent selftest` también sirve para comprobar a mano que un equipo llega al servidor: termina con 0 si todo va bien.

## Solución de problemas

**El instalador termina con código 2.** El servidor ha rechazado el token: no existe, ha caducado, se ha revocado o ya ha inscrito el máximo de equipos. Crea uno nuevo en la consola.

**El instalador termina con código 3 o no puede descargar.** No hay conexión con el servidor. Comprueba que el equipo resuelve el dominio, que llega al puerto 443 y que confía en el certificado (prueba con `curl https://mdm.empresa.es/healthz`).

**«status» dice «revocado».** Un administrador ha revocado el equipo. El agente sigue instalado pero no se comunica. Para volver a gestionarlo, ejecuta de nuevo el comando de instalación con un token válido.

**El equipo aparece como desconectado.** No se ha conectado en los últimos tres minutos. Mira el último error con `status` y el registro del servicio. El agente reintenta solo con esperas crecientes de hasta 15 minutos.

**Falta algún dato del inventario.** Si una parte del inventario no se pudo leer, la consola lo indica en esa sección y conserva el último valor conocido. En Linux, el estado del cifrado necesita `lsblk` y el del cortafuegos `ufw`, `firewalld` o `nft`. En Windows Server no hay Centro de seguridad, así que el antivirus aparece como desconocido.
