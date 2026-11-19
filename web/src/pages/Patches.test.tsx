import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { detail, me, page } from "../test/fixtures";
import { describeWindow } from "../lib/patches";

const updates = [
  { id: "u1", platform: "windows", kb: "KB5046617", title: "Actualización acumulativa", classification: "security", severity: "Critical",
    size_bytes: 0, published_at: null, security: true, missing: 12, installed: 30, failed: 1, decision: null },
  { id: "u2", platform: "windows", kb: "KB9", title: "Controlador de vídeo", classification: "driver", severity: "", size_bytes: 0,
    published_at: null, security: false, missing: 3, installed: 0, failed: 0, decision: "declined" },
];

function setup(role: "admin" | "operator" = "admin") {
  return mockApi({
    "GET /auth/me": { body: me(role) }, "GET /updates": { body: page(updates) }, "GET /patch-policies": { body: [] },
    "PUT /updates/u1/approval": { status: 204 }, "PUT /updates/u2/approval": { status: 204 },
  });
}

describe("Parches", () => {
  it("aprobar y rechazar envían la decisión; volver a la regla envía null", async () => {
    const calls = setup();
    renderApp("/parches");
    const user = userEvent.setup();
    const row = within((await screen.findByText("Actualización acumulativa")).closest("tr")!);
    expect(row.getByText("12")).toBeInTheDocument();
    await user.click(row.getByRole("button", { name: "Aprobar" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/updates/u1/approval")?.body).toEqual({ decision: "approved" }));
    const declined = within(screen.getByText("Controlador de vídeo").closest("tr")!);
    expect(declined.getByText("Rechazada")).toBeInTheDocument();
    await user.click(declined.getByRole("button", { name: "Volver a la regla" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/updates/u2/approval")?.body).toEqual({ decision: null }));
  });

  it("un operador ve las actualizaciones pero no los botones de decisión", async () => {
    setup("operator");
    renderApp("/parches");
    await screen.findByText("Actualización acumulativa");
    expect(screen.queryByRole("button", { name: "Aprobar" })).toBeNull();
  });

  it("describe una ventana que cruza la medianoche", () => {
    expect(describeWindow({ days: [1, 2, 3, 4, 5], start: "22:00", end: "05:00" })).toBe("De lunes a viernes, de 22:00 a 05:00 del día siguiente");
    expect(describeWindow({ days: [0, 1, 2, 3, 4, 5, 6], start: "13:00", end: "15:00" })).toBe("Todos los días, de 13:00 a 15:00");
    expect(describeWindow({ days: [6], start: "08:00", end: "08:00" })).toBe("Los sábados, todo el día");
  });

  it("el editor de políticas de parches guarda la ventana y el reinicio", async () => {
    const calls = mockApi({
      "GET /auth/me": { body: me() }, "GET /groups": { body: [] },
      "POST /patch-policies": { status: 201, body: { id: "p1", name: "Noches", platform: "windows", priority: 1, assignments: [] } },
      "PUT /patch-policies/p1/assignments": { body: {} },
      "GET /patch-policies/p1": { body: { id: "p1", name: "Noches", platform: "windows", priority: 1, auto_approve: ["security"], defer_days: 7,
        window: { days: [1, 2, 3, 4, 5], start: "22:00", end: "05:00" }, reboot: "in_window", notify_minutes: 15, linux_scope: "security", excluded_kbs: [], assignments: [] } },
    });
    const { location } = renderApp("/parches/politicas/nueva");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Noches");
    await user.clear(screen.getByLabelText("Días de espera desde la publicación"));
    await user.type(screen.getByLabelText("Días de espera desde la publicación"), "7");
    expect(screen.getByText("De lunes a viernes, de 22:00 a 05:00 del día siguiente")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Guardar política de parches" }));
    await waitFor(() => expect(location()).toBe("/parches/politicas/p1"));
    expect(calls.find((c) => c.method === "POST")!.body).toMatchObject({ name: "Noches", platform: "windows", defer_days: 7,
      auto_approve: ["security", "critical", "definition"], window: { days: [1, 2, 3, 4, 5], start: "22:00", end: "05:00" }, reboot: "in_window" });
  });

  it("la pestaña Actualizaciones muestra pendientes, reinicio pendiente y lanza Buscar ahora", async () => {
    const calls = mockApi({
      "GET /auth/me": { body: me("operator") }, "GET /devices/d1": { body: detail() }, "GET /groups": { body: [] },
      "GET /devices/d1/updates": { body: { state: { last_scan_at: "2026-11-17T08:00:00Z", last_install_at: null, reboot_pending: true,
        window: { days: [1, 2, 3, 4, 5], start: "22:00", end: "05:00" } },
        updates: [{ id: "u1", kb: "KB5046617", title: "Actualización acumulativa", classification: "security", severity: "Critical", security: true,
          state: "missing", approved: true, first_seen_at: "2026-11-10T00:00:00Z", installed_at: null, last_error: "" }] } },
      "POST /batches": { status: 201, body: { batch: { id: "b1" }, skipped: [] } },
    });
    renderApp("/dispositivos/d1");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("tab", { name: "Actualizaciones" }));
    expect(await screen.findByText(/Reinicio pendiente/)).toBeInTheDocument();
    expect(within(screen.getByText("Actualización acumulativa").closest("tr")!).getByText("Aprobada")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Buscar ahora" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/batches")?.body).toEqual({ type: "scan_updates", device_ids: ["d1"] }));
  });
});
