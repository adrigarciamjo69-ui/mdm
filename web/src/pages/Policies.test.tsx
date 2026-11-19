import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me } from "../test/fixtures";
import { linuxCatalog, windowsCatalog } from "../test/catalog";

const policies = [
  { id: "p1", name: "Seguridad Windows", description: "", platform: "windows", priority: 1, settings: [{ key: "windows.firewall", mode: "enforce", params: {} }],
    assignments: [{ group_id: null, group_name: "Todos los equipos", exclude: false }], created_at: "", updated_at: "" },
  { id: "p2", name: "Servidores Linux", description: "", platform: "linux", priority: 2, settings: [], assignments: [], created_at: "", updated_at: "" },
];

describe("Políticas", () => {
  it("la lista muestra prioridad y un operador no ve «Nueva política»", async () => {
    mockApi({ "GET /auth/me": { body: me("operator") }, "GET /policies": { body: policies } });
    renderApp("/politicas");
    const row = within((await screen.findByText("Seguridad Windows")).closest("tr")!);
    expect(row.getAllByRole("cell")[0]).toHaveTextContent("1");
    expect(row.getByText("Windows")).toBeInTheDocument();
    expect(row.getByText("Todos los equipos")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Nueva política" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Subir/ })).toBeNull();
  });

  it("reordenar envía la nueva lista de ids", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me() }, "GET /policies": { body: policies }, "POST /policies/reorder": { status: 204 } });
    renderApp("/politicas");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Subir Servidores Linux" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/policies/reorder")?.body).toEqual({ ids: ["p2", "p1"] }));
  });
});

function editorApi(extra: Record<string, unknown> = {}) {
  return mockApi({
    "GET /auth/me": { body: me() },
    "GET /groups": { body: [{ id: "g1", name: "Portátiles", kind: "static", description: "", device_count: 3 }, { id: "g2", name: "Servidores", kind: "dynamic", description: "", device_count: 2 }] },
    "GET /policy-catalog": (c) => ({ body: c.path.includes("platform=linux") ? linuxCatalog : windowsCatalog }),
    "POST /policies": (c) => ({ status: 201, body: { ...(c.body as object), id: "nueva", priority: 3, assignments: [] } }),
    "PUT /policies/nueva/assignments": (c) => ({ body: { id: "nueva", assignments: (c.body as { assignments: unknown[] }).assignments } }),
    "GET /policies/nueva": { body: { ...policies[0], id: "nueva" } },
    "GET /policies/nueva/devices": { body: [] },
    ...(extra as object),
  });
}

describe("Editor de políticas", () => {
  it("ofrece solo los ajustes de la plataforma elegida", async () => {
    editorApi();
    renderApp("/politicas/nueva");
    const user = userEvent.setup();
    const add = await screen.findByLabelText("Añadir ajuste");
    await screen.findByRole("option", { name: "BitLocker obligatorio" });
    await user.selectOptions(screen.getByLabelText("Plataforma"), "linux");
    await screen.findByRole("option", { name: "Paquetes prohibidos" });
    expect(within(add).queryByRole("option", { name: "BitLocker obligatorio" })).toBeNull();
  });

  it("un ajuste solo auditable no permite elegir «Aplicar» y lo explica", async () => {
    editorApi();
    renderApp("/politicas/nueva");
    const user = userEvent.setup();
    await user.selectOptions(await screen.findByLabelText("Plataforma"), "linux");
    await screen.findByRole("option", { name: "Cifrado de disco" });
    await user.selectOptions(screen.getByLabelText("Añadir ajuste"), "linux.disk_encryption");
    const section = screen.getByRole("group", { name: "Cifrado de disco" });
    expect(within(section).getByRole("option", { name: "Aplicar" })).toBeDisabled();
    expect(within(section).getByText(/solo se puede auditar/)).toBeInTheDocument();
  });

  it("valida los rangos antes de guardar", async () => {
    const calls = editorApi();
    renderApp("/politicas/nueva");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Bloqueo");
    await screen.findByRole("option", { name: "Bloqueo de pantalla por inactividad" });
    await user.selectOptions(screen.getByLabelText("Añadir ajuste"), "windows.screen_lock");
    await user.type(screen.getByLabelText("Minutos"), "0");
    await user.click(screen.getByRole("button", { name: "Guardar política" }));
    expect(await screen.findByText("Debe estar entre 1 y 60.")).toBeInTheDocument();
    expect(calls.some((c) => c.method === "POST" && c.path === "/policies")).toBe(false);
  });

  it("guardar envía ajustes y asignaciones con inclusiones y exclusiones", async () => {
    const calls = editorApi();
    const { location } = renderApp("/politicas/nueva");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Portátiles seguros");
    await screen.findByRole("option", { name: "BitLocker obligatorio" });
    await user.selectOptions(screen.getByLabelText("Añadir ajuste"), "windows.bitlocker");
    await user.selectOptions(screen.getByLabelText("Añadir ajuste"), "windows.screen_lock");
    await user.type(screen.getByLabelText("Minutos"), "10");
    await screen.findAllByRole("option", { name: "Portátiles" });
    await user.selectOptions(screen.getByLabelText("Incluir grupo"), "g1");
    await user.selectOptions(screen.getByLabelText("Excluir grupo"), "g2");
    await user.click(screen.getByRole("button", { name: "Guardar política" }));
    await waitFor(() => expect(location()).toBe("/politicas/nueva"));
    const post = calls.find((c) => c.method === "POST" && c.path === "/policies")!.body as { name: string; platform: string; settings: unknown[] };
    expect(post).toMatchObject({ name: "Portátiles seguros", platform: "windows" });
    expect(post.settings).toEqual([
      { key: "windows.bitlocker", mode: "enforce", params: { escrow_key: true } },
      { key: "windows.screen_lock", mode: "enforce", params: { minutes: 10 } },
    ]);
    expect(calls.find((c) => c.method === "PUT")!.body).toEqual({ assignments: [{ group_id: "g1", exclude: false }, { group_id: "g2", exclude: true }] });
  });
});
