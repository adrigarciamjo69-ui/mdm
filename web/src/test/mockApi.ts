import { vi } from "vitest";

export interface Call {
  method: string;
  path: string; // sin el prefijo /api/v1, con la consulta
  body: unknown;
  headers: Record<string, string>;
}

type Reply = { status?: number; body?: unknown; headers?: Record<string, string> };
type Handler = Reply | ((call: Call) => Reply);

/**
 * Sustituye fetch por un servidor falso. Las claves son "MÉTODO /ruta" (sin
 * /api/v1 ni consulta). Devuelve la lista de llamadas para inspeccionarla.
 */
export function mockApi(routes: Record<string, Handler>): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), "http://localhost");
      const method = (init?.method ?? "GET").toUpperCase();
      const path = decodeURIComponent(url.pathname).replace(/^\/api\/v1/, "");
      const headers: Record<string, string> = {};
      new Headers(init?.headers).forEach((v, k) => (headers[k.toLowerCase()] = v));
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      const call = { method, path: path + url.search, body, headers };
      calls.push(call);
      const handler = routes[`${method} ${path}`];
      if (!handler) {
        return new Response(JSON.stringify({ error: { code: "not_found", message: `Sin simular: ${method} ${path}` } }), { status: 404 });
      }
      const reply = typeof handler === "function" ? handler(call) : handler;
      const status = reply.status ?? 200;
      const payload = status === 204 ? null : JSON.stringify(reply.body ?? {});
      return new Response(payload, { status, headers: { "Content-Type": "application/json", ...reply.headers } });
    }),
  );
  return calls;
}

export const apiError = (status: number, code: string, message: string): Reply => ({
  status,
  body: { error: { code, message } },
});
