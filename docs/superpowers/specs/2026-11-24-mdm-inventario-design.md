# MDM — Inventario de activos, licencias, usuarios de Entra y actas de entrega (diseño)

Fecha: 2026-11-24 · Estado: pendiente de aprobación · Depende de: fases 1 a 6 y el cambio de contraseñas

## 1. Objetivo y porqué

Electrans tiene hoy dos aplicaciones: el MDM (equipos con agente) y una app de inventario aparte (activos, licencias, personas y actas de entrega). Se van a fusionar en el MDM para tener **un solo sitio** donde ver todo el material de IT, saber de quién es cada cosa y generar las actas cuando se entrega o se recoge.

Se empieza de cero: no se migran datos de la app de inventario.

Criterios de éxito:

- La pestaña **Inventario** muestra en una sola lista los portátiles con agente y los monitores, móviles o impresoras dados de alta a mano, con filtros por categoría, estado, origen y persona.
- Si se da de alta a mano un portátil con su número de serie y después se inscribe con el agente, aparece **una sola fila**: los datos técnicos los pone el agente y se conservan precio, pedido, estado y persona.
- La lista de **Usuarios** se llena sola desde Entra ID; en la ficha de una persona se ven sus activos, sus licencias y sus actas.
- En **Software** se registra una licencia con sus puestos y se ve cuántos equipos la tienen instalada de verdad.
- En **Doc. Entrega** se hace un acta de entrega a una persona con dos activos y se descarga el PDF; al confirmarla, los activos pasan a ser de esa persona.

No-objetivos: migrar datos de la app de inventario; plano de planta; servicios y suscripciones; escritura en Entra (solo lectura); usuarios de Entra como cuentas de la consola (siguen siendo cuentas propias del MDM).

Anti-objetivos (sería un fracaso aunque funcione):

- Que el mismo portátil aparezca dos veces (uno manual y otro del agente).
- Que tocar el inventario afecte a políticas, órdenes, parches o despliegues de los equipos con agente.
- Que las claves de licencia o el secreto de Entra queden legibles en la base de datos o en las respuestas de la API sin una acción explícita y auditada.

## 2. Enfoques considerados

- **A. Tabla de activos como base (elegido).** Todo lo inventariable es un activo; un equipo con agente es un activo enlazado a su dispositivo. Precio, pedido, estado y persona viven en un solo sitio; actas y licencias apuntan siempre a un activo.
- **B. Dos tablas unidas al mostrar.** Cambia menos lo existente, pero duplica en dos sitios los datos de negocio y obliga a cada pantalla nueva a tratar dos casos.
- **C. Activos manuales como dispositivos.** Descartado: políticas, cumplimiento, grupos y órdenes verían dispositivos sin agente.

Elegido A por el usuario.

## 3. Modelo de datos

Migración nueva `0023_inventory.sql`. Todo con `id uuid`, `created_at`, `updated_at`.

### 3.1 Categorías

`asset_categories`: `id`, `name` (único, sin distinguir mayúsculas), `sort`. Se crean con la migración: Portátil, Sobremesa, Ordenador, Servidor, Móvil, Tableta, Monitor, Impresora, Red, Periférico, Otro. «Ordenador» es la categoría de los equipos con agente cuyo tipo de chasis no se conoce. Un administrador puede añadir, renombrar y borrar (no se borra una categoría en uso).

### 3.2 Activos

`assets`:

| Campo | Notas |
|---|---|
| `device_id` | `uuid NULL UNIQUE REFERENCES devices ON DELETE SET NULL`. Enlace con el equipo del agente |
| `category_id` | Obligatoria |
| `name` | Nombre visible (para un equipo con agente, su hostname mientras el usuario no lo cambie) |
| `brand`, `model` | Texto |
| `serial` | Texto. Índice único parcial sobre `upper(trim(serial))` cuando no está vacío, no es basura (§4.3) y el activo no está en la papelera |
| `asset_tag` | Etiqueta interna opcional, única si no está vacía |
| `status` | `en_uso`, `en_almacen`, `en_reparacion`, `baja` |
| `owner_id` | `uuid NULL REFERENCES directory_users ON DELETE SET NULL` |
| `department` | Texto libre; por defecto, el de la persona al asignarla |
| `location` | Texto libre (oficina, planta) |
| `price` | `numeric(12,2) NULL`, euros |
| `purchase_date`, `warranty_until` | `date NULL` |
| `purchase_order`, `supplier` | Texto |
| `notes` | Texto |
| `deleted_at`, `deleted_by` | Papelera (§3.7). `NULL` = activo vivo |

