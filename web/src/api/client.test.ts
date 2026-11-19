import { describe, expect, it, vi } from "vitest";
import { api, ApiError, onUnauthorized, setCsrfToken } from "./client";
import { apiError, mockApi } from "../test/mockApi";

describe("api", () => {
  it("antepone /api/v1 y envía JSON", async () => {
    const calls = mockApi({ "POST /groups": { status: 201, body: { id: "g1" } } });
    const out = await api<{ id: string }>("POST", "/groups", { name: "Ventas" });
    expect(out.id).toBe("g1");
    expect(calls[0].path).toBe("/groups");
    expect(calls[0].body).toEqual({ name: "Ventas" });
    expect(calls[0].headers["content-type"]).toBe("application/json");
  });

  it("envía X-CSRF-Token en POST y no en GET", async () => {
    const calls = mockApi({ "GET /groups": { body: [] }, "POST /groups": { status: 201, body: {} } });
    setCsrfToken("tok");
    await api("GET", "/groups");
    await api("POST", "/groups", {});
    expect(calls[0].headers["x-csrf-token"]).toBeUndefined();
    expect(calls[1].headers["x-csrf-token"]).toBe("tok");
  });

  it("lanza ApiError con status, code y message del cuerpo de error", async () => {
    mockApi({ "POST /groups": apiError(409, "conflict", "Ya existe un grupo con ese nombre.") });
    const err = await api("POST", "/groups", {}).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 409, code: "conflict", message: "Ya existe un grupo con ese nombre." });
  });

  it("da un mensaje útil si el servidor no responde JSON", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("<html>502</html>", { status: 502 })));
    const err = (await api("GET", "/groups").catch((e: unknown) => e)) as ApiError;
    expect(err).toMatchObject({ status: 502, code: "unavailable" });
    expect(err.message).toMatch(/servidor/);
  });

  it("da un mensaje útil si no hay red", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new TypeError("Failed to fetch"); }));
    const err = (await api("GET", "/groups").catch((e: unknown) => e)) as ApiError;
    expect(err).toMatchObject({ status: 0, code: "network" });
    expect(err.message).toMatch(/conexión/);
  });

  it("llama a onUnauthorized ante un 401 fuera de /auth/login", async () => {
    mockApi({ "GET /devices": apiError(401, "unauthorized", "Inicia sesión."), "POST /auth/login": apiError(401, "unauthorized", "No.") });
    const cb = vi.fn();
    onUnauthorized(cb);
    await api("POST", "/auth/login", {}).catch(() => {});
    expect(cb).not.toHaveBeenCalled();
    await api("GET", "/devices").catch(() => {});
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it("devuelve undefined con un 204", async () => {
    mockApi({ "DELETE /groups/g1": { status: 204 } });
    expect(await api("DELETE", "/groups/g1")).toBeUndefined();
  });
});
