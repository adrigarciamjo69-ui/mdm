import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { apiError, mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { detail, inventory, me, page } from "../test/fixtures";

const groups = [{ id: "g1", name: "Contabilidad", description: "", device_count: 1 }, { id: "g2", name: "Ventas", description: "", device_count: 0 }];

function setup(role: "admin" | "operator" | "readonly" = "admin", d = detail()) {
  return mockApi({
    "GET /auth/me": { body: me(role) },
    "GET /groups": { body: groups },
    "GET /devices/d1": { body: d },
    "GET /devices/d1/software": (c) => {
      const all = [
        { name: "7-Zip", version: "24.08", publisher: "Igor Pavlov", installed_at: null },
        { name: "Mozilla Firefox", version: "132.0", publisher: "Mozilla", installed_at: "2026-01-15T00:00:00Z" },
      ];
      const q = new URLSearchParams(c.path.split("?")[1] ?? "").get("q") ?? "";
      const items = all.filter((s) => s.name.toLowerCase().includes(q.toLowerCase()));
      return { body: page(items) };
    },
    "GET /devices/d1/activity": { body: [{ hour: "2026-11-13T09:00:00Z", checkins: 58 }] },
    "PATCH /devices/d1": (c) => ({ body: { ...d, ...(c.body as object), group_name: "Ventas" } }),
    "POST /devices/d1/revoke": { status: 204 },
    "DELETE /devices/d1": { status: 204 },
    "GET /devices": { body: page([]) },
  });
}

async function openTab(name: string) {
  await userEvent.setup().click(await screen.findByRole("tab", { name }));
}

describe("Ficha de dispositivo", () => {
  it("la cabecera muestra nombre, modelo, serie, estado e identificador", async () => {
    setup();
    renderApp("/dispositivos/d1");
    expect(await screen.findByRole("heading", { level: 1, name: "PC-CONTA-07" })).toBeInTheDocument();
    expect(screen.getByText("Lenovo ThinkPad T14")).toBeInTheDocument();
    expect(screen.getByText("PF3K9X")).toBeInTheDocument();
    expect(screen.getAllByText("En línea").length).toBeGreaterThan(0);
  });

  it("la pestaña Almacenamiento muestra cada volumen con usado, libre y cifrado", async () => {
    setup();
    renderApp("/dispositivos/d1");
    await openTab("Almacenamiento");
    const c = within(screen.getByRole("row", { name: /C:/ }));
    expect(c.getByText("512 GB")).toBeInTheDocument();
    expect(c.getByText(/200 GB libres/)).toBeInTheDocument();
    expect(c.getByText("Cifrado")).toBeInTheDocument();
    expect(within(screen.getByRole("row", { name: /D:/ })).getByText("Sin cifrar")).toBeInTheDocument();
  });

  it("marca un volumen con menos del 10 % libre", async () => {
    setup();
    renderApp("/dispositivos/d1");
    await openTab("Almacenamiento");
    const d = within(screen.getByRole("row", { name: /D:/ }));
    expect(d.getByText(/5 GB libres/).closest(".usage")).toHaveClass("low");
    expect(d.getByText(/poco espacio/i)).toBeInTheDocument();
    expect(within(screen.getByRole("row", { name: /C:/ })).queryByText(/poco espacio/i)).toBeNull();
  });

  it("la pestaña Software filtra por nombre y pagina", async () => {
    const calls = setup();
    renderApp("/dispositivos/d1");
    await openTab("Software");
    expect(await screen.findByText("Mozilla Firefox")).toBeInTheDocument();
    await userEvent.setup().type(screen.getByRole("searchbox", { name: "Buscar programa" }), "fire");
    await waitFor(() => expect(screen.queryByText("7-Zip")).toBeNull());
    expect(calls.some((c) => c.path.includes("/software?") && c.path.includes("q=fire"))).toBe(true);
  });

  it("una sección no disponible muestra el aviso en lugar de datos vacíos", async () => {
    setup("admin", detail({ inventory: inventory({ unavailable: ["security"] }) }));
    renderApp("/dispositivos/d1");
    await openTab("Seguridad");
    expect(screen.getByText("No se pudo recoger este dato en el último inventario.")).toBeInTheDocument();
  });

  it("sin inventario todavía lo explica", async () => {
    setup("admin", detail({ inventory: null, inventory_collected_at: null }));
    renderApp("/dispositivos/d1");
    await openTab("Hardware");
    expect(screen.getByText(/todavía no ha enviado su inventario/)).toBeInTheDocument();
  });

  it("un operador edita grupo y etiquetas y ve «Cambios guardados.»", async () => {
    const calls = setup("operator");
    renderApp("/dispositivos/d1");
    const user = userEvent.setup();
    await screen.findByRole("option", { name: "Ventas" });
    await user.selectOptions(screen.getByLabelText("Grupo"), "g2");
    expect(screen.queryByRole("textbox", { name: "Usuario asignado" })).toBeNull(); // la persona se cambia en la pestaña Activo
    await user.clear(screen.getByLabelText("Etiquetas"));
    await user.type(screen.getByLabelText("Etiquetas"), "vip, planta-3");
    await user.click(screen.getByRole("button", { name: "Guardar cambios" }));
    expect(await screen.findByText("Cambios guardados.")).toBeInTheDocument();
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ group_id: "g2", tags: ["vip", "planta-3"] });
  });

  it("guardar sin cambios no llama al servidor", async () => {
    const calls = setup("operator");
    renderApp("/dispositivos/d1");
    expect(await screen.findByRole("button", { name: "Guardar cambios" })).toBeDisabled();
    expect(calls.some((c) => c.method === "PATCH")).toBe(false);
  });

  it("un rol de solo lectura no ve controles de edición", async () => {
    setup("readonly");
    renderApp("/dispositivos/d1");
    await screen.findByRole("heading", { level: 1, name: "PC-CONTA-07" });
    expect(screen.queryByRole("button", { name: "Guardar cambios" })).toBeNull();
    expect(screen.queryByLabelText("Grupo")).toBeNull();
    expect(screen.getByText("Contabilidad")).toBeInTheDocument();
  });

  it("solo un administrador ve «Revocar» y «Eliminar»", async () => {
    setup("operator");
    renderApp("/dispositivos/d1");
    await screen.findByRole("heading", { level: 1, name: "PC-CONTA-07" });
    expect(screen.queryByRole("button", { name: "Revocar" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Eliminar" })).toBeNull();
  });

  it("revocar pide confirmación y actualiza la ficha", async () => {
    const calls = setup();
    renderApp("/dispositivos/d1");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Revocar" }));
    const dialog = screen.getByRole("dialog", { name: "Revocar este dispositivo" });
    expect(dialog).toHaveTextContent("dejará de comunicarse con el servidor");
    await user.click(within(dialog).getByRole("button", { name: "Revocar" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/devices/d1/revoke")).toBe(true));
    await waitFor(() => expect(calls.filter((c) => c.path === "/devices/d1").length).toBeGreaterThan(1));
  });

  it("eliminar exige escribir el nombre del equipo antes de habilitar el botón", async () => {
    const calls = setup();
    const { location } = renderApp("/dispositivos/d1");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Eliminar" }));
    const dialog = screen.getByRole("dialog", { name: "Eliminar este dispositivo" });
    const confirm = within(dialog).getByRole("button", { name: "Eliminar" });
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByRole("textbox"), "PC-CONTA-0");
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByRole("textbox"), "7");
    expect(confirm).toBeEnabled();
    await user.click(confirm);
    await waitFor(() => expect(location()).toBe("/inventario?origin=agent"));
    expect(calls.some((c) => c.method === "DELETE" && c.path === "/devices/d1")).toBe(true);
  });

  it("un 404 muestra «Este dispositivo ya no existe.» con enlace al listado", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /groups": { body: [] }, "GET /devices/d1": apiError(404, "not_found", "Este dispositivo ya no existe.") });
    renderApp("/dispositivos/d1");
    expect(await screen.findByText("Este dispositivo ya no existe.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Volver al inventario" })).toHaveAttribute("href", "/inventario?origin=agent");
  });

  it("muestra la actividad de las últimas horas", async () => {
    setup();
    renderApp("/dispositivos/d1");
    await openTab("Conexiones");
    expect(await screen.findByText(/58 conexiones/)).toBeInTheDocument();
  });
});

describe("Grupo principal", () => {
  it("no ofrece los grupos dinámicos", async () => {
    mockApi({
      "GET /auth/me": { body: me("operator") },
      "GET /devices/d1": { body: detail() },
      "GET /groups": { body: [{ id: "g1", name: "Ventas", kind: "static", description: "", device_count: 1 }, { id: "g2", name: "Linux", kind: "dynamic", description: "", device_count: 4 }] },
    });
    renderApp("/dispositivos/d1");
    await screen.findByRole("option", { name: "Ventas" });
    expect(screen.queryByRole("option", { name: "Linux" })).toBeNull();
  });
});