Los campos técnicos (sistema, procesador, memoria, discos, red, software) **no** se copian al activo: se leen del dispositivo enlazado.

### 3.3 Usuarios del directorio

`directory_users`: `entra_id` (único), `display_name`, `email`, `upn`, `department`, `job_title`, `employee_id`, `phone`, `office`, `enabled` (de `accountEnabled`), `deleted_at` (desaparecido de Entra), `synced_at`. Solo lectura desde la consola salvo un campo `notes` propio.

### 3.4 Licencias

`licenses`: `name`, `vendor`, `version`, `license_type` (`perpetua`, `suscripcion`, `volumen`, `oem`, `freeware`, `open_source`, `prueba`), `seats` (entero ≥ 0; 0 = ilimitadas), `key_sealed` (bytea cifrado con `MDM_SECRET_KEY`, opcional), `purchase_date`, `expiry_date`, `purchase_order`, `price`, `department`, `status` (`activa`, `expirada`, `baja`), `match_pattern` (texto opcional, §6.2), `notes`.

`license_assignments`: `license_id`, y **uno** de `asset_id` o `user_id`; `notes`; `assigned_at`. Única por (licencia, activo) y por (licencia, persona).

### 3.5 Actas

`delivery_records`: `number` (texto `ENT-2026-0001` / `DEV-2026-0001`, correlativo por tipo y año), `type` (`entrega`, `devolucion`), `user_id` (persona de Entra, obligatoria), `recipient_dni`, `date`, `responsible` (quien firma por la empresa; por defecto, el correo del administrador que la crea), `status` (`borrador`, `confirmada`, `anulada`), `notes` (internas, no van al PDF), `created_by`.

`delivery_items`: `record_id`, `asset_id` (opcional), y una copia en texto de `category`, `brand`, `model`, `serial` tomada al añadirlo (el acta no cambia si luego se edita el activo); `observations`.

### 3.6 Documentos de un activo

`asset_documents`: `asset_id` (`ON DELETE CASCADE`), `sha256` (fichero en el almacén de ficheros existente, `MDM_FILES_DIR`), `filename` (nombre original), `content_type`, `size`, `uploaded_by`, `uploaded_at`.

- Tipos permitidos, como en la app de inventario: PDF, Word (doc, docx), Excel (xls, xlsx), imágenes (JPEG, PNG, GIF, WebP), texto y ZIP. Se comprueba la extensión y los primeros bytes, no solo el tipo que manda el navegador. Hasta 20 MB por fichero.
- Se usa el almacén de ficheros de los instaladores, con su control de espacio libre. La limpieza de ficheros sin uso (los que nada referencia a los 7 días) tiene que contar también `asset_documents`, para no borrar documentos.
- Descarga con `Content-Disposition: attachment` y el nombre original; nunca se muestran en línea en la consola, para que un HTML o SVG subido no se ejecute.
- Se ven en la ficha del activo, en una sección **Documentos** (facturas, garantías, albaranes, fotos). Subir y borrar: operador. Cada subida y borrado queda en la auditoría.

### 3.7 Papelera

- **Eliminar** un activo (administrador) lo manda a la papelera: `deleted_at` y `deleted_by`, con un motivo opcional que va a la auditoría. Desaparece del inventario, de los buscadores, de los informes y de las actas nuevas; las actas ya hechas conservan su copia en texto.
- Al ir a la papelera, el activo pierde su dueño. Sus asignaciones de licencias y sus documentos se conservan, por si se restaura.
- Pantalla **Papelera** (enlace desde Inventario): activos eliminados con quién y cuándo. **Restaurar** (operador) lo devuelve tal como estaba, salvo el dueño. Si mientras tanto otro activo ha tomado su número de serie, la restauración se rechaza con 409 y el motivo.
- **Eliminar definitivamente** (administrador, con confirmación escribiendo el nombre): borra el activo, sus documentos y sus asignaciones. No se vacía sola.
- Un activo en la papelera no cuenta para la unión por número de serie (§4.1): si el equipo vuelve a inscribirse, se crea un activo nuevo.
- Solo hay papelera para activos. Licencias y actas en borrador se eliminan directamente, con confirmación.

## 4. Unión entre activos y equipos con agente

### 4.1 Al inscribirse un equipo

En la misma transacción que `EnrollDevice`:

