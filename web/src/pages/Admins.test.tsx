import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { apiError, mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me } from "../test/fixtures";

const admins = [
  { id: "a1", email: "ana@ejemplo.es", role: "admin", disabled: false, has_totp: true, created_at: "2026-10-01T08:00:00Z", last_login_at: "2026-11-13T08:00:00Z" },
  { id: "a2", email: "luis@ejemplo.es", role: "operator", disabled: false, has_totp: false, created_at: "2026-11-01T08:00:00Z", last_login_at: null },
  { id: "a3", email: "marta@ejemplo.es", role: "readonly", disabled: true, has_totp: true, created_at: "2026-11-02T08:00:00Z", last_login_at: null },
];

function setup(extra: Record<string, unknown> = {}) {
  return mockApi({
    "GET /auth/me": { body: me("admin") },
    "GET /admins": { body: admins },
    "POST /admins": (c) => ({ status: 201, body: { id: "a9", ...(c.body as object), disabled: false, has_totp: false, created_at: "", last_login_at: null } }),
    "PATCH /admins/a2": (c) => ({ body: { ...admins[1], ...(c.body as object) } }),
    ...(extra as object),
  });
}

describe("Administradores", () => {
  it("lista cuentas con rol, estado y segundo factor", async () => {
    setup();
    renderApp("/administradores");
    const luis = within((await screen.findByRole("cell", { name: "luis@ejemplo.es" })).closest("tr")!);
    expect(luis.getByText("Pendiente de configurar")).toBeInTheDocument();
    expect(luis.getByText("nunca")).toBeInTheDocument();
    expect(within(screen.getByRole("cell", { name: "marta@ejemplo.es" }).closest("tr")!).getByText("Desactivada")).toBeInTheDocument();
  });

  it("crear con contraseña de 11 caracteres muestra el error sin enviar", async () => {
    const calls = setup();
    renderApp("/administradores");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Correo"), "nueva@ejemplo.es");
    await user.type(screen.getByLabelText("Contraseña inicial"), "12345678901");
    await user.click(screen.getByRole("button", { name: "Crear cuenta" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("La contraseña debe tener al menos 12 caracteres.");
    expect(calls.some((c) => c.method === "POST")).toBe(false);
  });

  it("crea una cuenta con el rol elegido", async () => {
    const calls = setup();
    renderApp("/administradores");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Correo"), "nueva@ejemplo.es");
    await user.type(screen.getByLabelText("Contraseña inicial"), "correcta-y-larga-1");
    await user.selectOptions(screen.getByLabelText("Rol"), "operator");
    await user.click(screen.getByRole("button", { name: "Crear cuenta" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ email: "nueva@ejemplo.es", password: "correcta-y-larga-1", role: "operator" }));
    expect(await screen.findByText(/Cuenta creada/)).toBeInTheDocument();
  });

  it("no ofrece desactivar ni cambiar el rol de uno mismo", async () => {
    setup();
    renderApp("/administradores");
    const self = within((await screen.findByRole("cell", { name: "ana@ejemplo.es" })).closest("tr")!);
    expect(self.queryByRole("combobox")).toBeNull();
    expect(self.queryByRole("button", { name: /Desactivar/ })).toBeNull();
    expect(self.getByText("Tu cuenta")).toBeInTheDocument();
  });

  it("cambia el rol de otra cuenta", async () => {
    const calls = setup();
    renderApp("/administradores");
    const row = within((await screen.findByRole("cell", { name: "luis@ejemplo.es" })).closest("tr")!);
    await userEvent.setup().selectOptions(row.getByRole("combobox", { name: "Rol de luis@ejemplo.es" }), "admin");
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ role: "admin" }));
  });

  it("un 409 al desactivar muestra «Debe quedar al menos un administrador activo.»", async () => {
    setup({ "PATCH /admins/a2": apiError(409, "conflict", "Debe quedar al menos un administrador activo.") });
    renderApp("/administradores");
    const user = userEvent.setup();
    const row = within((await screen.findByRole("cell", { name: "luis@ejemplo.es" })).closest("tr")!);
    await user.click(row.getByRole("button", { name: "Desactivar" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Desactivar" }));
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent("Debe quedar al menos un administrador activo.");
  });

  it("reactiva una cuenta desactivada sin pedir confirmación", async () => {
    const calls = setup({ "PATCH /admins/a3": { body: { ...admins[2], disabled: false } } });
    renderApp("/administradores");
    const row = within((await screen.findByRole("cell", { name: "marta@ejemplo.es" })).closest("tr")!);
    await userEvent.setup().click(row.getByRole("button", { name: "Reactivar" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ disabled: false }));
  });
  it("restablece la contraseña de otra cuenta", async () => {
    const calls = setup({ "POST /admins/a2/password": { status: 204 } });
    renderApp("/administradores");
    const user = userEvent.setup();
    const row = within((await screen.findByRole("cell", { name: "luis@ejemplo.es" })).closest("tr")!);
    await user.click(row.getByRole("button", { name: "Restablecer la contraseña de luis@ejemplo.es" }));
    const dialog = within(screen.getByRole("dialog"));
    expect(dialog.queryByLabelText("Contraseña actual")).toBeNull();
    await user.type(dialog.getByLabelText("Contraseña nueva"), "nueva-clave-larga-3");
    await user.type(dialog.getByLabelText("Repite la contraseña nueva"), "otra-distinta-xx-4");
    await user.click(dialog.getByRole("button", { name: "Cambiar contraseña" }));
    expect(await dialog.findByRole("alert")).toHaveTextContent("Las contraseñas no coinciden.");
    await user.clear(dialog.getByLabelText("Repite la contraseña nueva"));
    await user.type(dialog.getByLabelText("Repite la contraseña nueva"), "nueva-clave-larga-3");
    await user.click(dialog.getByRole("button", { name: "Cambiar contraseña" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/admins/a2/password")?.body).toEqual({ password: "nueva-clave-larga-3" }));
    expect(await screen.findByText(/Contraseña de luis@ejemplo.es cambiada/)).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("no ofrece restablecer la contraseña propia desde la lista", async () => {
    setup();
    renderApp("/administradores");
    const self = within((await screen.findByRole("cell", { name: "ana@ejemplo.es" })).closest("tr")!);
    expect(self.queryByRole("button", { name: /Restablecer/ })).toBeNull();
  });

  it("cambia la contraseña propia pidiendo la actual", async () => {
    const calls = setup({ "POST /auth/password": apiError(400, "invalid_request", "La contraseña actual no es correcta.") });
    renderApp("/administradores");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Cambiar mi contraseña" }));
    const dialog = within(screen.getByRole("dialog"));
    await user.type(dialog.getByLabelText("Contraseña actual"), "mala-mala-mala-1");
    await user.type(dialog.getByLabelText("Contraseña nueva"), "nueva-clave-larga-3");
    await user.type(dialog.getByLabelText("Repite la contraseña nueva"), "nueva-clave-larga-3");
    await user.click(dialog.getByRole("button", { name: "Cambiar contraseña" }));
    expect(await dialog.findByRole("alert")).toHaveTextContent("La contraseña actual no es correcta.");
    expect(calls.find((c) => c.path === "/auth/password")?.body).toEqual({ current_password: "mala-mala-mala-1", new_password: "nueva-clave-larga-3" });
  });
});
