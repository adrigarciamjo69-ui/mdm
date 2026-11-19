# Móviles Android

El MDM gestiona móviles y tabletas Android **propiedad de la empresa** a través de la Android Management API de Google (Android Enterprise). No hay agente propio: Google instala en el móvil su aplicación de gestión, y el servidor del MDM habla con Google.

Los móviles personales de los empleados (perfil de trabajo) y los iPhone no están incluidos.

## Puesta en marcha (una vez)

Necesitas una cuenta de Google de la empresa (puede ser una cuenta de Gmail creada para esto) y acceso a Google Cloud. Google no cobra por la Android Management API.

1. **Crea un proyecto en Google Cloud** en <https://console.cloud.google.com/>, por ejemplo «mdm-empresa».
2. **Activa la API**: en «APIs y servicios › Biblioteca», busca **Android Management API** y pulsa «Habilitar».
3. **Crea una cuenta de servicio**: en «IAM y administración › Cuentas de servicio», crea una (por ejemplo «mdm») y dale el rol **Android Management User**.
4. **Descarga su clave**: en la cuenta de servicio, «Claves › Agregar clave › Crear clave nueva › JSON». Guarda el fichero en un lugar seguro: da control sobre todos los móviles.
5. **En la consola del MDM**, ve a **Android** (menú lateral, solo administradores), pega el contenido del JSON y pulsa **Guardar clave**. El servidor comprueba con Google que la clave funciona y la guarda cifrada con `MDM_SECRET_KEY`.
6. **Pulsa «Vincular con Google»**. Irás a Google para crear la «empresa gestionada» (nombre de la empresa y cuenta de Google del administrador) y volverás a la consola con la empresa vinculada.

Para que la vuelta desde Google funcione, `MDM_PUBLIC_URL` tiene que ser la dirección con la que entras a la consola.

Google limita los proyectos nuevos a un número reducido de dispositivos hasta que revisa el uso que se le da a la API. Si necesitas más, Google lo explica en <https://developers.google.com/android/management/quotas>. La revisión puede tardar días: tenlo en cuenta al planificar.

## Inscribir un móvil

1. En **Inscripción › Android**, crea un token: nombre, grupo de destino (el móvil entrará en ese grupo), validez (de 1 a 90 días) y, si quieres, un solo uso.
2. Coge un móvil **nuevo o restablecido de fábrica**. En la pantalla de bienvenida, toca seis veces en el mismo sitio: se abre el lector de códigos QR (Android 7 o posterior).
3. Conéctalo a una red Wi-Fi y escanea el código. El móvil descarga la aplicación de gestión de Google, aplica la política de su grupo y aparece en **Dispositivos** en la siguiente sincronización (como mucho, 15 minutos).

Un móvil que ya está en uso no se puede inscribir así: hay que restablecerlo de fábrica antes. Para inscribir muchos móviles de golpe, Google ofrece la inscripción sin intervención (zero-touch) a través de los distribuidores; no se configura desde el MDM.

## Políticas

Las políticas de plataforma **Android** admiten:

| Ajuste | Qué hace |
|---|---|
| Contraseña de desbloqueo | Exige un código numérico, alfanumérico o complejo, con una longitud mínima de 4 a 16 |
| Bloqueo automático | Bloquea la pantalla tras 1 a 60 minutos sin uso |
| Cámara, capturas de pantalla, transferencia por USB | Permitir o desactivar |
| Restablecimiento de fábrica | Impedir que el usuario lo haga desde los ajustes |
| Opciones de desarrollador, aplicaciones de origen desconocido | Permitir o bloquear |
| Wi-Fi de la empresa | Red con contraseña (WPA-PSK). La contraseña se guarda cifrada y la consola no la vuelve a mostrar |
| Actualizaciones del sistema | Automáticas, dentro de una ventana horaria o aplazadas hasta 30 días |
| Modo quiosco | El móvil solo muestra una aplicación (lectores de almacén, tabletas de recepción) |

En Android no existe el modo «auditar»: Google aplica siempre la política y informa de lo que no puede cumplir, que aparece en la pestaña **Cumplimiento** de la ficha.

