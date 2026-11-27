import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { asset, device, me, page } from "../test/fixtures";


const agentAsset = asset({
  id: "a2", device_id: "d1", origin: "agent", category: "Portátil", category_id: "c-lap", name: "PC-CONTA-07", brand: "Lenovo",
  model: "ThinkPad T14", serial: "PF3K9X", status: "en_uso", owner: { id: "u1", name: "Ana Pérez", email: "ana@electrans.es" },
  device: device(),
});
const categories = [{ id: "c-lap", name: "Portátil", assets: 1 }, { id: "c-mon", name: "Monitor", assets: 1 }];

function setup(extra: Record<string, unknown> = {}, role: "admin" | "operator" | "readonly" = "operator") {
  return mockApi({
    "GET /auth/me": { body: me(role) },
    "GET /assets": { body: page([agentAsset, asset()]) },
    "GET /asset-categories": { body: categories },
    "GET /groups": { body: [] },
    "GET /alerts/counts": { body: {} },
    ...(extra as object),
  });
}

describe("Inventario", () => {
  it("mezcla equipos con agente y activos manuales", async () => {
    setup();
    renderApp("/inventario");
    const agentRow = within((await screen.findByRole("link", { name: "PC-CONTA-07" })).closest("tr")!);
    expect(screen.getByRole("link", { name: "PC-CONTA-07" })).toHaveAttribute("href", "/dispositivos/d1");
    expect(agentRow.getByText("Ana Pérez")).toBeInTheDocument();
    expect(agentRow.getByText("En línea")).toBeInTheDocument();
    const manualRow = within(screen.getByRole("link", { name: "Monitor sala" }).closest("tr")!);
    expect(screen.getByRole("link", { name: "Monitor sala" })).toHaveAttribute("href", "/inventario/a1");
    expect(manualRow.getByText("Manual")).toBeInTheDocument();
    expect(manualRow.getByText("En almacén")).toBeInTheDocument();
  });

  it("los filtros pasan a la URL y a la consulta", async () => {
    const calls = setup();
    const { location } = renderApp("/inventario");
    await screen.findByRole("link", { name: "Monitor sala" });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /^Categorías:/ }));
    await user.click(screen.getByRole("checkbox", { name: "Monitor" }));
    await user.click(screen.getByRole("checkbox", { name: "Portátil" }));
    await waitFor(() => expect(location()).toContain("category_id=c-mon&category_id=c-lap"));
    await waitFor(() => expect(calls.some((c) => c.path.startsWith("/assets?") && c.path.includes("category_id=c-mon&category_id=c-lap"))).toBe(true));
  });

  it("/dispositivos lleva al inventario filtrado por equipos con agente", async () => {
    setup();
    const { location } = renderApp("/dispositivos");
    await screen.findByRole("heading", { name: "Inventario" });
    expect(location()).toBe("/inventario?origin=agent");
  });

  it("los enlaces antiguos a /dispositivos conservan sus filtros", async () => {
    setup();
    const { location } = renderApp("/dispositivos?status=offline&group_id=g1&issue=low_disk");
    await screen.findByRole("heading", { name: "Inventario" });
    const got = new URLSearchParams(location().split("?")[1]);
    expect(location().startsWith("/inventario?")).toBe(true);
    expect(Object.fromEntries(got)).toEqual({ origin: "agent", device_status: "offline", group_id: "g1", issue: "low_disk" });
  });

  it("las órdenes solo aparecen al seleccionar equipos con agente", async () => {
    setup();
    renderApp("/inventario");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Seleccionar Monitor sala" }));
    expect(screen.queryByRole("button", { name: "Reiniciar" })).toBeNull();
    expect(screen.getByRole("button", { name: "Cambiar en bloque…" })).toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Seleccionar PC-CONTA-07" }));
    expect(screen.getByRole("button", { name: "Reiniciar" })).toBeInTheDocument();
  });

  it("cambia el estado de varios activos a la vez", async () => {
    const calls = setup({ "POST /assets/bulk": { body: { updated: 2 } } });
    renderApp("/inventario");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Seleccionar Monitor sala" }));
    await user.click(screen.getByRole("checkbox", { name: "Seleccionar PC-CONTA-07" }));
    await user.click(screen.getByRole("button", { name: "Cambiar en bloque…" }));
    const dialog = within(screen.getByRole("dialog"));
    await user.selectOptions(dialog.getByLabelText("Estado"), "en_reparacion");
    await user.click(dialog.getByRole("button", { name: "Aplicar" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/assets/bulk")?.body).toEqual({ ids: ["a1", "a2"], status: "en_reparacion" }));
  });

  it("solo lectura no ve los botones de alta ni de importación", async () => {
    setup({}, "readonly");
    renderApp("/inventario");
    await screen.findByRole("link", { name: "Monitor sala" });
    expect(screen.queryByRole("link", { name: "Nuevo activo" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Importar" })).toBeNull();
  });
});

describe("Alta de un activo", () => {
  it("valida y crea con los datos del formulario", async () => {
    const calls = setup({
      "POST /assets": (c: { body: unknown }) => ({ status: 201, body: asset({ id: "a9", ...(c.body as object) }) }),
      "GET /assets/a9": { body: asset({ id: "a9", name: "Monitor nuevo" }) },
      "GET /assets/a9/documents": { body: [] },
      "GET /assets/a9/licenses": { body: [] },
      "GET /assets/a9/deliveries": { body: [] },
      "GET /assets/a9/history": { body: [] },
    });
    const { location } = renderApp("/inventario/nuevo");
    const user = userEvent.setup();
    await screen.findByRole("option", { name: "Monitor" });
    await user.selectOptions(screen.getByLabelText("Categoría"), "c-mon");
    await user.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Escribe un nombre");
    await user.type(screen.getByLabelText("Nombre"), "Monitor nuevo");
    await user.type(screen.getByLabelText("Número de serie"), "CN-123");
    await user.type(screen.getByLabelText("Precio (€)"), "199,90");
    await user.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/assets")?.body).toMatchObject({
      category_id: "c-mon", name: "Monitor nuevo", serial: "CN-123", price: 199.9, status: "en_almacen", owner_id: "",
    }));
    await waitFor(() => expect(location()).toBe("/inventario/a9"));
  });
});

describe("Importar CSV", () => {
  it("muestra los errores por línea y no deja importar", async () => {
    const calls = setup({
      "POST /assets/import": { body: { rows: [{ line: 2, name: "Monitor 1", errors: [] }, { line: 3, name: "X", errors: ["La categoría «Nave» no existe."] }], valid: 1, invalid: 1, created: 0 } },
    });
    renderApp("/inventario/importar");
    const user = userEvent.setup();
    const file = new File(["categoria;nombre\nMonitor;Monitor 1\nNave;X\n"], "inventario.csv", { type: "text/csv" });
    await user.upload(await screen.findByLabelText("Fichero CSV"), file);
    expect(await screen.findByText("La categoría «Nave» no existe.")).toBeInTheDocument();
    expect(calls.find((c) => c.path === "/assets/import?dry_run=1")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Importar/ })).toBeDisabled();
  });
});

