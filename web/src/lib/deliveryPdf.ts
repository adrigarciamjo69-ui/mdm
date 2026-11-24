// PDF de las actas de entrega y devolución. Es la plantilla de la app de
// inventario anterior («JUSTIFICANTE DE ENTREGA/DEVOLUCIÓN DE MATERIAL»),
// adaptada a los datos del MDM.
import { jsPDF } from "jspdf";
import type { Delivery, DeliveryItem, DeliverySettings } from "../api/types";

type RGB = [number, number, number];

function hexToRgb(hex: string): RGB {
  const h = hex.replace("#", "");
  const n = (i: number) => parseInt(h.slice(i, i + 2), 16) || 0;
  return [n(0), n(2), n(4)];
}

/** Datos mínimos para generar el PDF (también de un borrador sin guardar). */
export interface PdfRecord {
  number: string;
  type: Delivery["type"];
  date: string;
  responsible: string;
  recipient_dni: string;
  user: { name: string };
  items: DeliveryItem[];
}

/** «Pérez García, Ana» a partir de «Ana Pérez García» (como en la app anterior). */
export function surnameFirst(fullName: string): string {
  const parts = fullName.trim().split(/\s+/).filter(Boolean);
  if (parts.length < 2) return fullName.trim();
  return `${parts.slice(1).join(" ")}, ${parts[0]}`;
}

export function fillClause(text: string, s: DeliverySettings, worker: string): string {
  return text.replace(/\{empresa\}/g, s.company || "la empresa").replace(/\{trabajador\}/g, worker).replace(/\{ciudad\}/g, s.city || "");
}

export function pdfFileName(r: PdfRecord): string {
  return `${r.number || "BORRADOR"}_${r.type}.pdf`;
}

function formatDay(iso: string): string {
  if (!iso) return "XX/XX/XXXX";
  const [y, m, d] = iso.slice(0, 10).split("-");
  return `${d}/${m}/${y}`;
}

