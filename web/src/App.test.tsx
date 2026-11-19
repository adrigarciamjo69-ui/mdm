import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { apiError, mockApi } from "./test/mockApi";
import { renderApp } from "./test/render";

const meAs = (role: string) => ({ id: "a1", email: "ana@ejemplo.es", role, csrf_token: "csrf" });

describe("App", () => {
  it("sin sesión, redirige a /login conservando la ruta pedida", async () => {
    mockApi({ "GET /auth/me": apiError(401, "unauthorized", "x") });
    const { location } = renderApp("/dispositivos?platform=linux");
    expect(await screen.findByRole("button", { name: "Iniciar sesión" })).toBeInTheDocument();
    expect(location()).toMatch(/^\/login/);
    expect(decodeURIComponent(location())).toContain("/dispositivos?platform=linux");
  });

  it("un operador no ve «Administradores» ni «Auditoría» en el menú", async () => {
    mockApi({ "GET /auth/me": { body: meAs("operator") }, "GET /groups": { body: [] } });
    renderApp("/grupos");
    const nav = await screen.findByRole("navigation", { name: "Principal" });
    expect(within(nav).getByRole("link", { name: "Inscripción" })).toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Administradores" })).toBeNull();
    expect(within(nav).queryByRole("link", { name: "Auditoría" })).toBeNull();
  });

  it("un rol de solo lectura no ve «Inscripción»", async () => {
    mockApi({ "GET /auth/me": { body: meAs("readonly") }, "GET /groups": { body: [] } });
    renderApp("/grupos");
    const nav = await screen.findByRole("navigation", { name: "Principal" });
    expect(within(nav).queryByRole("link", { name: "Inscripción" })).toBeNull();
  });

  it("un rol de solo lectura que abre /auditoria ve «No tienes permiso para ver esta página.»", async () => {
    mockApi({ "GET /auth/me": { body: meAs("readonly") } });
    renderApp("/auditoria");
    expect(await screen.findByText("No tienes permiso para ver esta página.")).toBeInTheDocument();
  });

  it("muestra correo y rol y permite cerrar sesión", async () => {
    let loggedIn = true;
    const calls = mockApi({
      "GET /auth/me": () => (loggedIn ? { body: meAs("admin") } : apiError(401, "unauthorized", "x")),
      "GET /groups": { body: [] },
      "POST /auth/logout": () => { loggedIn = false; return { status: 204 }; },
    });
    const { location } = renderApp("/grupos");
    expect(await screen.findByText("ana@ejemplo.es")).toBeInTheDocument();
    expect(screen.getByText("Administrador")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Cerrar sesión" }));
    expect(await screen.findByRole("button", { name: "Iniciar sesión" })).toBeInTheDocument();
    expect(location()).toBe("/login");
    expect(calls.find((c) => c.path === "/auth/logout")?.headers["x-csrf-token"]).toBe("csrf");
  });

  it("una sesión que caduca a mitad de uso lleva al inicio de sesión", async () => {
    let expired = false;
    mockApi({
      "GET /auth/me": () => (expired ? apiError(401, "unauthorized", "x") : { body: meAs("admin") }),
      "GET /groups": () => { expired = true; return apiError(401, "unauthorized", "Inicia sesión para continuar."); },
    });
    const { location } = renderApp("/grupos");
    expect(await screen.findByRole("button", { name: "Iniciar sesión" })).toBeInTheDocument();
    expect(decodeURIComponent(location())).toContain("/grupos");
  });

  it("una ruta desconocida muestra un aviso con enlace al resumen", async () => {
    mockApi({ "GET /auth/me": { body: meAs("admin") } });
    renderApp("/no-existe");
    expect(await screen.findByText("Esta página no existe.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ir al resumen" })).toHaveAttribute("href", "/");
  });
});
