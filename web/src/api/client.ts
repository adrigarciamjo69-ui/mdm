// Cliente de la API de consola. Añade el prefijo, el token CSRF y traduce los
// errores del servidor a ApiError con un mensaje listo para mostrarse.

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

const PREFIX = "/api/v1";

let csrfToken: string | null = null;
let unauthorizedHandler: (() => void) | null = null;

export function setCsrfToken(token: string | null) {
  csrfToken = token;
}

/** Registra lo que hay que hacer cuando la sesión deja de ser válida. */
/** Token CSRF de la sesión, para peticiones que no pasan por api() (subidas). */
export function getCsrfToken(): string | null {
  return csrfToken;
}

export function onUnauthorized(cb: (() => void) | null) {
  unauthorizedHandler = cb;
}

export function resetApiForTests() {
  csrfToken = null;
  unauthorizedHandler = null;
}

export async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && method !== "HEAD" && csrfToken) headers["X-CSRF-Token"] = csrfToken;

  let res: Response;
  try {
    res = await fetch(PREFIX + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "same-origin",
    });
  } catch {
    throw new ApiError(0, "network", "No hay conexión con el servidor. Comprueba la red e inténtalo de nuevo.");
  }

  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = undefined;
  }

  if (!res.ok) {
    const e = (data as { error?: { code?: string; message?: string } } | undefined)?.error;
    if (res.status === 401 && !path.startsWith("/auth/")) unauthorizedHandler?.();
    if (!e?.message) {
      throw new ApiError(res.status, res.status >= 500 ? "unavailable" : "unknown",
        "El servidor no está respondiendo como debería. Inténtalo de nuevo en unos minutos.");
    }
    throw new ApiError(res.status, e.code ?? "unknown", e.message);
  }
  return data as T;
}

/** Construye una consulta omitiendo los valores vacíos. */
export function query(params: Record<string, string | number | undefined | null>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== "") q.set(k, String(v));
  }
  const s = q.toString();
  return s ? "?" + s : "";
}
