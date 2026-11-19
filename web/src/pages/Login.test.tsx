import { describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { apiError, mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";

const me = { id: "a1", email: "ana@ejemplo.es", role: "admin", csrf_token: "csrf" };

async function fillCredentials() {
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText("Correo"), "ana@ejemplo.es");
  await user.type(screen.getByLabelText("Contraseña"), "correcta-y-larga-1");
  await user.click(screen.getByRole("button", { name: "Iniciar sesión" }));
  return user;
}

describe("Login", () => {
  it("pide correo y contraseña y, con next=totp, pasa al paso del código", async () => {
    const calls = mockApi({ "GET /auth/me": apiError(401, "unauthorized", "x"), "POST /auth/login": { body: { next: "totp" } } });
    renderApp("/login");
    await fillCredentials();
    expect(await screen.findByLabelText("Código de verificación")).toBeInTheDocument();
    expect(calls.find((c) => c.path === "/auth/login")?.body).toEqual({ email: "ana@ejemplo.es", password: "correcta-y-larga-1" });
  });

  it("con next=totp_setup muestra el QR y la clave en texto para copiarla", async () => {
    mockApi({
      "GET /auth/me": apiError(401, "unauthorized", "x"),
      "POST /auth/login": { body: { next: "totp_setup" } },
      "POST /auth/totp/setup": { body: { secret: "JBSWY3DPEHPK3PXP", uri: "otpauth://totp/MDM:ana?secret=JBSWY3DPEHPK3PXP&issuer=MDM" } },
    });
    renderApp("/login");
    await fillCredentials();
    expect(await screen.findByRole("img", { name: /código QR/i })).toBeInTheDocument();
    expect(screen.getByText("JBSW Y3DP EHPK 3PXP")).toBeInTheDocument();
    expect(screen.getByLabelText("Código de verificación")).toBeInTheDocument();
  });

  it("muestra «Correo o contraseña incorrectos.» ante un 401", async () => {
    mockApi({ "GET /auth/me": apiError(401, "unauthorized", "x"), "POST /auth/login": apiError(401, "unauthorized", "Correo o contraseña incorrectos.") });
    renderApp("/login");
    await fillCredentials();
    expect(await screen.findByRole("alert")).toHaveTextContent("Correo o contraseña incorrectos.");
  });

  it("muestra el tiempo de espera ante un 429", async () => {
    mockApi({
      "GET /auth/me": apiError(401, "unauthorized", "x"),
      "POST /auth/login": apiError(429, "rate_limited", "Demasiados intentos fallidos. Vuelve a intentarlo dentro de 10 minutos."),
    });
    renderApp("/login");
    await fillCredentials();
    expect(await screen.findByRole("alert")).toHaveTextContent("dentro de 10 minutos");
  });

  it("tras verificar el código, navega a la ruta de origen", async () => {
    let loggedIn = false;
    mockApi({
      "GET /auth/me": () => (loggedIn ? { body: me } : apiError(401, "unauthorized", "x")),
      "POST /auth/login": { body: { next: "totp" } },
      "POST /auth/totp/verify": () => { loggedIn = true; return { body: me }; },
      "GET /groups": { body: [] },
    });
    renderApp("/grupos");
    const user = await fillCredentials();
    await user.type(await screen.findByLabelText("Código de verificación"), "123456");
    await user.click(screen.getByRole("button", { name: "Verificar" }));
    expect(await screen.findByRole("heading", { name: "Grupos" })).toBeInTheDocument();
  });

  it("un código erróneo deja repetirlo sin volver a la contraseña", async () => {
    mockApi({
      "GET /auth/me": apiError(401, "unauthorized", "x"),
      "POST /auth/login": { body: { next: "totp" } },
      "POST /auth/totp/verify": apiError(401, "unauthorized", "El código no es válido."),
    });
    renderApp("/login");
    const user = await fillCredentials();
    const input = await screen.findByLabelText("Código de verificación");
    await user.type(input, "000000");
    await user.click(screen.getByRole("button", { name: "Verificar" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("El código no es válido.");
    await waitFor(() => expect(screen.getByLabelText("Código de verificación")).toHaveValue(""));
  });
});