1. Si ya hay un activo enlazado a ese `device_id` (reinscripción), no se hace nada más.
2. Si el número de serie es válido (§4.3) y existe un activo **sin enlazar y fuera de la papelera** con el mismo serie normalizado, se enlaza. Se conservan todos sus campos; si `brand` o `model` estaban vacíos, se rellenan con los del agente.
3. Si no, se crea un activo nuevo: `name` = hostname, `brand`/`model`/`serial` del agente, `status` = `en_uso`, y la categoría según §4.5.

Los móviles Android inscritos por Google siguen el mismo camino en su sincronización.

La migración crea también, al arrancar, el activo de cada equipo ya inscrito, con estas mismas reglas.

### 4.2 Después

- Si el agente informa de un número de serie distinto (cambio de placa), el activo enlazado **no** cambia su serie; la ficha muestra un aviso «el agente informa de otro número de serie».
- Si un activo manual se crea o edita con el serie de un equipo con agente que aún no tiene activo enlazado, se enlaza. Si el equipo ya tiene activo, se rechaza con 409 «Ese número de serie ya es de otro activo».
- Al **eliminar** el dispositivo en la consola, el activo se queda (desenlazado), con su historial. Al **retirar** o **revocar**, el activo no cambia.
- Un administrador puede **desenlazar** a mano un activo de su equipo, y **enlazar** un activo sin equipo con un equipo sin activo.

### 4.3 Números de serie que no sirven para unir

Se normalizan con `upper(trim())`. No se usan para unir ni cuentan para la unicidad: vacío, menos de 4 caracteres, todo el mismo carácter, y la lista conocida: `TO BE FILLED BY O.E.M.`, `DEFAULT STRING`, `SYSTEM SERIAL NUMBER`, `NONE`, `N/A`, `0123456789`, `CHASSIS SERIAL NUMBER`, `NOT SPECIFIED`, y los que empiezan por `VMWARE-` o contienen `PARALLELS`. En esos casos el activo se crea igualmente, pero sin unión por serie.

### 4.4 Persona y campo «usuario asignado» del dispositivo

El dueño del activo es la referencia. Al cambiar el dueño de un activo enlazado, `devices.assigned_user` se actualiza con el correo de esa persona (su UPN si no tiene correo; vacío si no hay dueño), para que las reglas de grupos dinámicos por usuario sigan funcionando. En la ficha del equipo, el campo «usuario» se sustituye por el dueño del activo.

### 4.5 Categoría de los activos creados por el agente

El agente no informa hoy del tipo de chasis. Se añade al inventario del agente un campo `chassis` (`laptop`, `desktop`, `server`, `unknown`), leído en Windows de `Win32_SystemEnclosure.ChassisTypes` y en Linux de `/sys/class/dmi/id/chassis_type`, con los códigos SMBIOS estándar. La categoría inicial es:

- Servidor, si el chasis es de servidor o el sistema es Windows Server;
- Portátil o Sobremesa, según el chasis;
- Móvil para Android (Tableta no se distingue: se cambia a mano);
- Ordenador en cualquier otro caso, incluidos los agentes antiguos que aún no informan del chasis.

Solo se aplica al crear el activo; después, la categoría es la que elija el usuario.

## 5. Usuarios desde Entra ID

### 5.1 Configuración (administrador)

En **Ajustes › Integraciones › Entra ID**: identificador del inquilino, identificador de la aplicación y secreto de cliente (cifrado con `MDM_SECRET_KEY`, no se vuelve a mostrar), y la fecha de caducidad del secreto (para avisar). **Probar conexión** pide un token y una página de usuarios. La guía explica cómo registrar la aplicación en Entra con el permiso de aplicación `User.Read.All` y el consentimiento del administrador.

### 5.2 Sincronización

- Tarea periódica `directory.sync` cada hora y botón **Sincronizar ahora**. Flujo de credenciales de cliente contra `login.microsoftonline.com` y Microsoft Graph `GET /v1.0/users` con `$select` de los campos de §3.3, filtrando `userType eq 'Member'`, siguiendo `@odata.nextLink`.
- Altas y cambios se aplican por `entra_id`. Los que ya no vienen se marcan con `deleted_at`; si vuelven, se limpia. Nunca se borran filas.
- Si la sincronización falla, se guarda el error y la hora, se ve en la pantalla de Usuarios y abre una alerta del tipo nuevo `directory.sync_failed`. Un fallo no marca a nadie como borrado.
- Aviso 14 días antes de que caduque el secreto (alerta del tipo nuevo `directory.secret_expiring`).

### 5.3 Pantalla Usuarios

Lista con buscador (nombre, correo, número de empleado), filtros por departamento y estado (activo, deshabilitado en Entra, eliminado de Entra) y un contador de activos por persona. Filtro rápido **«Bajas con material»**: personas deshabilitadas o eliminadas en Entra que aún tienen activos asignados.

