import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me, page, summary } from "../test/fixtures";

const alerts = [
  { id: 812, rule_id: "r1", rule_name: "Servidores sin conexión", device_id: "d1", hostname: "srv-db-01", state: "open", severity: "critical",
    title: "srv-db-01 lleva más de 1 h sin conectar", details: {}, opened_at: "2026-11-18T10:00:00Z", acknowledged_at: null, resolved_at: null, resolved_by_hand: false },
];

describe("Alertas", () => {
  it("lista las abiertas, reconoce y muestra el contador en el menú", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me("operator") }, "GET /alerts": { body: page(alerts) }, "GET /alerts/counts": { body: { critical: 1, warning: 0, info: 0 } },
      "POST /alerts/812/acknowledge": { status: 204 } });
    renderApp("/alertas");
    const row = within((await screen.findByText(/srv-db-01 lleva más de 1 h/)).closest("tr")!);
    expect(row.getByText("Crítica")).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: /Alertas 1/ })).toBeInTheDocument();
    await userEvent.setup().click(row.getByRole("button", { name: "Reconocer" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/alerts/812/acknowledge")).toBe(true));
    expect(calls.find((c) => c.path.startsWith("/alerts?"))!.path).toContain("state=unresolved");
  });
});

describe("Reglas de alerta", () => {
  it("el tipo cambia los parámetros y guardar envía la regla", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me() }, "GET /channels": { body: [{ id: "c1", name: "Correo IT", type: "email", recipients: ["it@x.es"], url: "", last_error: "" }] },
      "GET /groups": { body: [] }, "POST /alert-rules": { status: 201, body: { id: "r9" } }, "GET /alert-rules": { body: [] } });
    const { location } = renderApp("/alertas/reglas/nueva");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Disco lleno");
    await user.selectOptions(screen.getByLabelText("Tipo"), "disk.low");
    await user.clear(screen.getByLabelText("Porcentaje libre por debajo de"));
    await user.type(screen.getByLabelText("Porcentaje libre por debajo de"), "15");
    await user.click(screen.getByLabelText("Correo IT"));
    await user.click(screen.getByRole("button", { name: "Guardar regla" }));
    await waitFor(() => expect(location()).toBe("/alertas/reglas"));
    expect(calls.find((c) => c.method === "POST")!.body).toMatchObject({ name: "Disco lleno", type: "disk.low", params: { percent: 15 },
      channels: ["c1"], assignments: [{ group_id: null, exclude: false }] });
  });
});

describe("Avisos", () => {
  it("crear un webhook muestra su secreto una sola vez", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /integrations/email": { body: { configured: false } }, "GET /channels": { body: [] },
      "POST /channels": { status: 201, body: { id: "c2", name: "Teams", type: "webhook", recipients: [], url: "https://x.es/h", secret: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", last_error: "" } } });
    renderApp("/integraciones/avisos");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre del canal"), "Teams");
    await user.selectOptions(screen.getByLabelText("Tipo de canal"), "webhook");
    await user.type(screen.getByLabelText("Dirección (URL)"), "https://x.es/h");
    await user.click(screen.getByRole("button", { name: "Crear canal" }));
    expect(await screen.findByText("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4")).toBeInTheDocument();
    expect(screen.getByText(/no se volverá a mostrar/)).toBeInTheDocument();
  });
});

describe("Informes", () => {
  it("muestra el informe con sus filtros y el enlace CSV", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /groups": { body: [] }, "GET /channels": { body: [] }, "GET /report-schedules": { body: [] },
      "GET /reports": { body: [{ name: "inactive", title: "Equipos inactivos", description: "x", columns: ["Equipo", "Días"], filters: ["days"] }] },
      "GET /reports/inactive": { body: { columns: ["Equipo", "Días"], rows: [["PC-VIEJO", "45"]], total: 1 } } });
    renderApp("/informes/inactive");
    expect(await screen.findByText("PC-VIEJO")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Exportar CSV" }).getAttribute("href")).toContain("/api/v1/reports/inactive?");
    expect(screen.getByRole("link", { name: "Exportar CSV" }).getAttribute("href")).toContain("format=csv");
  });
});

describe("Automatizaciones", () => {
  it("un operador no puede elegir reiniciar; simular muestra los equipos", async () => {
    mockApi({ "GET /auth/me": { body: me("operator") }, "GET /groups": { body: [] }, "GET /channels": { body: [] }, "GET /scripts": { body: [] },
      "GET /alert-rules": { body: [] },
      "GET /automations/a1": { body: { id: "a1", name: "Etiquetar", enabled: true, trigger: { type: "schedule", frequency: "daily", time: "02:00" },
        actions: [{ type: "add_tag", tag: "x" }], max_devices: 500, assignments: [], needs_admin: false, last_run_at: null, next_run_at: "2026-11-19T01:00:00Z" } },
      "GET /automations/a1/runs": { body: [] },
      "POST /automations/a1/simulate": { body: { device_count: 2, devices: [{ id: "d1", hostname: "PC-1", platform: "windows" }, { id: "d2", hostname: "PC-2", platform: "windows" }],
        actions: ["Añadir etiqueta «x»"], blocked: false, max_devices: 500 } } });
    renderApp("/automatizaciones/a1");
    const user = userEvent.setup();
    const addAction = await screen.findByLabelText("Añadir acción");
    expect(within(addAction).getByRole("option", { name: "Reiniciar (solo administradores)" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Simular" }));
    expect(await screen.findByText(/Afectaría a 2 equipos/)).toBeInTheDocument();
    expect(screen.getByText("PC-1, PC-2")).toBeInTheDocument();
  });
});

describe("Resumen personalizable", () => {
  it("respeta las secciones elegidas", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /summary": { body: summary() }, "GET /me/preferences": { body: { summary_sections: ["alerts"] } },
      "GET /alerts/counts": { body: { critical: 2, warning: 0, info: 0 } }, "GET /alerts": { body: page([]) } });
    renderApp("/");
    expect(await screen.findByRole("heading", { name: "Alertas abiertas" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Pendiente de revisar" })).toBeNull();
  });
});
