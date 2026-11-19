# Software y parches

## Catálogo de aplicaciones

Cada aplicación tiene un **origen**:

| Origen | Plataforma | Cómo se instala | Cómo se detecta |
|---|---|---|---|
| winget | Windows | `winget install --id <id> --exact --silent --scope machine` | `winget list --id <id>` |
| apt | Debian, Ubuntu | `apt-get install -y <paquete>` | `dpkg-query` |
| dnf | RHEL, Rocky, Alma, Fedora | `dnf install -y <paquete>` | `rpm -q` |
| Instalador propio | Windows (MSI, EXE) y Linux (DEB, RPM) | La orden de instalación de la versión | La regla de detección de la versión |

**winget** necesita el paquete App Installer instalado para todo el equipo. Viene con Windows 10 y 11, pero no suele estar en Windows Server; allí las aplicaciones de winget aparecen como «no aplicable» y hay que usar un instalador propio.

### Instaladores propios

Se suben desde la ficha de la aplicación (hasta 4 GB). Cada versión guarda:

- **Orden de instalación y de desinstalación.** Se ejecutan como SYSTEM o root, con `cmd` en Windows y `sh` en Linux; `{file}` se sustituye por la ruta del instalador descargado. Por defecto:

  | Tipo | Instalar | Desinstalar |
  |---|---|---|
  | MSI | `msiexec /i "{file}" /qn /norestart` | `msiexec /x "{file}" /qn /norestart` |
  | DEB | `apt-get install -y "{file}"` | `apt-get remove -y "$(dpkg-deb -f "{file}" Package)"` |
  | RPM | `dnf install -y "{file}"` | `dnf remove -y "$(rpm -qp --qf '%{NAME}' "{file}")"` |
  | EXE | Obligatorio indicarla (cada fabricante usa sus parámetros: `/S`, `/silent`, `/quiet`…) | Obligatorio indicarla |

- **Códigos de éxito**, por defecto 0, 3010 y 1641. Los dos últimos significan «correcto, pero hay que reiniciar».
- **Regla de detección**, que decide si la aplicación está instalada:
  - **inventario** (por defecto): un programa del inventario cuyo nombre contiene un texto, con versión mínima opcional;
  - **fichero** (Windows): existe una ruta, opcionalmente con versión mínima;
  - **registro** (Windows): existe una clave o un valor;
  - **script**: termina con 0 si está instalada.

Una instalación solo cuenta como correcta si el instalador termina con un código de éxito **y** la regla detecta la aplicación. Si el instalador dice que todo ha ido bien pero la regla no la encuentra, el despliegue lo marca como fallo, y casi siempre es la regla la que está mal.

Los instaladores que nada usa se borran a los 7 días de subirlos. Están en el volumen `files`: inclúyelo en las copias de seguridad ([despliegue](despliegue.md)).

## Despliegues

Un despliegue asigna una aplicación a grupos (con exclusiones) con una de dos acciones:

- **Instalar y mantener instalada.** Si alguien la desinstala, vuelve a instalarse. Para winget, apt y dnf se puede pedir además **mantener actualizada**: una vez al día se lanza la actualización desde el repositorio.
- **Desinstalar.** Si dos despliegues de la misma aplicación chocan en un equipo, gana desinstalar.

Opciones:
- **A partir de:** no se hace nada antes de esa fecha.
- **Solo dentro de la ventana de mantenimiento:** la de la política de parches del equipo.
- **Reinicio** si el instalador lo pide: nunca, avisar al usuario y reiniciar a los 10 minutos, o dentro de la ventana.

Cada equipo comprueba sus despliegues cada 5 minutos y al recibir cambios. Para no saturar el servidor ni la línea cuando cientos de equipos tienen que descargar el mismo instalador, cada uno espera un **retraso aleatorio de hasta 30 minutos**, siempre el mismo para ese equipo y ese despliegue. Las descargas cortadas se reanudan y cada fichero se verifica con su SHA-256 antes de usarse.

Si una instalación falla, se reintenta a los 15 minutos, a la hora y a las 4 horas. Después queda como **fallida** hasta que alguien pulse **Reintentar** en el despliegue (en un equipo o en todos los fallidos). La tabla de estado muestra primero los equipos con problemas y el final del mensaje de error de cada uno.

## Parches del sistema

Cada equipo busca actualizaciones una vez al día, a una hora fija distinta para cada equipo, y cuando se lo pides con **Buscar ahora**.
- **Windows:** usa la API de Windows Update. La clasificación (seguridad, crítica, definiciones, controladores…) se toma del identificador de la categoría, que no depende del idioma.
- **Linux:** usa `apt` (las de seguridad son las del origen `-security`) o `dnf` (`updateinfo --security`).

### Políticas de parches

Se asignan a grupos con prioridad, como las demás políticas; un equipo usa la de mayor prioridad que le aplique. Cada una define:
- **Qué se aprueba solo:** en Windows, por clasificación; en Linux, solo seguridad o todo.
- **Días de espera** desde la publicación, para no instalar el primer día una actualización defectuosa.
- **KB excluidas**, que no se instalan nunca (solo Windows).
- **Ventana de mantenimiento**, en la hora local de cada equipo. Si termina antes de empezar (22:00 a 05:00), cruza la medianoche y pertenece al día en que empieza.
- **Reinicio:** dentro de la ventana, avisando al usuario con antelación, o nunca.

En la página **Parches** se puede **aprobar** o **rechazar** a mano una actualización de Windows por encima de la regla, o devolverla a la regla. Las aprobadas a mano también respetan los días de espera.

Dentro de la ventana, cada equipo instala lo aprobado que le falta, informa del resultado de cada actualización, vuelve a buscar y reinicia si la política lo permite. **Instalar ahora** (en la ficha del equipo) no espera a la ventana, pero tampoco reinicia fuera de ella si la política es «dentro de la ventana».

Sin política de parches, los equipos informan de lo que les falta, pero no instalan nada.

### Cumplimiento

En **Ajustes › Cumplimiento**, «máximo de días con parches de seguridad pendientes» (30 por defecto, 0 para desactivarlo): un equipo con una actualización de seguridad **aprobada** pendiente desde hace más días deja de cumplir.

Las actualizaciones de versión de Windows (por ejemplo, de 23H2 a 24H2) no se gestionan desde aquí.
