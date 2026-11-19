import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { apiError, mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me } from "../test/fixtures";

const groups = [
  { id: "g1", name: "Contabilidad", description: "Planta 2", device_count: 14 },
  { id: "g2", name: "Ventas", description: "", device_count: 0 },
];

describe("Grupos", () => {
  it("lista grupos con su número de dispositivos, enlazado al listado filtrado", async () => {
    mockApi({ "GET /auth/me": { body: me("readonly") }, "GET /groups": { body: groups } });
    renderApp("/grupos");
    const row = (await screen.findByText("Contabilidad")).closest("tr")!;
    expect(within(row).getByRole("link", { name: "14 dispositivos" })).toHaveAttribute("href", "/dispositivos?group_id=g1");
    expect(within(row).getByText("Planta 2")).toBeInTheDocument();
    // Solo lectura: sin controles de edición.
    expect(screen.queryByRole("button", { name: "Crear grupo" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Eliminar/ })).toBeNull();
  });

  it("crea un grupo y lo añade a la lista", async () => {
    let list = [...groups];
    const calls = mockApi({
      "GET /auth/me": { body: me("operator") },
      "GET /groups": () => ({ body: list }),
      "POST /groups": (c) => { const g = { id: "g3", ...(c.body as object), device_count: 0 } as typeof groups[0]; list = [...list, g]; return { status: 201, body: g }; },
    });
    renderApp("/grupos");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Dirección");
    await user.click(screen.getByRole("button", { name: "Crear grupo" }));
    expect(await screen.findByText("Dirección")).toBeInTheDocument();
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "Dirección", description: "" });
    expect(screen.getByLabelText("Nombre")).toHaveValue("");
  });

  it("crear un grupo con nombre repetido muestra «Ya existe un grupo con ese nombre.»", async () => {
    mockApi({
      "GET /auth/me": { body: me("operator") }, "GET /groups": { body: groups },
      "POST /groups": apiError(409, "conflict", "Ya existe un grupo con ese nombre."),
    });
    renderApp("/grupos");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "ventas");
    await user.click(screen.getByRole("button", { name: "Crear grupo" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Ya existe un grupo con ese nombre.");
  });

  it("edita el nombre y la descripción de un grupo", async () => {
    const calls = mockApi({
      "GET /auth/me": { body: me("operator") }, "GET /groups": { body: groups },
      "PATCH /groups/g2": (c) => ({ body: { id: "g2", ...(c.body as object), device_count: 0 } }),
    });
    renderApp("/grupos");
    const user = userEvent.setup();
    const row = (await screen.findByText("Ventas")).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Editar" }));
    // La fila se sustituye por el formulario de edición.
    const name = screen.getByLabelText("Nombre del grupo");
    expect(name).toHaveFocus();
    await user.clear(name);
    await user.type(name, "Comercial");
    await user.type(screen.getByLabelText("Descripción del grupo"), "Portátiles");
    await user.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ name: "Comercial", description: "Portátiles" }));
  });

  it("eliminar avisa de que los dispositivos quedarán sin grupo y no se borran", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me("operator") }, "GET /groups": { body: groups }, "DELETE /groups/g1": { status: 204 } });
    renderApp("/grupos");
    const user = userEvent.setup();
    const row = (await screen.findByText("Contabilidad")).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Eliminar" }));
    const dialog = screen.getByRole("dialog", { name: "Eliminar el grupo «Contabilidad»" });
    expect(dialog).toHaveTextContent("Sus 14 dispositivos quedarán sin grupo; no se borra ninguno.");
    await user.click(within(dialog).getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
  });

  it("sin grupos invita a crear el primero", async () => {
    mockApi({ "GET /auth/me": { body: me("operator") }, "GET /groups": { body: [] } });
    renderApp("/grupos");
    expect(await screen.findByText(/Aún no hay grupos/)).toBeInTheDocument();
  });
});

describe("Grupos dinámicos", () => {
  it("crear un grupo dinámico envía el tipo y la regla", async () => {
    const calls = mockApi({
      "GET /auth/me": { body: me("operator") },
      "GET /groups": { body: [{ id: "g1", name: "Ventas", description: "", kind: "static", device_count: 2 }] },
      "POST /groups/preview": { body: { count: 3, sample: [] } },
      "POST /groups": { status: 201, body: { id: "g9", name: "Portátiles Windows", kind: "dynamic", device_count: 3 } },
    });
    renderApp("/grupos");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Portátiles Windows");
    await user.selectOptions(screen.getByLabelText("Tipo"), "dynamic");
    expect(await screen.findByText(/3 equipos cumplen la regla/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Crear grupo" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/groups")?.body).toEqual({
      name: "Portátiles Windows", description: "", kind: "dynamic",
      rule: { match: "all", conditions: [{ field: "platform", op: "eq", value: "windows" }] },
    }));
  });

  it("la lista distingue los grupos dinámicos", async () => {
    mockApi({
      "GET /auth/me": { body: me() },
      "GET /groups": { body: [{ id: "g2", name: "Linux", description: "", kind: "dynamic", rule: { match: "all", conditions: [] }, device_count: 5 }] },
    });
    renderApp("/grupos");
    expect(within((await screen.findByText("Linux")).closest("tr")!).getByText("Dinámico")).toBeInTheDocument();
  });
});