describe("Papelera", () => {
  it("restaura un activo", async () => {
    const trashed = asset({ id: "a5", name: "Monitor roto", deleted_at: "2026-11-20T10:00:00Z", deleted_reason: "Pantalla rota" });
    const calls = setup({ "GET /assets": { body: page([trashed]) }, "POST /assets/a5/restore": { body: asset({ id: "a5" }) } });
    renderApp("/inventario/papelera");
    const row = within((await screen.findByText("Monitor roto")).closest("tr")!);
    expect(row.getByText("Pantalla rota")).toBeInTheDocument();
    await userEvent.setup().click(row.getByRole("button", { name: "Restaurar" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/assets/a5/restore")).toBe(true));
    expect(calls.some((c) => c.path.includes("trash=1"))).toBe(true);
  });
});

describe("Pestaña Activo de un equipo", () => {
  it("muestra los datos de inventario y la persona", async () => {
    mockApi({
      "GET /auth/me": { body: me("operator") },
      "GET /alerts/counts": { body: {} },
      "GET /devices/d1": { body: { ...device(), inventory: null, inventory_collected_at: null } },
      "GET /devices/d1/asset": { body: agentAsset },
      "GET /groups": { body: [] },
      "GET /asset-categories": { body: categories },
      "GET /assets/a2/documents": { body: [{ id: "doc1", filename: "Factura.pdf", content_type: "application/pdf", size: 2048, uploaded_by: "ana@ejemplo.es", uploaded_at: "2026-11-20T10:00:00Z" }] },
      "GET /assets/a2/licenses": { body: [] },
      "GET /assets/a2/deliveries": { body: [] },
      "GET /assets/a2/history": { body: [] },
    });
    renderApp("/dispositivos/d1?tab=asset");
    expect(await screen.findByRole("link", { name: "Ana Pérez" })).toHaveAttribute("href", "/usuarios/u1");
    expect(await screen.findByRole("link", { name: "Factura.pdf" })).toHaveAttribute("href", "/api/v1/assets/a2/documents/doc1");
    expect(screen.getByText("Portátil")).toBeInTheDocument();
  });
});

describe("Inventario · departamento y lista sin páginas", () => {
  it("muestra el departamento y filtra por él", async () => {
    const calls = setup({
      "GET /assets": { body: page([{ ...agentAsset, department: "Finanzas" }, asset()]) },
      "GET /assets/departments": { body: ["Finanzas", "Taller"] },
    });
    renderApp("/inventario");
    const row = within((await screen.findByRole("link", { name: "PC-CONTA-07" })).closest("tr")!);
    expect(row.getByText("Finanzas")).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /^Departamentos:/ }));
    await user.click(await screen.findByRole("checkbox", { name: "Taller" }));
    await user.click(screen.getByRole("checkbox", { name: "Finanzas" }));
    await user.click(screen.getByRole("checkbox", { name: "(Sin departamento)" }));
    await waitFor(() => expect(calls.some((c) => c.path.startsWith("/assets?") && c.path.includes("department=Taller&department=Finanzas")
      && c.path.includes("no_department=1"))).toBe(true));
    expect(screen.getByRole("button", { name: /^Departamentos: Finanzas, Taller, \(Sin departamento\)/ })).toBeInTheDocument();
  });

  it("pide bloques grandes y «Cargar más» trae el siguiente sin cambiar de página", async () => {
    const calls = setup({
      "GET /assets": (c: { path: string }) => ({
        body: /[?&]page=2(&|$)/.test(c.path)
          ? page([asset({ id: "a9", name: "Monitor recepción" })], { total: 3, page: 2, per_page: 2 })
          : page([agentAsset, asset()], { total: 3, page: 1, per_page: 2 }),
      }),
      "GET /assets/departments": { body: [] },
    });
    renderApp("/inventario");
    expect(await screen.findByText("Mostrando 2 de 3")).toBeInTheDocument();
    expect(calls.find((c) => c.path.startsWith("/assets?"))!.path).toContain("per_page=200");
    await userEvent.setup().click(screen.getByRole("button", { name: "Cargar más" }));
    expect(await screen.findByRole("link", { name: "Monitor recepción" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "PC-CONTA-07" })).toBeInTheDocument();
    expect(screen.getByText("3 activos")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cargar más" })).toBeNull();
  });
});

describe("Inventario · grupos, compra y cambios en bloque", () => {
  const withGroup = { ...agentAsset, price: 899.5, purchase_order: "PO-114", groups: [{ id: "g1", name: "Oficina Bilbao", dynamic: false }, { id: "g2", name: "Portátiles", dynamic: true }] };

  it("muestra grupo, precio y pedido, y se pueden ocultar columnas", async () => {
    setup({ "GET /assets": { body: page([withGroup]) }, "GET /assets/departments": { body: [] } });
    renderApp("/inventario");
    const row = within((await screen.findByRole("link", { name: "PC-CONTA-07" })).closest("tr")!);
    expect(row.getByRole("link", { name: "Oficina Bilbao" })).toHaveAttribute("href", "/inventario?group_id=g1");
    expect(row.getByRole("link", { name: "Portátiles" })).toBeInTheDocument();
    expect(row.getByText("PO-114")).toBeInTheDocument();
    expect(row.getByText(/899,50/)).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /^Columnas/ }));
    await user.click(screen.getByRole("checkbox", { name: "Pedido" }));
    expect(screen.queryByText("PO-114")).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Pedido" })).toBeNull();
  });

  it("cambia el grupo, el pedido y el precio de varios a la vez", async () => {
    const calls = setup({
      "GET /assets": { body: page([withGroup, { ...agentAsset, id: "a3", device_id: "d3", name: "PC-CONTA-08" }]) },
      "GET /assets/departments": { body: [] },
      "GET /groups": { body: [{ id: "g1", name: "Oficina Bilbao", kind: "static", description: "", device_count: 1 },
        { id: "g2", name: "Portátiles", kind: "dynamic", description: "", device_count: 1 }] },
      "POST /assets/bulk": { body: { updated: 2 } },
    });
    renderApp("/inventario");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Seleccionar PC-CONTA-07" }));
    await user.click(screen.getByRole("checkbox", { name: "Seleccionar PC-CONTA-08" }));
    await user.click(screen.getByRole("button", { name: "Cambiar en bloque…" }));
    const dialog = within(screen.getByRole("dialog"));
    const groupSel = dialog.getByLabelText(/^Grupo/);
    expect(within(groupSel).queryByRole("option", { name: "Portátiles" })).toBeNull();
    await user.selectOptions(groupSel, "g1");
    await user.type(dialog.getByLabelText("Pedido de compra"), "PO-200");
    await user.type(dialog.getByLabelText("Precio (€)"), "1.050,5".replace(".", ""));
    await user.click(dialog.getByRole("button", { name: "Aplicar" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    expect(calls.find((c) => c.method === "POST")!.body).toEqual({ ids: ["a2", "a3"], group_id: "g1", purchase_order: "PO-200", price: 1050.5 });
  });
});
