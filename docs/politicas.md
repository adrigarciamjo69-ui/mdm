# Políticas, órdenes y scripts

## Políticas

Una política es un conjunto de ajustes para una plataforma (Windows o Linux) que se asigna a grupos. Cada ajuste se configura en uno de dos modos:

- **Auditar**: el agente solo comprueba y el equipo aparece como «no cumple» si el ajuste no se cumple.
- **Aplicar**: el agente comprueba y, si no se cumple, lo corrige y vuelve a comprobar. Si un usuario lo cambia después, se corrige en la siguiente evaluación.

El agente evalúa al recibir una política nueva (en su siguiente conexión, como mucho un minuto) y después cada 15 minutos. En la ficha de cada equipo, la pestaña **Cumplimiento** muestra el resultado de cada ajuste; **Reevaluar políticas** fuerza una evaluación inmediata.

### Asignación, prioridad y conflictos

- Se asigna a grupos (manuales o dinámicos) o a **todos los equipos**, y se pueden **excluir** grupos. La exclusión siempre gana: un equipo incluido por un grupo y excluido por otro no recibe la política.
- Si dos políticas fijan el mismo ajuste en un equipo, gana la de **mayor prioridad** (número más bajo en la lista de Políticas). La ficha del equipo muestra el conflicto.

### Catálogo de ajustes

| Ajuste | Comprobación | Al aplicar |
|---|---|---|
| **Windows** | | |
| BitLocker obligatorio | Unidad del sistema protegida y con contraseña de recuperación | Activa BitLocker con TPM (`-UsedSpaceOnly`) y añade la contraseña de recuperación. Sin TPM: «no aplicable». Si se pide, custodia la clave en el servidor |
| Cortafuegos activado | Perfiles de dominio, privado y público | Los activa |
| Defender en tiempo real | Protección en tiempo real activa | La activa. Con otro antivirus: «no aplicable» |
| Bloqueo de pantalla | `InactivityTimeoutSecs` ≤ los minutos indicados | Fija ese valor |
| Contraseña local | Longitud mínima y caducidad (exportadas con `secedit`) | `net accounts` |
| Almacenamiento USB | Servicio `USBSTOR` deshabilitado (o no) | Cambia su inicio |
| SMBv1, cuenta de invitado, inicio automático | Desactivados | Los desactiva |
| Escritorio remoto | `fDenyTSConnections` y reglas del cortafuegos | Permite o deniega |
| Unir al dominio | `Win32_ComputerSystem`: en el dominio indicado (o unido y pendiente de reinicio) | Comprueba con `nltest /dsgetdc` que hay un controlador a la vista y ejecuta `Add-Computer` con la cuenta indicada y, si se pide, la OU. Reinicia con 5 minutos de aviso o deja el reinicio al usuario. Sin controlador (sin VPN, fuera de la oficina) da error y lo reintenta en cada evaluación. Si el equipo está en otro dominio no lo cambia |
| Energía | Plan activo, minutos de pantalla y suspensión con corriente y con batería (`powercfg`) e hibernación | `powercfg /setactive`, `/change` y `/hibernate`. Solo toca lo que indica la política |
| **Linux** | | |
| Cortafuegos activado | ufw o firewalld activo | `ufw --force enable` o activa firewalld. Sin ninguno: «no aplicable» |
| Actualizaciones de seguridad automáticas | unattended-upgrades o dnf-automatic en modo seguridad | Los instala y configura |
| SSH | `sshd -T`: acceso de root y autenticación por contraseña | Escribe `/etc/ssh/sshd_config.d/01-mdm.conf`, lo valida con `sshd -t` (si no es válido, lo borra y da error) y recarga sshd |
| Cifrado de disco | Volúmenes de datos sobre LUKS | Solo auditar |
| Paquetes prohibidos | Ninguno de la lista instalado | Solo auditar |
| **Ambas** | | |
| Script personalizado | El script de detección termina con 0 | Ejecuta el script de corrección |

Las comprobaciones de Windows no dependen del idioma del sistema.

La **contraseña de la cuenta para unir al dominio** se guarda cifrada con `MDM_SECRET_KEY` y la consola no la vuelve a mostrar (para cambiarla, escríbela de nuevo). El servidor solo la descifra al entregar la política al propio equipo, por TLS, y el agente la pasa al script por una variable de entorno; no aparece en la salida ni en el cumplimiento. Usa una cuenta dedicada con permiso únicamente para unir equipos a la OU indicada.

**Equipo nuevo listo en un paso:** crea un grupo dinámico (por ejemplo «Windows sin dominio» o por etiqueta), asígnale una política con «Unir al dominio» y «Energía» en modo «Aplicar» y, si quieres que ocurra nada más inscribirse, una automatización con el evento «equipo inscrito» que lance «Reevaluar políticas».

