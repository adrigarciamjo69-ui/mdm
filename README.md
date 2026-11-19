# MDM

Plataforma propia y autoalojada de gestión de dispositivos para Windows y Linux, pensada para empresas de 100 a 1.000 equipos.

- **Fase 1, núcleo:** inscripción de equipos con un comando, inventario de hardware, sistema, seguridad y software, y una consola web con administración segura (segundo factor obligatorio, roles y auditoría).
- **Fase 2, políticas:** políticas de configuración con cumplimiento (auditar o aplicar), grupos dinámicos por reglas, órdenes remotas (reiniciar, apagar, actualizar inventario, retirar), biblioteca de scripts con versiones, custodia de claves de BitLocker y actualización automática de los agentes. Ver [docs/politicas.md](docs/politicas.md).

- **Fase 3, software y parches:** catálogo de aplicaciones (winget, apt, dnf e instaladores propios subidos a la consola), despliegues con reintentos y ventanas de mantenimiento, y parches del sistema con aprobaciones y reinicios controlados. Ver [docs/software.md](docs/software.md).

- **Fase 4, Android:** móviles y tabletas de empresa a través de Google (Android Enterprise): inscripción con código QR, políticas, aplicaciones de Google Play, cumplimiento y acciones remotas (bloquear, modo perdido, borrar). Ver [docs/android.md](docs/android.md).

- **Fase 6, alertas, informes y automatizaciones:** alertas con avisos por correo y webhook firmado (agrupados), informes predefinidos con exportación CSV y envío programado, automatizaciones con simulación y salvaguardas, y un resumen personalizable con tendencias de 90 días. Ver [docs/alertas.md](docs/alertas.md) y [docs/webhooks.md](docs/webhooks.md).

Queda fuera, por decisión del diseño: los iPhone y los móviles personales (perfil de trabajo), y el control remoto en directo.

## Piezas

El **servidor** (`cmd/mdm-server`) es un único binario de Go que sirve la API de los agentes, la API de la consola y la propia consola, sobre PostgreSQL. El **agente** (`cmd/mdm-agent`) es un binario de Go para Windows y Linux que se instala como servicio y solo abre conexiones salientes por HTTPS. La **consola** (`web/`) es una aplicación React que se compila y se incrusta en el servidor.

## Empezar

- Desplegar el servidor: [docs/despliegue.md](docs/despliegue.md)
- Instalar el agente en los equipos: [docs/agente.md](docs/agente.md)
- Validar el agente en Windows antes de cada versión: [docs/validacion-windows.md](docs/validacion-windows.md)
- Diseño: [docs/superpowers/specs/2026-11-12-mdm-nucleo-design.md](docs/superpowers/specs/2026-11-12-mdm-nucleo-design.md)

## Desarrollo

Hace falta Go 1.23, Node 22 y un PostgreSQL 16 para las pruebas.

```sh
# Pruebas de Go (las que usan base de datos leen MDM_TEST_DATABASE_URL)
export MDM_TEST_DATABASE_URL='postgres://mdm:mdm@localhost:5432/mdm_test?sslmode=disable'
make test           # todas
make test-short     # sin las que necesitan PostgreSQL

# Consola
cd web && npm ci && npx vitest run && npm run dev

# Binarios: servidor y agentes para linux/windows en amd64/arm64, en dist/
make web build

# Instalador de Linux y prueba de extremo a extremo con Docker
sudo sh scripts/install_test.sh
sh scripts/e2e.sh
```

La consola en desarrollo (`npm run dev`) envía `/api` a un servidor local en `https://localhost:8443`.

## Estructura

```
cmd/            mdm-server y mdm-agent
internal/
  protocol/     mensajes entre agente y servidor
  server/       almacén (único con SQL), autenticación, las dos API, ensamblado
  agent/        configuración, cliente, inventario, inscripción, bucle, servicio
migrations/     esquema de PostgreSQL
web/            consola
deploy/         Dockerfile, Docker Compose, Caddy
scripts/        instaladores, prueba de extremo a extremo y simulador de carga
docs/           guías y diseño
```
