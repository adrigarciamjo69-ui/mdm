# Inventario, usuarios, licencias y actas

El MDM reúne en una sola consola los equipos con agente y el resto del material de IT: monitores, móviles, impresoras, material de red…

## Inventario

**Inventario** sustituye a la antigua lista de Dispositivos. Cada fila es un activo:

- **Con agente:** se crea solo al inscribir el equipo (o al sincronizar un móvil Android) y abre la ficha del equipo, con una pestaña **Activo** para los datos de inventario.
- **Manual:** se da de alta con **Nuevo activo** o con **Importar** (CSV).

Datos de un activo: categoría, nombre, marca, modelo, número de serie, etiqueta interna, estado (en uso, en almacén, en reparación, baja), persona, departamento, ubicación, precio, fecha de compra, garantía, pedido, proveedor y notas.

**Departamento:** si la persona viene de Entra ID y allí tiene departamento, el activo toma ese y no se edita a mano; cuando cambia en Entra, se actualiza en la siguiente sincronización. Sin persona, o si su ficha de Entra no tiene departamento, se escribe a mano. La **ubicación** es siempre manual y sirve para los activos que están en un sitio y no en manos de alguien.

Los grupos dinámicos pueden usar el departamento de los equipos con agente (condición «Departamento (inventario)»), por ejemplo un grupo «Finanzas» y un subgrupo «Finanzas – Portátiles» con «grupo = Finanzas» y otra condición. Los activos manuales no entran en grupos: los grupos solo sirven para políticas, software y órdenes de equipos gestionados.

### Unión por número de serie

Si das de alta a mano un portátil con su número de serie y después se inscribe con el agente, el equipo se une a esa ficha: se conservan precio, pedido, estado y persona, y el agente aporta los datos técnicos. No aparecen duplicados.

No se usan para unir los números de serie de relleno (`To be filled by O.E.M.`, `Default string`, `0`, los de máquinas virtuales…).

La categoría de los equipos con agente se elige sola (portátil, sobremesa o servidor según el tipo de chasis que informa el agente; «Ordenador» si no se sabe; «Móvil» para Android). Si la cambias a mano, ya no se toca.

Un administrador puede **desenlazar** un activo de su equipo si la unión fue un error: el activo queda como manual y el equipo recibe uno nuevo.

### Fusionar a mano

Cuando dos fichas son el mismo objeto pero no se unieron solas (sin número de serie, o con uno distinto en la ficha manual), selecciona las dos en Inventario y pulsa **Fusionar** (administradores):

- Eliges qué ficha se conserva (por defecto, la del equipo con agente) y, campo a campo, de cuál se toma el valor. De entrada se toma el de la ficha conservada y, si lo tiene vacío, el de la otra. Las notas se pueden juntar.
- Los documentos, las licencias asignadas (sin duplicar) y las actas de la otra pasan a la ficha resultante; la otra va a la papelera con el motivo «Fusionado con…».
- Si una de las dos tiene agente, el equipo se queda con la ficha resultante. Dos fichas con agente no se fusionan: para el MDM son dos equipos distintos (si es el mismo equipo inscrito dos veces, elimina el que sobra).
- Queda en la auditoría.

### Documentos y papelera

Cada activo tiene **Documentos** (facturas, garantías, albaranes, fotos): PDF, Word, Excel, imágenes, texto o ZIP, hasta 20 MB. Se guardan en el volumen `files`, que debe entrar en las copias de seguridad.

**Eliminar** un activo lo manda a la **Papelera** (enlace en Inventario). Allí se puede restaurar o eliminar definitivamente. Un activo con equipo enlazado no se puede eliminar: primero se elimina el equipo o se desenlaza.

### Importar CSV

Descarga la plantilla en **Importar**. Columnas: `categoria;nombre;marca;modelo;serie;etiqueta;estado;persona;departamento;ubicacion;precio;fecha_compra;garantia;pedido;proveedor;notas`. La persona es el correo de alguien de Usuarios. Antes de guardar se revisa todo el fichero; si una fila tiene errores, no se guarda ninguna.

## Usuarios (Entra ID)

La lista de personas se sincroniza cada hora desde Microsoft Entra ID, en solo lectura. Las personas que se dan de baja en Entra no se borran: quedan como «deshabilitada» o «eliminada», con su material asignado. El botón **Bajas con material** las lista para saber qué recoger.

### Conectar Entra ID (una vez)

1. En el centro de administración de Entra: **Aplicaciones › Registros de aplicaciones › Nuevo registro** (por ejemplo, «MDM inventario»), solo cuentas de este directorio.
2. **Permisos de API › Agregar un permiso › Microsoft Graph › Permisos de la aplicación › User.Read.All**, y **Conceder consentimiento de administrador**.
3. **Certificados y secretos › Nuevo secreto de cliente**. Copia el valor (solo se ve al crearlo) y apunta su caducidad.
4. En la consola, **Ajustes › Ajustes del inventario › Usuarios de Entra ID**: identificador del inquilino, de la aplicación, secreto y su caducidad. **Probar conexión** y **Guardar conexión**.

El secreto se guarda cifrado con `MDM_SECRET_KEY`. El servidor necesita salida HTTPS a `login.microsoftonline.com` y `graph.microsoft.com`.

Reglas de alerta nuevas: **Fallo al sincronizar usuarios de Entra ID** y **Secreto de Entra ID a punto de caducar** (14 días antes).

## Licencias

Licencias de software registradas a mano: tipo, puestos (0 = ilimitados), clave, compra, caducidad, precio. Se asignan a activos o a personas.

Si rellenas **Detectar instalaciones** (por ejemplo, `Office`), se cuentan los equipos con agente que tienen instalado un programa cuyo nombre contiene ese texto. La lista marca en ámbar las licencias con más asignaciones o instalaciones que puestos.

La clave se guarda cifrada. Solo un administrador puede verla (**Mostrar clave**), y queda en la auditoría.

Regla de alerta nueva: **Licencias que caducan pronto**. Informes nuevos: **Inventario de activos** y **Licencias**.

## Doc. Entrega

Actas de entrega y devolución de material:

1. **Nueva acta** (o desde la ficha de una persona): tipo, persona, DNI/NIE, fecha, responsable.
2. Añade activos del inventario o líneas sueltas (un cargador, un ratón…). En una devolución desde la ficha de la persona, sus activos ya vienen añadidos.
3. **Vista previa PDF** en cualquier momento (con marca «BORRADOR»).
4. **Confirmar acta**: recibe número (`ENT-2026-0001`, `DEV-2026-0001`) y ya no se edita. En una entrega, los activos pasan a la persona y a «en uso»; en una devolución, quedan sin persona y «en almacén».
5. Un administrador puede **anular** un acta confirmada (no deshace los cambios en los activos).

Los datos de la empresa, el logotipo, los colores y las cláusulas del PDF se configuran en **Ajustes › Ajustes del inventario › Actas de entrega**. Las cláusulas admiten `{empresa}`, `{trabajador}` y `{ciudad}`.

## Permisos

| Acción | Rol mínimo |
|---|---|
| Ver inventario, usuarios, licencias y actas; exportar | Solo lectura |
| Crear y editar activos y licencias, asignar, importar, documentos, restaurar de la papelera, crear y confirmar actas, sincronizar usuarios | Operador |
| Mandar a la papelera y eliminar definitivamente, eliminar licencias y borradores de actas, anular actas, enlazar, desenlazar y fusionar, ver claves, categorías, Entra ID y datos de las actas | Administrador |