### Scripts de detección y corrección

- La **detección** debe terminar con código 0 si el equipo cumple y con otro código si no. La última línea que escriba se muestra como detalle en la consola.
- La **corrección** solo se ejecuta en modo «Aplicar» y si la detección falla. Después se vuelve a detectar: si sigue fallando, el ajuste queda como «no cumple».
- Se ejecutan como SYSTEM (Windows, PowerShell) o root (Linux: sh, bash o python3), con el tiempo máximo indicado (5 minutos por defecto).

## Cumplimiento

Un equipo **cumple** si todos sus ajustes se cumplen o no aplican. Es **desconocido** si no tiene políticas o si no ha informado en 24 horas con la versión vigente de su política. Además, el servidor aplica sus propias reglas (**Ajustes › Cumplimiento**): días máximos sin conexión y versiones mínimas del agente y del sistema.

## Grupos dinámicos

Se definen con una regla de hasta 20 condiciones sobre plataforma, sistema y versión, nombre, fabricante, modelo, usuario, dominio, etiquetas, programas instalados, cumplimiento, estado o días sin conexión. Un equipo puede estar en varios grupos dinámicos, pero en un solo grupo manual (su grupo principal). La pertenencia se recalcula al cambiar el inventario o la ficha de un equipo, y cada hora.

## Órdenes

Desde el listado (seleccionando equipos), desde la ficha o sobre un grupo:

| Orden | Rol |
|---|---|
| Reiniciar o apagar (con retraso y aviso al usuario), actualizar inventario, reevaluar políticas | Operador |
| Ejecutar script, administrador temporal (dar y quitar), retirar (desinstala el agente y revoca el equipo) | Administrador |

**Retirar** borra la configuración del agente (su credencial) y quita el servicio. En Windows el ejecutable queda en `C:\Program Files\MDMAgent` porque el propio proceso lo tiene abierto; se puede borrar a mano o con la siguiente limpieza del equipo.

### Administrador temporal

Hace a un usuario administrador local durante 15 minutos a 8 horas (Windows: grupo Administradores, encontrado por su SID; Linux: grupo `sudo` o `wheel`).

- **Usuario**: vacío para el que tenga la sesión iniciada; si no, `EMPRESA\usuario` (dominio), `AzureAD\usuario@empresa.es` (Entra) o el nombre de la cuenta local.
- **Motivo**: obligatorio; queda en la auditoría con el usuario y la duración.
- El agente guarda el permiso en su carpeta antes de darlo y lo quita él solo al caducar: aunque el equipo no tenga red o se reinicie. Lo revisa cada minuto y al arrancar.
- Si el usuario ya era administrador por otra vía, la orden falla y no se toca nada (así nunca se quita a un administrador permanente). Repetirla amplía el plazo.
- **Quitar administrador temporal** lo retira antes; con el usuario vacío quita todos los del equipo. La ficha del equipo avisa de los permisos vigentes y tiene «Quitar ya».
- Al retirar el agente se quitan primero los permisos temporales.
- La orden solo llega a agentes con esta función (versión actual) y caduca si el equipo no la recoge en 4 horas.

Cómo lo usa el usuario en Windows: al instalar algo, en el aviso de Control de cuentas escribe **su propia** contraseña (o «Ejecutar como administrador»). Para trabajar toda la sesión como administrador, debe cerrar sesión y volver a entrar.

Límites: los programas que se abrieron como administrador siguen así hasta que se cierran, y una sesión iniciada durante el periodo conserva el permiso hasta que se cierra. Mientras es administrador, el usuario podría darse permisos permanentes por su cuenta: concédelo solo a quien sea de confianza; la auditoría registra quién lo pidió y por qué.

### Entrega

Cada equipo recibe la orden en su siguiente conexión. Una orden no entregada caduca a los 7 días. El agente no repite nunca una orden, aunque se reinicie a mitad. La página **Actividad** muestra cada lote con su progreso y la salida de cada equipo (se conserva 30 días).

## Scripts

La biblioteca guarda cada versión de cada script. Los parámetros llegan al script como variables de entorno `MDM_PARAM_<NOMBRE>` y nunca se pegan en el texto, así que un valor como `; rm -rf /` llega literal.

## Claves de BitLocker

Se guardan cifradas con `MDM_SECRET_KEY`. Solo un administrador puede verlas (ficha › Cumplimiento › Mostrar clave); cada consulta queda en la auditoría y la clave se oculta al cabo de un minuto. Si se pierde `MDM_SECRET_KEY`, las claves custodiadas no se pueden recuperar desde la consola.