Ficha de la persona: datos de Entra, sus activos, sus licencias y sus actas, con botones **Nueva acta de entrega** y **Nueva acta de devolución** que abren el acta con la persona ya elegida (y, en la de devolución, sus activos ya añadidos).

## 6. Software (licencias)

### 6.1 Pantalla

Lista con nombre, fabricante, tipo, puestos, asignaciones, instalaciones detectadas, caducidad y estado; filtros por tipo, estado y «caduca en 30 días». Ficha con los datos, la clave (oculta; **Mostrar clave** solo administradores, se oculta al minuto y queda en la auditoría, igual que BitLocker) y dos listas de asignaciones: activos y personas.

### 6.2 Instalaciones detectadas

Si la licencia tiene `match_pattern` (por ejemplo `Microsoft Office`), se cuentan los equipos con agente cuyo software instalado contiene ese texto (sin distinguir mayúsculas), y se listan. La lista muestra por separado **asignadas** (asignaciones manuales a activos y personas) e **instaladas** (detectadas). Si cualquiera de las dos supera los puestos (y los puestos no son 0), la fila se marca en ámbar. El patrón es opcional: sin él, solo cuentan las asignaciones manuales.

### 6.3 Caducidad

Una licencia con `expiry_date` pasada se ve como «expirada» aunque su estado guardado sea «activa». Nueva regla de alerta `license.expiring` con parámetro `days` (1–180).

## 7. Doc. Entrega

### 7.1 Flujo

1. **Nueva acta**: tipo, persona (buscador sobre Usuarios), DNI/NIE del receptor, fecha, responsable, notas internas.
2. **Añadir activos**: buscador sobre el Inventario. En una entrega se proponen los activos en almacén; en una devolución, los de esa persona. Se puede añadir una línea libre sin activo (por ejemplo, un cargador) con categoría, marca, modelo, serie y observaciones.
3. **Vista previa PDF** en cualquier momento (marca de agua «BORRADOR»).
4. **Confirmar** (operador): el acta queda confirmada, con número definitivo, y ya no se edita. Efectos: en una entrega, cada activo pasa a ser de la persona y a `en_uso`; en una devolución, cada activo queda sin dueño y en `en_almacen`. Todo en una transacción y en la auditoría.
5. **Anular** (administrador) una confirmada: no deshace los cambios en los activos; queda como anulada con motivo.

### 7.2 PDF

Se genera en el navegador con jsPDF, **portando la plantilla de la app de inventario** (`DeliveryPage.tsx`, función `generatePDF`) para que las actas salgan igual que hasta ahora. A4, con:

- cabecera con el logotipo (o, sin logo, el nombre de la empresa en un recuadro del color principal) y, a la derecha, dirección y teléfono;
- título «JUSTIFICANTE DE ENTREGA DE MATERIAL» o «JUSTIFICANTE DE DEVOLUCIÓN DE MATERIAL»;
- párrafo de introducción con la empresa, el trabajador (apellidos, nombre), la ciudad y la fecha;
- tabla de material: Dispositivo (categoría), Descripción / Modelo (marca y modelo), Número de serie, Observaciones;
- cláusulas en viñetas, con los marcadores `{empresa}`, `{trabajador}` y `{ciudad}`. Por defecto, las mismas cinco de entrega y cuatro de devolución de la app actual;
- dos recuadros de firma: «Recibe/Devuelve el material EL TRABAJADOR» (con su DNI) y «Entrega/Recibe el material LA EMPRESA» (con el responsable y el NIF de la empresa);
- frase de cierre en cursiva y pie con web, número de página y correo;
- nombre del fichero: `<número>_<tipo>.pdf`.

**Ajustes › Actas** (administrador): empresa, NIF, dos líneas de dirección, teléfono, web, correo, ciudad, logotipo (PNG o JPG hasta 500 KB, guardado en la base de datos), color principal y color del pie para cada tipo, y las cláusulas de cada tipo.

La firma se hace a mano sobre el papel impreso. No hay firma electrónica.

## 8. Pantallas y navegación

