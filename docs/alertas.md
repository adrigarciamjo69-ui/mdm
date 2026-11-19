# Alertas, informes y automatizaciones

## Avisos: correo y canales

En **Avisos** (menú lateral, solo administradores):

- **Correo (SMTP).** Servidor, puerto y seguridad (STARTTLS en el 587, TLS en el 465 o sin cifrar solo en la red interna), usuario, contraseña y remitente. La contraseña se guarda cifrada con `MDM_SECRET_KEY` y no se vuelve a mostrar. Con **Enviar prueba** compruebas la configuración en el momento.
- **Canales.** Un canal es un destino al que se puede avisar:
  - **Correo:** una lista de direcciones.
  - **Webhook:** una dirección https a la que el servidor envía un JSON firmado (Teams, Slack u otro sistema; ver [webhooks](webhooks.md)). Al crearlo se muestra **una sola vez** su secreto de firma.

Si un envío falla, se reintenta a los 1, 5, 15, 60 y 240 minutos. Después queda como fallido y el canal muestra el último error.

## Alertas

Una **regla** dice qué situación abre una alerta, en qué equipos (grupos incluidos y excluidos) y a quién avisar (canales y direcciones adicionales).

| Regla | Tipo | Parámetro |
|---|---|---|
| Equipo sin conexión | situación | horas (1 a 720) |
| Poco espacio en disco | situación | porcentaje libre (1 a 50) |
| Equipo que no cumple | situación | política (opcional) |
| Parches de seguridad pendientes | situación | días (1 a 90) |
| Clave de BitLocker sin custodiar | situación | — |
| Móvil con integridad dudosa | situación | — |
| Despliegue fallido | suceso | despliegue (opcional) |
| Orden fallida | suceso | script (opcional) |
| Equipo inscrito | suceso | — |
| Inicios de sesión fallidos en la consola | suceso | intentos en 15 minutos (3 a 100) |

- **Situaciones.** Se comprueban cada 5 minutos. Se abre una alerta por equipo, y **se resuelve sola** cuando la situación se corrige (el equipo vuelve, se libera espacio…).
- **Sucesos.** Abren la alerta en el momento. Se cierran a mano o solas a los 7 días.

Nunca hay dos alertas abiertas de la misma regla para el mismo equipo.

Cada alerta está **abierta**, **reconocida** (alguien se está ocupando; operador o administrador) o **resuelta**. El menú muestra cuántas hay sin resolver, y el resumen las resume por gravedad.

**Avisos agrupados.** El aviso sale 2 minutos después de abrirse la alerta. Si en ese tiempo una regla abre más de 5 alertas, se envía un único aviso («23 equipos: PC-1, PC-2… y 18 más») con un enlace a la consola filtrado por esa regla. Así, un corte de red no llena el buzón con cien correos. Por defecto también se avisa cuando una alerta de situación se resuelve.

Desactivar o borrar una regla resuelve sus alertas abiertas. Las ya cerradas se conservan en el historial.

## Informes

En **Informes** hay nueve informes predefinidos:
- hardware;
- software por programa y por equipo;
- cumplimiento;
- parches pendientes, por equipo y por actualización;
- despliegues;
- equipos inactivos;
- nivel de parche de Android.

Todos se pueden filtrar por grupo y plataforma, consultar en pantalla y **exportar a CSV**. El CSV va en UTF-8 con BOM para que Excel lea bien los acentos, y protege las celdas que empiezan por `=`, `+`, `-` o `@` para que no se interpreten como fórmulas.

**Programar envío** (operador o administrador) lo envía cada día, semana o mes, a la hora indicada, con los filtros del momento:
- **por correo:** el CSV adjunto, o un enlace si pesa más de 10 MB;
- **por webhook:** un enlace a la consola.

Las horas de los informes y de las automatizaciones van en la **zona horaria de las programaciones** (**Ajustes**, `Europe/Madrid` por defecto), y respetan el cambio de hora. Una hora que no existe esa noche (las 2:30 del último domingo de marzo) se ejecuta justo después del salto. Si el servidor estuvo parado, lo que tocaba se envía una sola vez al arrancar.

## Automatizaciones

Una automatización ejecuta **acciones** sobre los equipos de su **alcance**:
- **Programada:** cada hora, día, semana o mes.
- **Por suceso:** cuando se inscribe un equipo, cuando un equipo deja de cumplir o cuando se abre una alerta, de cualquier regla o de una concreta. En este caso la acción se aplica solo a ese equipo.

**Acciones**, en orden. Si una falla, las siguientes no se ejecutan:
- **Órdenes:** ejecutar un script (la versión vigente al ejecutarse o una fija), reiniciar, actualizar el inventario, buscar actualizaciones y reevaluar las políticas. Crean un lote normal, visible en **Actividad**.
- **Cambios en el equipo:** añadir o quitar una etiqueta, cambiar el grupo principal.
- **Avisar** por un canal con un resumen de lo que se hizo.

**Salvaguardas:**
- **Simular** muestra qué equipos se verían afectados ahora y qué se haría, sin ejecutar nada.
- **Máximo de equipos por ejecución** (500 por defecto, hasta 1.000). Si el alcance lo supera, no se ejecuta nada: queda «bloqueada» en el historial y se abre una alerta informativa. Protege de un alcance mal configurado que reinicie medio parque.
- Una automatización por suceso no se dispara dos veces para el mismo equipo y suceso en **10 minutos**.
- **Ejecutar scripts, reiniciar o cambiar de grupo** solo lo puede configurar un administrador. Tampoco un operador puede editar ni borrar una automatización que ya haga alguna de esas cosas.
- Al crearla, una programada no se ejecuta en el momento: espera a su primera hora.

El **historial** (90 días) muestra cada ejecución con su resultado (correcta, fallida, parcial o bloqueada), los equipos afectados y enlaces a los lotes creados.

## Resumen y tendencias

El **Resumen** muestra:
- el estado del parque;
- las alertas abiertas;
- lo pendiente de revisar;
- las **tendencias de 90 días** (equipos en línea y desconectados, porcentaje de cumplimiento, parches de seguridad pendientes y alertas críticas);
- las últimas inscripciones.

Las tendencias salen de una instantánea que el servidor toma cada día pasadas las 00:05 y conserva 400 días, así que las gráficas se llenan a medida que pasan los días.

Con **Personalizar**, cada administrador elige qué secciones ve y en qué orden. **Restablecer** vuelve al orden por defecto.
