import { describe, expect, it } from "vitest";
import { formatBytes, formatDateTime, relativeTime, statusLabel } from "./format";

describe("formatBytes", () => {
  it("usa unidades binarias y coma decimal", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1536)).toBe("1,5 KB");
    expect(formatBytes(1610612736)).toBe("1,5 GB");
    expect(formatBytes(16 * 2 ** 30)).toBe("16 GB");
    expect(formatBytes(2 * 2 ** 40)).toBe("2 TB");
  });
});

describe("relativeTime", () => {
  const now = new Date("2026-11-13T12:00:00Z");
  const ago = (s: number) => new Date(now.getTime() - s * 1000).toISOString();
  it("elige la unidad natural", () => {
    expect(relativeTime(ago(40), now)).toBe("hace 40 s");
    expect(relativeTime(ago(180), now)).toBe("hace 3 min");
    expect(relativeTime(ago(2 * 3600), now)).toBe("hace 2 h");
    expect(relativeTime(ago(5 * 86400), now)).toBe("hace 5 días");
    expect(relativeTime(ago(86400), now)).toBe("hace 1 día");
    expect(relativeTime(null, now)).toBe("nunca");
  });
  it("trata fechas futuras (reloj desajustado) como ahora", () => {
    expect(relativeTime(ago(-30), now)).toBe("hace 0 s");
  });
});

describe("formatDateTime", () => {
  it("da fecha y hora en formato español", () => {
    expect(formatDateTime("2026-11-13T09:05:00Z")).toMatch(/13\/11\/2026.*\d{2}:05/);
  });
});

describe("statusLabel", () => {
  it("traduce cada estado", () => {
    expect(statusLabel("online")).toBe("En línea");
    expect(statusLabel("offline")).toBe("Desconectado");
    expect(statusLabel("inactive")).toBe("Inactivo");
    expect(statusLabel("revoked")).toBe("Revocado");
  });
});
