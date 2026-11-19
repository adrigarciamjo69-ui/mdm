import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { detail, me } from "../test/fixtures";

const apps = [
  { id: "a1", name: "Firefox", platform: "windows", source: "winget", package_id: "Mozilla.Firefox", archived: false, current_version: null, versions: [] },
  { id: "a2", name: "Contabilidad Pro", platform: "windows", source: "upload", package_id: "", archived: false, current_version: { id: "v1", version: "3.2.1" }, versions: [] },
];
const groups = [{ id: "g1", name: "Contabilidad", kind: "static", description: "", device_count: 14 }];
const dep = { id: "d1", app_id: "a2", app_name: "Contabilidad Pro", app_source: "upload", platform: "windows", intent: "required", keep_updated: false,
  start_at: null, respect_window: false, reboot: "never", assignments: [{ group_id: "g1", group_name: "Contabilidad", exclude: false }],
  counts: { pending: 2, installed: 11, failed: 1 }, created_at: "" };

function setup(extra: Record<string, unknown> = {}) {
  return mockApi({
    "GET /auth/me": { body: me() }, "GET /apps": { body: apps }, "GET /groups": { body: groups },
    "GET /deployments": { body: [dep] }, "GET /deployments/d1": { body: dep },
    "GET /deployments/d1/status": { body: { counts: dep.counts, devices: [
      { device_id: "x1", hostname: "PC-CONTA-01", state: "installed", message: "", attempts: 0, exit_code: 0 },
      { device_id: "x2", hostname: "PC-CONTA-02", state: "failed", message: "terminó con el código 1603: error fatal", attempts: 4, exit_code: 1603 },
    ] } },
    "POST /deployments": { status: 201, body: { ...dep, id: "d9" } },
    "PUT /deployments/d9/assignments": { body: dep },
    "POST /deployments/d1/retry": { status: 204 },
    ...(extra as object),
  });
}

describe("Despliegues", () => {
  it("la lista muestra la aplicación, la intención y los recuentos", async () => {
    setup();
    renderApp("/despliegues");
    const row = within((await screen.findByText("Contabilidad Pro")).closest("tr")!);
    expect(row.getByText("Instalar")).toBeInTheDocument();
    expect(row.getByText(/11 instalados/)).toBeInTheDocument();
    expect(row.getByText(/1 con error/)).toBeInTheDocument();
  });

  it("mantener actualizada solo se ofrece para winget, apt y dnf", async () => {
    setup();
    renderApp("/despliegues/nuevo");
    const user = userEvent.setup();
    await screen.findByRole("option", { name: "Contabilidad Pro" });
    await user.selectOptions(screen.getByLabelText("Aplicación"), "a2");
    expect(screen.queryByLabelText(/Mantener actualizada/)).toBeNull();
    await user.selectOptions(screen.getByLabelText("Aplicación"), "a1");
    expect(screen.getByLabelText(/Mantener actualizada/)).toBeInTheDocument();
  });

  it("guardar envía la intención, las opciones y las asignaciones", async () => {
    const calls = setup();
    const { location } = renderApp("/despliegues/nuevo?app=a2");
    const user = userEvent.setup();
    await screen.findAllByRole("option", { name: "Contabilidad" });
    await user.selectOptions(screen.getByLabelText("Reinicio"), "in_window");
    await user.click(screen.getByLabelText(/Solo dentro de la ventana/));
    await user.selectOptions(screen.getByLabelText("Incluir grupo"), "g1");
    await user.click(screen.getByRole("button", { name: "Guardar despliegue" }));
    await waitFor(() => expect(location()).toBe("/despliegues/d9"));
    expect(calls.find((c) => c.method === "POST" && c.path === "/deployments")!.body).toMatchObject({
      app_id: "a2", intent: "required", reboot: "in_window", respect_window: true, keep_updated: false });
    expect(calls.find((c) => c.method === "PUT")!.body).toEqual({ assignments: [{ group_id: "g1", exclude: false }] });
  });

  it("el estado muestra los equipos con su error y reintentar todos llama a /retry sin equipo", async () => {
    const calls = setup();
    renderApp("/despliegues/d1");
    const user = userEvent.setup();
    const row = within((await screen.findByText("PC-CONTA-02")).closest("tr")!);
    expect(row.getByText(/1603: error fatal/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reintentar los fallidos" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/deployments/d1/retry")?.body).toEqual({}));
    await user.click(row.getByRole("button", { name: "Reintentar" }));
    await waitFor(() => expect(calls.filter((c) => c.path === "/deployments/d1/retry").pop()?.body).toEqual({ device_id: "x2" }));
  });

  it("la ficha muestra los despliegues que le aplican con su estado", async () => {
    mockApi({
      "GET /auth/me": { body: me() }, "GET /devices/d1": { body: detail() }, "GET /groups": { body: [] },
      "GET /devices/d1/deployments": { body: [{ deployment_id: "d1", app_name: "Contabilidad Pro", intent: "required", state: "installed", message: "", attempts: 0 }] },
    });
    renderApp("/dispositivos/d1");
    await userEvent.setup().click(await screen.findByRole("tab", { name: "Software gestionado" }));
    const row = within((await screen.findByText("Contabilidad Pro")).closest("tr")!);
    expect(row.getByText("Instalado")).toBeInTheDocument();
  });
});
