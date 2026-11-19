import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me } from "../test/fixtures";

const apps = [
  { id: "a1", name: "Firefox", publisher: "Mozilla", description: "", platform: "windows", source: "winget", package_id: "Mozilla.Firefox",
    archived: false, current_version: null, versions: [], updated_at: "" },
  { id: "a2", name: "Contabilidad Pro", publisher: "", description: "", platform: "windows", source: "upload", package_id: "", archived: false,
    current_version: { id: "v1", version: "3.2.1" }, versions: [{ id: "v1", version: "3.2.1" }], updated_at: "" },
];

describe("Software", () => {
  it("lista el catálogo con origen y versión; un operador no ve «Añadir aplicación»", async () => {
    mockApi({ "GET /auth/me": { body: me("operator") }, "GET /apps": { body: apps } });
    renderApp("/software");
    const row = within((await screen.findByText("Contabilidad Pro")).closest("tr")!);
    expect(row.getByText("Instalador propio")).toBeInTheDocument();
    expect(row.getByText("3.2.1")).toBeInTheDocument();
    expect(within(screen.getByText("Firefox").closest("tr")!).getByText("winget · Mozilla.Firefox")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Añadir aplicación" })).toBeNull();
  });

  it("el origen se limita según la plataforma", async () => {
    mockApi({ "GET /auth/me": { body: me() } });
    renderApp("/software/nueva");
    const user = userEvent.setup();
    const source = await screen.findByLabelText("Origen");
    expect(within(source).getByRole("option", { name: /winget/ })).toBeInTheDocument();
    expect(within(source).queryByRole("option", { name: /apt/ })).toBeNull();
    await user.selectOptions(screen.getByLabelText("Plataforma"), "linux");
    expect(within(screen.getByLabelText("Origen")).getByRole("option", { name: /apt/ })).toBeInTheDocument();
    expect(within(screen.getByLabelText("Origen")).queryByRole("option", { name: /winget/ })).toBeNull();
  });

  it("crear una aplicación de winget envía el identificador", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me() }, "POST /apps": { status: 201, body: { ...apps[0], id: "nuevo" } }, "GET /apps/nuevo": { body: { ...apps[0], id: "nuevo" } } });
    const { location } = renderApp("/software/nueva");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Firefox");
    await user.type(screen.getByLabelText("Identificador de winget"), "Mozilla.Firefox");
    await user.click(screen.getByRole("button", { name: "Crear aplicación" }));
    await waitFor(() => expect(location()).toBe("/software/nuevo"));
    expect(calls.find((c) => c.method === "POST")!.body).toMatchObject({ name: "Firefox", platform: "windows", source: "winget", package_id: "Mozilla.Firefox" });
  });
});

describe("Versión de un instalador propio", () => {
  function setup() {
    return mockApi({
      "GET /auth/me": { body: me() }, "GET /apps/a2": { body: apps[1] },
      "POST /apps/a2/versions": { status: 201, body: { id: "v2", version: "3.3" } },
    });
  }

  it("un EXE exige las órdenes de instalación y desinstalación", async () => {
    const calls = setup();
    renderApp("/software/a2");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Añadir versión" }));
    await user.type(screen.getByLabelText("Versión"), "3.3");
    await user.selectOptions(screen.getByLabelText("Tipo de instalador"), "exe");
    await user.type(screen.getByLabelText("SHA-256 del fichero"), "b".repeat(64));
    await user.click(screen.getByRole("button", { name: "Guardar versión" }));
    expect(await screen.findByText(/Un instalador EXE necesita/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === "POST")).toBe(false);
  });

  it("la regla de detección cambia sus campos según el tipo", async () => {
    setup();
    renderApp("/software/a2");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Añadir versión" }));
    expect(screen.getByLabelText("El nombre del programa contiene")).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText("Detección"), "registry");
    expect(screen.getByLabelText("Clave del registro")).toBeInTheDocument();
    expect(screen.queryByLabelText("El nombre del programa contiene")).toBeNull();
    await user.selectOptions(screen.getByLabelText("Detección"), "script");
    expect(screen.getByLabelText("Script de detección")).toBeInTheDocument();
  });

  it("guardar una versión MSI envía el SHA y la detección por defecto", async () => {
    const calls = setup();
    renderApp("/software/a2");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Añadir versión" }));
    await user.type(screen.getByLabelText("Versión"), "3.3");
    await user.type(screen.getByLabelText("SHA-256 del fichero"), "c".repeat(64));
    await user.click(screen.getByRole("button", { name: "Guardar versión" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({
      version: "3.3", installer_type: "msi", file_sha256: "c".repeat(64),
      detection: { type: "inventory", name_contains: "Contabilidad Pro", min_version: "3.3" },
    }));
  });
});
