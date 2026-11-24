import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { apiError, mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { asset, dirUser, license, me, page } from "../test/fixtures";

const base = (role: "admin" | "operator" | "readonly" = "operator") => ({
  "GET /auth/me": { body: me(role) },
  "GET /alerts/counts": { body: {} },
});
const status = { configured: true, last_sync_at: "2026-11-24T08:00:00Z", last_success_at: "2026-11-24T08:00:00Z", last_error: "", last_count: 120, secret_expires: "2027-06-30" };

describe("Usuarios", () => {
  it("lista personas con su estado y sus activos", async () => {
    mockApi({ ...base(), "GET /directory/status": { body: status }, "GET /directory/departments": { body: ["TIC"] },
      "GET /directory/users": { body: page([dirUser(), dirUser({ id: "u2", display_name: "Luis Gil", status: "deleted", deleted_at: "2026-11-20T00:00:00Z", asset_count: 2 })]) } });
    renderApp("/usuarios");
    const luis = within((await screen.findByRole("link", { name: "Luis Gil" })).closest("tr")!);
    expect(luis.getByText("Eliminada de Entra")).toBeInTheDocument();
    expect(luis.getByRole("link", { name: "2" })).toHaveAttribute("href", "/inventario?owner_id=u2");
    expect(screen.getByText(/120 personas/)).toBeInTheDocument();
  });

  it("el filtro «Bajas con material» pide status=leavers", async () => {
    const calls = mockApi({ ...base(), "GET /directory/status": { body: status }, "GET /directory/departments": { body: [] },
      "GET /directory/users": { body: page([dirUser()]) } });
    renderApp("/usuarios");
    await screen.findByRole("link", { name: "Ana Pérez" });
    await userEvent.setup().click(screen.getByRole("button", { name: "Bajas con material" }));
    await waitFor(() => expect(calls.some((c) => c.path.includes("status=leavers"))).toBe(true));
  });

  it("sincronizar ahora y el aviso si Entra no está configurado", async () => {
    const calls = mockApi({ ...base(), "GET /directory/status": { body: { ...status, configured: false, last_sync_at: null } },
      "GET /directory/departments": { body: [] }, "GET /directory/users": { body: page([]) },
      "POST /directory/sync": apiError(409, "conflict", "Entra ID no está configurado: un administrador debe hacerlo en Ajustes.") });
    renderApp("/usuarios");
    expect(await screen.findByText(/Entra ID aún no está conectado/)).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Sincronizar ahora" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("no está configurado");
    expect(calls.some((c) => c.method === "POST" && c.path === "/directory/sync")).toBe(true);
  });

  it("la ficha muestra activos, licencias y actas, y abre un acta con la persona", async () => {
    mockApi({ ...base(), "GET /directory/users/u1": { body: { ...dirUser(), assets: [asset({ owner: { id: "u1", name: "Ana Pérez", email: "" } })],
      licenses: [license()], deliveries: [{ id: "d1", number: "ENT-2026-0001", type: "entrega", status: "confirmada", date: "2026-11-20", user: { id: "u1", name: "Ana Pérez" }, items: [] }] } } });
    const { location } = renderApp("/usuarios/u1");
    expect(await screen.findByRole("heading", { name: "Ana Pérez" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Monitor sala" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Microsoft Office 2021" })).toHaveAttribute("href", "/licencias/l1");
    expect(screen.getByRole("link", { name: "ENT-2026-0001" })).toHaveAttribute("href", "/actas/d1");
    await userEvent.setup().click(screen.getByRole("link", { name: "Nueva acta de devolución" }));
    await waitFor(() => expect(location()).toBe("/actas/nueva?tipo=devolucion&persona=u1"));
  });
});

describe("Licencias", () => {
  it("lista con asignadas e instaladas y marca las que superan los puestos", async () => {
    mockApi({ ...base(), "GET /licenses": { body: page([license(), license({ id: "l2", name: "Antivirus", over: false, effective_status: "expirada" })]) } });
    renderApp("/licencias");
    const office = within((await screen.findByRole("link", { name: "Microsoft Office 2021" })).closest("tr")!);
    expect(office.getByText("12")).toHaveClass("seat-over");
    expect(within(screen.getByRole("link", { name: "Antivirus" }).closest("tr")!).getByText("Expirada")).toBeInTheDocument();
  });

  it("crea una licencia", async () => {
    const calls = mockApi({ ...base(), "GET /licenses": { body: page([]) },
      "POST /licenses": (c: { body: unknown }) => ({ status: 201, body: license({ id: "l9", ...(c.body as object) }) }),
      "GET /licenses/l9": { body: { ...license({ id: "l9" }), assignments: [] } }, "GET /licenses/l9/installations": { body: [] } });
    const { location } = renderApp("/licencias/nueva");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Adobe Acrobat");
    await user.selectOptions(screen.getByLabelText("Tipo"), "suscripcion");
    await user.clear(screen.getByLabelText(/Puestos/));
    await user.type(screen.getByLabelText(/Puestos/), "5");
    await user.type(screen.getByLabelText("Clave de licencia"), "XXXX-YYYY");
    await user.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({ name: "Adobe Acrobat", license_type: "suscripcion", seats: 5, license_key: "XXXX-YYYY", status: "activa" }));
    await waitFor(() => expect(location()).toBe("/licencias/l9"));
  });

  it("un administrador ve la clave y asigna la licencia a un activo", async () => {
    const calls = mockApi({ ...base("admin"),
      "GET /licenses/l1": { body: { ...license(), assignments: [{ id: "as1", asset: null, user: { id: "u1", name: "Ana Pérez", email: "ana@electrans.es" }, notes: "", assigned_at: "2026-11-01T00:00:00Z" }] } },
      "GET /licenses/l1/installations": { body: [{ device_id: "d1", hostname: "PC-CONTA-07", software: "Microsoft Office Professional Plus 2021", version: "16.0" }] },
      "POST /licenses/l1/key/reveal": { body: { key: "AAAAA-BBBBB" } },
      "GET /assets": { body: page([asset()]) },
      "POST /licenses/l1/assignments": { status: 201, body: { id: "as2" } } });
    renderApp("/licencias/l1");
    const user = userEvent.setup();
    expect(await screen.findByRole("link", { name: "PC-CONTA-07" })).toHaveAttribute("href", "/dispositivos/d1");
    await user.click(screen.getByRole("button", { name: "Mostrar clave" }));
    expect(await screen.findByText("AAAAA-BBBBB")).toBeInTheDocument();
    await user.type(screen.getByLabelText("Buscar activo"), "Moni");
    await user.click(await screen.findByRole("button", { name: /Monitor sala/ }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/licenses/l1/assignments")?.body).toEqual({ asset_id: "a1" }));
  });

  it("un operador no ve el botón de mostrar la clave", async () => {
    mockApi({ ...base("operator"), "GET /licenses/l1": { body: { ...license(), assignments: [] } }, "GET /licenses/l1/installations": { body: [] } });
    renderApp("/licencias/l1");
    await screen.findByRole("heading", { name: "Microsoft Office 2021" });
    expect(screen.queryByRole("button", { name: "Mostrar clave" })).toBeNull();
  });
});

describe("Ajustes del inventario", () => {
  const settings = { company: "EMPRESA, S.A.", nif: "", address1: "", address2: "", phone: "", web: "", email: "", city: "", logo: "",
    colors: { entrega: { primary: "#1c3ca8", footer: "#0f172a" }, devolucion: { primary: "#b45309", footer: "#0f172a" } },
    clauses: { entrega: ["Cláusula 1 de {empresa}."], devolucion: ["Devuelve {trabajador}."] } };

  it("guarda la conexión con Entra ID y prueba la conexión", async () => {
    const calls = mockApi({ ...base("admin"), "GET /asset-categories": { body: [] }, "GET /delivery-settings": { body: settings },
      "GET /integrations/entra": { body: { tenant_id: "", client_id: "", client_secret: "", secret_expires: "", configured: false } },
      "POST /integrations/entra/test": { body: { users: 120 } },
      "PUT /integrations/entra": (c: { body: unknown }) => ({ body: { ...(c.body as object), client_secret: "********", configured: true } }) });
    renderApp("/ajustes/inventario");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Identificador del inquilino (tenant)"), "electrans.onmicrosoft.com");
    await user.type(screen.getByLabelText("Identificador de la aplicación (client ID)"), "1111");
    await user.type(screen.getByLabelText("Secreto de cliente"), "s3cr3t");
    await user.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByText(/120 personas/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Guardar conexión" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PUT" && c.path === "/integrations/entra")?.body).toMatchObject({
      tenant_id: "electrans.onmicrosoft.com", client_id: "1111", client_secret: "s3cr3t" }));
  });

  it("añade una categoría y guarda los datos de las actas", async () => {
    const calls = mockApi({ ...base("admin"), "GET /asset-categories": { body: [{ id: "c1", name: "Portátil", assets: 3 }] },
      "POST /asset-categories": { status: 201, body: { id: "c2", name: "Proyector", assets: 0 } },
      "GET /integrations/entra": { body: { tenant_id: "", client_id: "", client_secret: "", secret_expires: "", configured: false } },
      "GET /delivery-settings": { body: settings }, "PUT /delivery-settings": (c: { body: unknown }) => ({ body: c.body }) });
    renderApp("/ajustes/inventario");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nueva categoría"), "Proyector");
    await user.click(screen.getByRole("button", { name: "Añadir" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/asset-categories" && c.method === "POST")?.body).toEqual({ name: "Proyector" }));
    const company = await screen.findByLabelText("Empresa");
    await user.clear(company);
    await user.type(company, "Electrans S.A.");
    await user.click(screen.getByRole("button", { name: "Guardar datos de las actas" }));
    await waitFor(() => expect((calls.find((c) => c.method === "PUT" && c.path === "/delivery-settings")?.body as { company: string }).company).toBe("Electrans S.A."));
  });
});
