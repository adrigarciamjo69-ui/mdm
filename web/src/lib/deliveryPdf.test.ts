import { describe, expect, it } from "vitest";
import { buildDeliveryPdf, fillClause, pdfFileName, surnameFirst, type PdfRecord } from "./deliveryPdf";
import type { DeliverySettings } from "../api/types";

const settings: DeliverySettings = {
  company: "Electrans S.A.", nif: "A12345678", address1: "C/ Mayor 1", address2: "08001 Barcelona", phone: "930000000",
  web: "www.electrans.es", email: "tic@electrans.es", city: "Barcelona", logo: "",
  colors: { entrega: { primary: "#1c3ca8", footer: "#0f172a" }, devolucion: { primary: "#b45309", footer: "#0f172a" } },
  clauses: { entrega: ["El material es propiedad de {empresa}.", "Uso en {ciudad}."], devolucion: ["{trabajador} devuelve el material."] },
};
const record: PdfRecord = {
  number: "ENT-2026-0001", type: "entrega", date: "2026-11-24", responsible: "ana@electrans.es", recipient_dni: "12345678Z",
  user: { name: "Luis Gil Martín" },
  items: [{ asset_id: "a1", category: "Portátil", brand: "Lenovo", model: "T14", serial: "PF3K9X", observations: "Con cargador" }],
};

describe("PDF de las actas", () => {
  it("nombra el fichero con el número y el tipo", () => {
    expect(pdfFileName(record)).toBe("ENT-2026-0001_entrega.pdf");
    expect(pdfFileName({ ...record, number: "", type: "devolucion" })).toBe("BORRADOR_devolucion.pdf");
  });

  it("sustituye los marcadores de las cláusulas", () => {
    expect(fillClause("De {empresa} a {trabajador} en {ciudad}.", settings, "Gil Martín, Luis")).toBe("De Electrans S.A. a Gil Martín, Luis en Barcelona.");
    expect(surnameFirst("Luis Gil Martín")).toBe("Gil Martín, Luis");
    expect(surnameFirst("Luis")).toBe("Luis");
  });

  it("incluye la empresa, el trabajador, el material y las firmas", () => {
    const doc = buildDeliveryPdf(record, settings);
    const text = doc.output();
    for (const s of ["JUSTIFICANTE DE ENTREGA DE MATERIAL", "ENT-2026-0001", "Gil Martín, Luis", "Electrans S.A.", "PF3K9X", "Lenovo T14",
      "Con cargador", "El material es propiedad de Electrans S.A.", "DNI: 12345678Z", "A12345678", "Recibe el material EL TRABAJADOR"]) {
      expect(text).toContain(s);
    }
  });

  it("la devolución usa sus propios textos y un borrador lleva marca de agua", () => {
    const text = buildDeliveryPdf({ ...record, number: "", type: "devolucion" }, settings).output();
    expect(text).toContain("JUSTIFICANTE DE DEVOLUCI");
    expect(text).toContain("Gil Martín, Luis devuelve el material.");
    expect(text).toContain("BORRADOR");
  });
});
