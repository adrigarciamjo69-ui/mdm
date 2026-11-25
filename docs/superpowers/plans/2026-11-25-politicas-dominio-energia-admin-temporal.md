# Plan: unir al dominio, energía y administrador temporal

Diseño: `docs/superpowers/specs/2026-11-25-politicas-dominio-energia-admin-temporal-design.md`.
Cada tarea: prueba que falla → código → pruebas verdes → commit.

1. **Catálogo**: `Field.OptionLabels`; ajustes `windows.domain_join` y
   `windows.power` (con `check` de dominio/OU); `protocol.SettingKeys`.
   Pruebas de validación en `internal/server/policy`.
2. **Secretos al agente**: `agentapi.getPolicy` descifra los `TypeSecret` de
   Windows/Linux. Prueba en `agentapi/policy_test.go`.
3. **Scripts del agente**: `scripts/windows.domain_join.ps1` y
   `scripts/windows.power.ps1`; prueba de que todo ajuste de Windows tiene script
   y de que el secreto llega como `MDM_PARAM_PASSWORD`.
4. **Orden en el servidor**: `protocol` (tipos, carga, capacidad
   `tempadmin.v1`), `batches.Build`, roles, SQL de capacidades, detalles de
   auditoría. Pruebas en `batches` / `adminapi` / `store`.
5. **Agente `tempadmin`**: gestor con estado en disco, barrido, ampliación,
   negativa si ya era administrador; implementaciones Windows/Linux con
   ejecutor inyectado. Pruebas con SO falso.
6. **Cableado del agente**: `commands` (TempAdmin/TempAdminRevoke), `main.go`,
   capacidad. Pruebas en `commands`.
7. **Consola**: etiquetas de enum y ayuda en `SettingForm`; botones y diálogo;
   aviso de concesiones vigentes en el equipo. Pruebas vitest.
8. **Documentación**: `docs/politicas.md`, guía de órdenes.