export function buildDeliveryPdf(r: PdfRecord, s: DeliverySettings): jsPDF {
  const isE = r.type !== "devolucion";
  const colors = isE ? s.colors.entrega : s.colors.devolucion;
  const pri = hexToRgb(colors.primary || "#1c3ca8");
  const foot = hexToRgb(colors.footer || "#0f172a");
  const light = pri.map((c) => Math.round(c + (255 - c) * 0.85)) as RGB;
  const zebra = light.map((c) => Math.round(c + (255 - c) * 0.7)) as RGB;
  const GR: RGB = [100, 110, 125];
  const BK: RGB = [17, 24, 39];
  const company = s.company || "EMPRESA, S.A.";
  const worker = surnameFirst(r.user.name);
  const fecha = formatDay(r.date);
  const clauses = (isE ? s.clauses.entrega : s.clauses.devolucion).filter((c) => c.trim()).map((c) => fillClause(c, s, worker));

  const doc = new jsPDF({ unit: "mm", format: "a4" });
  const W = 210, H = 297, mL = 16, mR = 16, cW = W - mL - mR;
  let y = 12;
  const logoW = 68, logoH = 18;

  // Cabecera: logotipo (o el nombre en un recuadro) y la dirección a la derecha.
  const nameBox = () => {
    doc.setFillColor(...pri);
    doc.roundedRect(mL, y, logoW, logoH, 2, 2, "F");
    doc.setTextColor(255, 255, 255);
    doc.setFont("helvetica", "bold");
    let fs = 10;
    doc.setFontSize(fs);
    while (doc.getTextWidth(company) > logoW - 6 && fs > 6.5) {
      fs -= 0.5;
      doc.setFontSize(fs);
    }
    doc.text(company, mL + logoW / 2, y + logoH / 2 + 1.5, { align: "center" });
  };
  if (s.logo) {
    try {
      doc.addImage(s.logo, s.logo.startsWith("data:image/png") ? "PNG" : "JPEG", mL, y, logoW, logoH);
    } catch {
      nameBox();
    }
  } else {
    nameBox();
  }
  doc.setTextColor(...GR);
  doc.setFont("helvetica", "normal");
  doc.setFontSize(7.2);
  if (s.address1) doc.text(s.address1, W - mR, y + 3, { align: "right" });
  if (s.address2) doc.text(s.address2, W - mR, y + 7.5, { align: "right" });
  if (s.phone) doc.text(s.phone, W - mR, y + 12, { align: "right" });
  y += 22;
  doc.setDrawColor(...pri);
  doc.setLineWidth(0.5);
  doc.line(mL, y, W - mR, y);
  y += 9;

  doc.setTextColor(...BK);
  doc.setFont("helvetica", "bold");
  doc.setFontSize(13);
  doc.text(isE ? "JUSTIFICANTE DE ENTREGA DE MATERIAL" : "JUSTIFICANTE DE DEVOLUCIÓN DE MATERIAL", W / 2, y, { align: "center" });
  y += 5;
  doc.setFont("helvetica", "normal");
  doc.setFontSize(8.5);
  doc.setTextColor(...GR);
  doc.text(r.number || "BORRADOR", W / 2, y, { align: "center" });
  y += 8;

  doc.setFontSize(9.5);
  doc.setTextColor(...BK);
  const where = s.city ? ` en ${s.city}` : "";
  const tail = isE
    ? `hace constar la entrega del siguiente material al trabajador ${worker}, para su uso en las tareas relacionadas con su puesto de trabajo${where}, a ${fecha}:`
    : `hace constar la devolución del siguiente material por parte del trabajador ${worker}${where}, a ${fecha}:`;
  const intro = doc.splitTextToSize(`La empresa ${company}, a través del siguiente justificante, ${tail}`, cW);
  doc.text(intro, mL, y);
  y += intro.length * 5 + 7;

  // Tabla de material.
  const headers = ["Dispositivo", "Descripción / Modelo", "Número serie", "Observaciones"];
  const colW = [46, 56, 42, cW - 46 - 56 - 42];
  const rowH = 8.5;
  doc.setFillColor(...pri);
  doc.rect(mL, y, cW, rowH, "F");
  doc.setTextColor(255, 255, 255);
  doc.setFont("helvetica", "bold");
  doc.setFontSize(8);
  let cx = mL;
  headers.forEach((h, i) => {
    doc.text(h, cx + 3, y + 5.8);
    cx += colW[i];
  });
  let ty = y + rowH;
  r.items.forEach((it, ri) => {
    const cells = [it.category, [it.brand, it.model].filter(Boolean).join(" "), it.serial, it.observations];
    const wrapped = cells.map((c, ci) => doc.splitTextToSize(c || "", colW[ci] - 5) as string[]);
    const h = Math.max(rowH, Math.max(...wrapped.map((w) => w.length)) * 4.8 + 4);
    if (ty + h > H - 90) {
      doc.addPage();
      ty = 20;
    }
    doc.setFillColor(...(ri % 2 === 0 ? zebra : ([255, 255, 255] as RGB)));
    doc.rect(mL, ty, cW, h, "F");
    doc.setDrawColor(196, 210, 230);
    doc.setLineWidth(0.2);
    let x2 = mL;
    colW.forEach((w) => {
      x2 += w;
      doc.line(x2, ty, x2, ty + h);
    });
    doc.line(mL, ty + h, mL + cW, ty + h);
    doc.setTextColor(...BK);
    doc.setFont("helvetica", "bold");
    doc.setFontSize(8.5);
    let x3 = mL;
    wrapped.forEach((lines, ci) => {
      doc.text(lines, x3 + 3, ty + 5.5);
      x3 += colW[ci];
    });
    ty += h;
  });
  if (doc.getNumberOfPages() === 1) {
    doc.setDrawColor(...pri);
    doc.setLineWidth(0.45);
    doc.rect(mL, y, cW, ty - y, "S");
  }
  y = ty + 10;

  // Cláusulas.
  doc.setFont("helvetica", "normal");
  doc.setFontSize(8.8);
  doc.setTextColor(...BK);
  clauses.forEach((c) => {
    const lines = doc.splitTextToSize(c, cW - 7);
    doc.text("•", mL + 0.5, y);
    doc.text(lines, mL + 5, y);
    y += lines.length * 4.8 + 2.5;
  });
  y += 6;
  if (y > H - 85) {
    doc.addPage();
    y = 20;
  }

  // Firmas.
  const bW = (cW - 5) / 2, bH = 50, lx = mL, rx = mL + bW + 5;
  doc.setFillColor(...pri);
  doc.rect(lx, y, bW, 9, "F");
  doc.rect(rx, y, bW, 9, "F");
  doc.setTextColor(255, 255, 255);
  doc.setFont("helvetica", "bold");
  doc.setFontSize(8.5);
  doc.text(isE ? "Recibe el material EL TRABAJADOR:" : "Devuelve el material EL TRABAJADOR:", lx + 3, y + 6);
  doc.text(isE ? "Entrega el material LA EMPRESA:" : "Recibe el material LA EMPRESA:", rx + 3, y + 6);
  doc.setFillColor(...light);
  doc.rect(lx, y + 9, bW, bH, "F");
  doc.rect(rx, y + 9, bW, bH, "F");
  doc.setTextColor(...BK);
  doc.text(r.user.name, lx + 3, y + 15);
  if (r.responsible) doc.text(r.responsible, rx + 3, y + 15);
  doc.setDrawColor(...pri);
  doc.setLineWidth(0.4);
  doc.line(lx + 4, y + 9 + bH - 16, lx + bW - 4, y + 9 + bH - 16);
  doc.line(rx + 4, y + 9 + bH - 16, rx + bW - 4, y + 9 + bH - 16);
  doc.setTextColor(...GR);
  doc.setFont("helvetica", "normal");
  doc.setFontSize(8);
  doc.text(`DNI: ${r.recipient_dni || ""}`, lx + 4, y + 9 + bH - 9);
  doc.text(s.nif ? `${company} NIF ${s.nif}` : company, rx + 4, y + 9 + bH - 9);
  doc.setLineWidth(0.45);
  doc.rect(lx, y, bW, 9 + bH, "S");
  doc.rect(rx, y, bW, 9 + bH, "S");
  y += 9 + bH + 8;

  const closing = isE
    ? "Este justificante formaliza la entrega de los dispositivos, detalla sus características y asegura que ambas partes comprenden sus responsabilidades respecto al uso y cuidado del material."
    : "Este justificante formaliza la devolución del material, detalla su estado y libera al trabajador de su custodia a partir de la fecha indicada.";
  doc.setFont("helvetica", "italic");
  doc.setFontSize(8);
  doc.text(doc.splitTextToSize(closing, cW), mL, Math.min(y, H - 24));

  // Pie en todas las páginas.
  const pages = doc.getNumberOfPages();
  for (let p = 1; p <= pages; p++) {
    doc.setPage(p);
    doc.setFillColor(...foot);
    doc.rect(0, H - 14, W, 14, "F");
    doc.setTextColor(255, 255, 255);
    doc.setFont("helvetica", "normal");
    doc.setFontSize(8);
    if (s.web) doc.text(s.web, mL, H - 7);
    doc.text(`Pág. ${p} de ${pages}`, W / 2, H - 7, { align: "center" });
    if (s.email) doc.text(s.email, W - mR, H - 7, { align: "right" });
  }
  if (!r.number) {
    doc.setPage(1);
    doc.setTextColor(220, 220, 220);
    doc.setFont("helvetica", "bold");
    doc.setFontSize(60);
    doc.text("BORRADOR", W / 2, H / 2, { align: "center", angle: 30 });
  }
  return doc;
}

export function downloadDeliveryPdf(r: PdfRecord, s: DeliverySettings) {
  buildDeliveryPdf(r, s).save(pdfFileName(r));
}
