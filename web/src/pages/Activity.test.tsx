import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { device, me, page } from "../test/fixtures";
import { auditLabel } from "../lib/auditLabels";

const batch = { id: "b1", type: "run_script", params: { script_name: "Limpiar", version: 2 }, target_description: "Grupo Ventas (2 equipos)",
  device_count: 2, created_by_email: "ana@ejemplo.es", created_at: "2026-11-16T10:00:00Z",
  counts: { pending: 1, sent: 0, running: 0, succeeded: 1, failed: 0, expired: 0, cancelled: 0 } };
const commands = [
  { id: "c1", device_id: "d1", hostname: "PC-1", type: "run_script", status: "succeeded", exit_code: 0, error: "", created_at: "", expires_at: "" },
  { id: "c2", device_id: "d2", hostname: "PC-2", type: "run_script", status: "pending", exit_code: null, error: "", created_at: "", expires_at: "" },
];

describe("Actividad", () => {
  it("lista los lotes con su progreso", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /batches": { body: page([batch]) } });
    renderApp("/actividad");
    const row = within((await screen.findByText("Grupo Ventas (2 equipos)")).closest("tr")!);
    expect(row.getByText(/Ejecutar script.*Limpiar/)).toBeInTheDocument();
    expect(row.getByText("1 de 2 terminadas")).toBeInTheDocument();
  });

  it("el detalle del lote muestra cada equipo y su salida", async () => {
    mockApi({
      "GET /auth/me": { body: me() }, "GET /batches/b1": { body: { ...batch, commands } },
      "GET /commands/c1": { body: { ...commands[0], stdout: "Borrados 12 ficheros\n", stderr: "" } },
    });
    renderApp("/actividad/b1");
    const user = userEvent.setup();
    const row = (await screen.findByText("PC-1")).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Ver salida" }));
    expect(await screen.findByText(/Borrados 12 ficheros/)).toBeInTheDocument();
  });

  it("cancelar una orden pendiente la marca cancelada", async () => {
    let status = "pending";
    const calls = mockApi({
      "GET /auth/me": { body: me("operator") },
      "GET /batches/b1": () => ({ body: { ...batch, commands: [commands[0], { ...commands[1], status }] } }),
      "POST /commands/c2/cancel": () => { status = "cancelled"; return { status: 204 }; },
    });
    renderApp("/actividad/b1");
    const user = userEvent.setup();
    const row = (await screen.findByText("PC-2")).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(within(screen.getByText("PC-2").closest("tr")!).getByText("Cancelada")).toBeInTheDocument());
    expect(calls.some((c) => c.path === "/commands/c2/cancel")).toBe(true);
  });
});

describe("Acciones desde el listado", () => {
  it("seleccionar equipos muestra la barra de acciones según el rol", async () => {
    mockApi({ "GET /auth/me": { body: me("operator") }, "GET /groups": { body: [] },
      "GET /devices": { body: page([device(), device({ id: "d2", hostname: "PC-2" })]) } });
    renderApp("/dispositivos");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Seleccionar PC-CONTA-07" }));
    const bar = screen.getByRole("toolbar", { name: "Acciones" });
    expect(within(bar).getByText("1 seleccionado")).toBeInTheDocument();
    expect(within(bar).getByRole("button", { name: "Reiniciar" })).toBeInTheDocument();
    expect(within(bar).queryByRole("button", { name: "Ejecutar script" })).toBeNull();
    expect(within(bar).queryByRole("button", { name: "Retirar" })).toBeNull();
  });
});

describe("auditLabel", () => {
  it("traduce todas las acciones de la fase 2", () => {
    for (const a of ["settings.updated", "policy.created", "policy.updated", "policy.deleted", "policy.assigned", "batch.created",
      "command.cancelled", "device.retired", "script.created", "script.updated", "script.archived", "recovery_key.viewed", "agent.updated", "agent.pinned"]) {
      expect(auditLabel(a)).not.toBe(a);
    }
  });
});
