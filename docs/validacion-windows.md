# Validación manual en Windows

El agente de Windows se compila y sus funciones de interpretación tienen pruebas automáticas, pero la instalación como servicio y la lectura real de WMI y del registro solo se pueden comprobar en un Windows de verdad. Esta lista se repasa antes de cada versión en un **Windows 11 Pro** unido a dominio y en un **Windows Server 2022**.

Prepara un token en la consola y ten a mano la ficha del equipo.

## Instalación

- [ ] El comando de instalación de la consola, pegado en PowerShell como administrador, termina con «Equipo inscrito».
- [ ] El mismo comando en PowerShell **sin** elevar termina con «hace falta ejecutar como administrador» y no deja nada instalado.
- [ ] Con un token inventado termina con código de salida 2 (`$LASTEXITCODE`) y no deja nada instalado.
- [ ] Existe `C:\Program Files\MDMAgent\mdm-agent.exe`.
- [ ] En «Servicios» aparece «Agente MDM», en ejecución, con inicio automático y cuenta «Sistema local».
- [ ] En las propiedades del servicio, pestaña «Recuperación», los tres fallos reinician el servicio.

## Permisos

- [ ] Un usuario sin privilegios no puede abrir `C:\ProgramData\MDMAgent\agent.json` (acceso denegado).
- [ ] `icacls C:\ProgramData\MDMAgent` muestra solo SYSTEM y Administradores, sin herencia.

## Funcionamiento

- [ ] El equipo aparece en línea en la consola en menos de dos minutos.
- [ ] Tras reiniciar el equipo, el servicio arranca solo y el equipo vuelve a estar en línea sin intervención.
- [ ] `mdm-agent.exe status` (como administrador) muestra «activo» y una última conexión reciente.
- [ ] `C:\ProgramData\MDMAgent\agent.log` contiene «agente en marcha» e «inventario enviado».

## Inventario

Compara la ficha del equipo con lo que muestra Windows.

- [ ] Sistema, versión, dominio y usuario con sesión iniciada coinciden con `winver` y `whoami`.
- [ ] Fabricante, modelo, número de serie y BIOS coinciden con `msinfo32`.
- [ ] Procesador, núcleos, hilos y memoria coinciden con el Administrador de tareas.
- [ ] Cada unidad fija aparece con su tamaño y espacio libre, y el cifrado coincide con `manage-bde -status`.
- [ ] Con BitLocker activado en C: el cifrado aparece «Todos los volúmenes cifrados»; tras suspenderlo (`manage-bde -protectors -disable C:`) y esperar al siguiente inventario, aparece sin cifrar.
- [ ] El cortafuegos coincide con `Get-NetFirewallProfile` (los tres perfiles).
- [ ] En Windows 11, el antivirus aparece activado con «Microsoft Defender» (o el producto instalado).
- [ ] En Windows Server 2022, el antivirus aparece como desconocido y la sección de seguridad no se marca como no disponible.
- [ ] El arranque seguro coincide con `Confirm-SecureBootUEFI`.
- [ ] La pestaña Software contiene los programas de «Aplicaciones instaladas» (los instalados para todos los usuarios), sin duplicados entre 32 y 64 bits y sin componentes del sistema.
- [ ] Al instalar un programa nuevo, aparece en la ficha en menos de cinco minutos.

## Revocación y reinscripción

- [ ] Al revocar el equipo desde la consola, `status` pasa a «revocado» en el siguiente minuto y el registro lo explica.
- [ ] Al ejecutar de nuevo el comando de instalación con un token válido, el equipo vuelve a estar en línea **en el mismo registro** de la consola (mismo identificador, conserva grupo, usuario y etiquetas).
- [ ] La reinscripción sustituye el ejecutable aunque el servicio estuviera en marcha.

## Desinstalación

- [ ] `mdm-agent.exe uninstall` como administrador termina sin error.
- [ ] El servicio «Agente MDM» ya no existe y `C:\ProgramData\MDMAgent` se ha borrado.
- [ ] Si el ejecutable no se pudo borrar por estar en uso, el mensaje lo indica con su ruta.

