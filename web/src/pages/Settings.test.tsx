import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { apiError, mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me, page } from "../test/fixtures";

const values = { agent_auto_update: true, max_offline_days: 14, min_agent_version: "", min_os_version: { windows: "", linux: "" } };
const releases = { version: "1.2.0", binaries: ["mdm-agent-linux-amd64", "mdm-agent-windows-amd64.exe"], pins: [{ group_id: "g1", group_name: "Piloto", version: "1.1.0" }] };
const groups = [{ id: "g1", name: "Piloto", kind: "static", description: "", device_count: 3 }, { id: "g2", name: "Ventas", kind: "static", description: "", device_count: 9 },
  { id: "g3", name: "Linux", kind: "dynamic", description: "", device_count: 4 }];

function setup(extra: Record<string, unknown> = {}) {
  return mockApi({
    "GET /auth/me": { body: me() }, "GET /settings": { body: values }, "GET /agent-releases": { body: releases }, "GET /groups": { body: groups },
    "GET /jobs": { body: page([{ id: 7, kind: "groups.recompute", attempts: 5, last_error: "regla ilegible", run_at: "", created_at: "", finished_at: null, failed_at: "2026-11-16T10:00:00Z" }]) },
    "PUT /settings": (c) => ({ body: { ...values, ...(c.body as object) } }),
    "PUT /agent-pins/g2": { body: { group_id: "g2", group_name: "Ventas", version: "1.2.0" } },
    "DELETE /agent-pins/g1": { status: 204 },
    ...(extra as object),
  });
}

describe("Ajustes", () => {
  it("solo un administrador ve Ajustes en el menú", async () => {
    mockApi({ "GET /auth/me": { body: me("operator") }, "GET /summary": { body: {} } });
    renderApp("/");
    await screen.findByRole("link", { name: "Inventario" });
    expect(screen.queryByRole("link", { name: "Ajustes" })).toBeNull();
  });

  it("guardar envía solo los campos cambiados", async () => {
    const calls = setup();
    renderApp("/ajustes");
    const user = userEvent.setup();
    const days = await screen.findByLabelText("Días máximos sin conexión");
    await waitFor(() => expect(days).toHaveValue(14));
    await user.clear(days);
    await user.type(days, "30");
    await user.click(screen.getByRole("button", { name: "Guardar cumplimiento" }));
    expect(await screen.findByText("Ajustes guardados.")).toBeInTheDocument();
    expect(calls.find((c) => c.method === "PUT" && c.path === "/settings")!.body).toEqual({ max_offline_days: 30 });
  });

  it("una versión mínima con formato inválido muestra el error del servidor", async () => {
    setup({ "PUT /settings": apiError(400, "invalid_request", "La versión mínima del agente no tiene un formato válido (por ejemplo, 1.2.0).") });
    renderApp("/ajustes");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Versión mínima del agente"), "uno");
    await user.click(screen.getByRole("button", { name: "Guardar cumplimiento" }));
    expect(await screen.findByText(/no tiene un formato válido/)).toBeInTheDocument();
  });

  it("congelar un grupo llama a PUT /agent-pins/{id} y descongelar a DELETE", async () => {
    const calls = setup();
    renderApp("/ajustes");
    const user = userEvent.setup();
    expect(await screen.findByText(/Versión distribuida: 1.2.0/)).toBeInTheDocument();
    await screen.findByRole("option", { name: "Ventas" });
    expect(within(screen.getByLabelText("Grupo a congelar")).queryByRole("option", { name: "Linux" })).toBeNull();
    await user.selectOptions(screen.getByLabelText("Grupo a congelar"), "g2");
    await user.click(screen.getByRole("button", { name: "Congelar" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PUT" && c.path === "/agent-pins/g2")?.body).toEqual({ version: "1.2.0" }));
    await user.click(screen.getByRole("button", { name: "Descongelar Piloto" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/agent-pins/g1")).toBe(true));
  });

  it("la sección Tareas lista las tareas fallidas con su error", async () => {
    setup();
    renderApp("/ajustes");
    const row = within((await screen.findByText("groups.recompute")).closest("tr")!);
    expect(row.getByText("regla ilegible")).toBeInTheDocument();
    expect(row.getByText("5")).toBeInTheDocument();
  });
});