Asignación, prioridad y exclusiones funcionan como en Windows y Linux ([políticas](politicas.md)). El servidor publica en Google una política por cada combinación distinta de ajustes y aplicaciones, y la reutiliza entre los móviles que la comparten.

Las redes Wi-Fi de empresa con certificado (802.1X) no están incluidas.

## Aplicaciones

En **Software**, una aplicación de plataforma Android y origen **Google Play** se identifica por su nombre de paquete (por ejemplo, `com.microsoft.teams`, el que aparece en la dirección de la aplicación en Google Play). En un despliegue puede ser:

- **Instalar:** se instala sola y el usuario no puede desinstalarla.
- **Disponible:** aparece en la Play Store de la empresa para que el usuario la instale si quiere.
- **Desinstalar:** se desinstala y queda bloqueada.

Las aplicaciones de pago o privadas se aprueban antes en Google Play gestionado (<https://play.google.com/work>) con la cuenta de la empresa.

## Acciones

En la ficha de un móvil (y sobre una selección de móviles):

| Acción | Rol | Qué hace |
|---|---|---|
| Bloquear | Operador | Bloquea la pantalla al momento |
| Reiniciar | Operador | Reinicia el móvil |
| Modo perdido | Operador | Bloquea el móvil con tu mensaje en la pantalla; solo permite llamar al teléfono indicado |
| Salir del modo perdido | Operador | |
| Cambiar contraseña | Administrador | Pone un código de desbloqueo nuevo (para un usuario que lo ha olvidado) y bloquea la pantalla |
| Borrar el móvil | Administrador | Lo restablece de fábrica y deja de estar gestionado (opcionalmente, también la tarjeta de memoria). No se puede deshacer |

El servidor envía las órdenes a Google cada minuto. El móvil las recibe cuando tiene conexión, y la página **Actividad** muestra el resultado.

Los móviles no se «revocan» ni se eliminan desde su ficha: para dejar de gestionar uno, bórralo. Uno que se borre por otra vía (desde Google o restableciéndolo) aparece como revocado en la siguiente sincronización.

## Estado, inventario y cumplimiento

El servidor sincroniza con Google **cada 15 minutos**. Un móvil está «en línea» si informó a Google en las últimas 24 horas: los móviles informan con mucha menos frecuencia que los PC.

La ficha muestra:
- modelo, número de serie, versión de Android y nivel de parche;
- IMEI, teléfono y operador;
- cifrado y bloqueo de pantalla;
- el **veredicto de integridad** de Google, que detecta móviles rooteados o modificados;
- las aplicaciones instaladas.

Además de sus políticas, un móvil **no cumple** si:
- su parche de seguridad es anterior al **mínimo** fijado en **Ajustes › Cumplimiento**;
- Google duda de su integridad.

La pantalla **Android** muestra cuándo fue la última sincronización y, si falló, el motivo (credenciales caducadas, cuota de Google…).

## Desvincular

**Desvincular** borra la empresa en Google: **todos los móviles se restablecen de fábrica** y dejan de estar gestionados. Solo tiene sentido si dejas de usar el MDM para móviles.

## Comprobaciones para el primer despliegue

Nada de esto se ha probado contra Google real: las pruebas usan un simulador de la API. Antes de inscribir móviles de empleados, comprueba con un móvil de prueba:

- [ ] Guardar la clave y vincular la empresa; la vuelta desde Google lleva a la consola con la empresa vinculada.
- [ ] Inscribir un móvil con el QR de un token con grupo: aparece en Dispositivos en ese grupo, con su inventario.
- [ ] Una política con contraseña y cámara desactivada: el móvil pide el código y la cámara no abre. Quitar la contraseña en el móvil no es posible.
- [ ] La pestaña Cumplimiento refleja lo que el móvil no cumple.
- [ ] Una aplicación de Google Play «Instalar» se instala sola; «Desinstalar» la quita.
- [ ] Bloquear, modo perdido (con mensaje y teléfono), salir del modo perdido y cambiar la contraseña.
- [ ] Borrar el móvil lo restablece y aparece revocado.
- [ ] Wi-Fi de empresa: el móvil se conecta solo a la red.