## Políticas (fase 2)

Crea una política de Windows con cada ajuste, primero en modo «Auditar» y después en «Aplicar», asígnala a un grupo con el equipo de prueba y pulsa «Reevaluar políticas» en su ficha.

- [ ] **BitLocker obligatorio**, con un equipo sin cifrar y con TPM: en «Auditar» aparece «no cumple»; en «Aplicar» empieza el cifrado (`manage-bde -status`), se añade la contraseña de recuperación y, en la ficha, «Mostrar clave» enseña la misma que `manage-bde -protectors -get C:`.
- [ ] La consulta de la clave queda en la auditoría como «recovery_key.viewed».
- [ ] En una máquina virtual sin TPM, BitLocker aparece como «no aplicable» y el detalle lo explica.
- [ ] **Cortafuegos**: desactiva el perfil público a mano; en «Aplicar» vuelve a activarse en la siguiente evaluación.
- [ ] **Defender en tiempo real**: desactívalo a mano (Seguridad de Windows); en «Aplicar» se reactiva. Con otro antivirus instalado, el ajuste aparece como «no aplicable».
- [ ] **Bloqueo de pantalla** a 5 minutos: el valor `InactivityTimeoutSecs` queda en 300 y la pantalla se bloquea a los 5 minutos.
- [ ] **Contraseña local** mínima 12 sin caducidad: `net accounts` muestra 12 y «Ilimitado»/«Unlimited». Probar en un Windows en español y en uno en inglés.
- [ ] **Almacenamiento USB** bloqueado: una memoria USB deja de montarse (puede requerir desconectarla y volver a conectarla).
- [ ] **SMBv1**, **cuenta de invitado** e **inicio automático**: tras activarlos a mano, «Aplicar» los desactiva.
- [ ] **Escritorio remoto** permitido y denegado: cambia `fDenyTSConnections` y las reglas del cortafuegos del grupo de Escritorio remoto.
- [ ] Un ajuste con un error (por ejemplo, ejecutar el agente sin privilegios en una prueba manual) aparece como «error» con el mensaje de PowerShell.

## Scripts y órdenes (fase 2)

- [ ] Un script de PowerShell con un parámetro se ejecuta desde la consola y su salida aparece en el lote.
- [ ] Un script que tarda más que su tiempo máximo se corta y los procesos que lanzó también terminan (comprobar en el Administrador de tareas).
- [ ] «Reiniciar» con 5 minutos de retraso muestra el aviso de Windows con el mensaje indicado.
- [ ] «Retirar» desinstala el servicio y el equipo pasa a revocado en la consola.

## Software y parches (fase 3)

- [ ] **winget como SYSTEM**: un despliegue de `Mozilla.Firefox` (winget) se instala en Windows 10 y 11. En Windows Server, sin App Installer, aparece como «no aplicable» con el motivo.
- [ ] **MSI subido**: se descarga, se instala con `msiexec` y se detecta por inventario. Desinstalarlo con un segundo despliegue funciona.
- [ ] **EXE con parámetros propios** (por ejemplo, 7-Zip con `"{file}" /S`): instala en silencio y se detecta.
- [ ] **Detección por fichero y por registro**: con una ruta y una clave existentes, «instalado»; con una que no existe, se reinstala.
- [ ] **Código 3010**: el despliegue queda «instalado» con reinicio pendiente, y con reinicio «dentro de la ventana» reinicia solo dentro de ella.
- [ ] **Búsqueda de Windows Update** («Buscar ahora» en la ficha): la lista coincide con la de Configuración › Windows Update, con su clasificación, en un Windows en español y en uno en inglés.
- [ ] **Instalación dentro de la ventana**: con una política de parches cuya ventana empiece en unos minutos, las aprobadas se instalan, el resultado llega a la consola y el equipo avisa y reinicia.
- [ ] **KB excluida**: no se instala aunque su clasificación esté autoaprobada.
- [ ] **Descarga reanudada**: cortar la red durante la descarga de un instalador grande; al volver, continúa donde se quedó.