- **Inventario** sustituye a **Dispositivos** en el menú (la ruta `/dispositivos` redirige a `/inventario`).
  - Columnas: nombre, categoría, marca y modelo, número de serie, persona, estado, origen (icono de agente con su estado de conexión, o «manual»). Filtros: categoría, estado, origen (con agente, manual), persona, departamento, y los filtros actuales de equipos (grupo, plataforma, problemas) que solo afectan a los activos con agente. Exportar CSV.
  - Las acciones masivas actuales de equipos (reiniciar, scripts…) siguen disponibles al seleccionar filas con agente; las filas manuales no se pueden seleccionar para órdenes. Acciones masivas nuevas para cualquier activo: cambiar estado, categoría, persona o ubicación.
  - **Nuevo activo**: formulario con los campos de §3.2. **Importar CSV** con plantilla descargable (categoría, nombre, marca, modelo, serie, etiqueta, estado, correo de la persona, departamento, ubicación, precio, fecha de compra, garantía, pedido, proveedor, notas); vista previa con errores por fila antes de guardar.
  - Ficha de un activo manual: sus datos, documentos, historial (auditoría filtrada), licencias y actas. Ficha de un activo con agente: la ficha actual del equipo con una pestaña nueva **Activo** (los datos de §3.2, documentos, licencias y actas).
  - Enlace **Papelera** junto a los botones de la lista (§3.7).
- **Usuarios** (§5.3), **Software** (§6), **Doc. Entrega** (§7): entradas nuevas del menú.
- El **Resumen** añade un bloque opcional: activos por estado y licencias que caducan en 30 días.

## 9. Permisos

| Acción | Rol mínimo |
|---|---|
| Ver inventario, usuarios, licencias, actas; exportar | Solo lectura |
| Crear y editar activos, licencias, asignaciones; importar CSV; subir y borrar documentos; restaurar desde la papelera; crear y confirmar actas; sincronizar ahora | Operador |
| Mandar activos a la papelera y eliminarlos definitivamente; eliminar licencias y actas en borrador; anular actas; enlazar y desenlazar a mano; mostrar claves; categorías; configurar Entra y los datos de las actas | Administrador |

Mandar a la papelera un activo con agente enlazado no está permitido: primero se elimina el dispositivo o se desenlaza.

## 10. Auditoría, informes y alertas

- Acciones nuevas de auditoría para cada alta, cambio, borrado, enlace, importación, confirmación y anulación de acta, revelado de clave, y configuración y sincronización de Entra.
- Informes nuevos: **Inventario de activos** (todos los campos, filtros de la lista) y **Licencias** (puestos, asignaciones, instalaciones, caducidad), con CSV y envío programado como los demás.
- Tipos de alerta nuevos: `license.expiring`, `directory.sync_failed`, `directory.secret_expiring`.

## 11. Restricciones

- Mismo stack: Go 1.23, PostgreSQL 16, React con Vite, sin Tailwind (estilos del proyecto). Dependencias nuevas solo en la consola: `jspdf` y `jspdf-autotable`.
- El servidor necesita salida HTTPS a `login.microsoftonline.com` y `graph.microsoft.com` (la VM tiene Internet).
- Todo dato cifrado usa la caja de `internal/server/secrets` con `MDM_SECRET_KEY`.
- Las rutas nuevas de la API van bajo `/api/v1` y, por tanto, quedan solo para la red interna con la configuración de Traefik actual.
- La importación CSV busca la persona por correo o UPN en Usuarios y la categoría por nombre; una fila con alguna de las dos sin encontrar es un error de esa fila. No se guarda nada mientras haya errores.
- Pruebas: almacén y API con PostgreSQL real como el resto; la unión por serie, la sincronización (con un Graph simulado) y la confirmación de actas con pruebas dedicadas; la consola con Vitest.

## 12. Decisiones tomadas por Claude (revisables)

- El agente informa del tipo de chasis para clasificar solos portátiles, sobremesas y servidores; si no se sabe, «Ordenador».
- La plantilla del PDF es la de la app de inventario actual, con sus mismos textos por defecto.
- En licencias se muestran por separado asignadas e instaladas.

- Estados de activo `en_uso`, `en_almacen`, `en_reparacion`, `baja` (la app de inventario usaba activo, inactivo, reparación, baja; «en almacén» encaja mejor con las actas).
- Papelera solo para activos, sin vaciado automático; al ir a la papelera, el activo pierde su dueño.
- Documentos: mismos tipos que la app de inventario, hasta 20 MB, siempre como descarga.
- Confirmar un acta cambia la persona y el estado de sus activos.
- Numeración `ENT-AAAA-NNNN` y `DEV-AAAA-NNNN`.
- Datos y logotipo de la empresa y texto legal del PDF configurables en Ajustes; firma manuscrita sobre papel.
- Solo usuarios miembro de Entra (sin invitados); nunca se borran, se marcan.
- El dueño del activo actualiza `devices.assigned_user` con su correo.
- Importación CSV incluida para la carga inicial.
