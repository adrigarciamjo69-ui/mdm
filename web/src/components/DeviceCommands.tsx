import { Fragment, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { formatDateTime } from "../lib/format";
import { commandTitle, FINISHED, type CommandRow } from "../lib/commands";
import { CommandOutput, CommandStatus } from "../pages/BatchDetail";

interface Row extends CommandRow { batch_id: string | null }

/** Órdenes enviadas a un equipo, con su estado y su salida. */
export default function DeviceCommands({ id }: { id: string }) {
  const [open, setOpen] = useState<string | null>(null);
  const cmds = useQuery({ queryKey: ["device-commands", id], queryFn: () => api<Row[]>("GET", `/devices/${id}/commands`), refetchInterval: 15_000 });
  if (!cmds.data) return <p className="muted">Cargando…</p>;
  if (cmds.data.length === 0) return <p className="empty">A este equipo no se le ha enviado ninguna orden.</p>;
  return (
    <div className="table-wrap">
      <table>
        <thead><tr><th scope="col">Fecha</th><th scope="col">Acción</th><th scope="col">Estado</th><th scope="col"><span className="visually-hidden">Salida</span></th></tr></thead>
        <tbody>
          {cmds.data.map((c) => (
            <Fragment key={c.id}>
              <tr>
                <td className="nowrap">{formatDateTime(c.created_at)}</td>
                <td>{c.batch_id ? <Link to={`/actividad/${c.batch_id}`}>{commandTitle(c.type, c.params)}</Link> : commandTitle(c.type, c.params)}</td>
                <td><CommandStatus status={c.status} />{c.error && <div className="small" style={{ color: "var(--bad)" }}>{c.error}</div>}</td>
                <td className="num">
                  {FINISHED.includes(c.status) && c.status !== "cancelled" && (
                    <button className="btn btn-small" onClick={() => setOpen(open === c.id ? null : c.id)}>{open === c.id ? "Ocultar salida" : "Ver salida"}</button>
                  )}
                </td>
              </tr>
              {open === c.id && <tr><td colSpan={4}><CommandOutput id={c.id} /></td></tr>}
            </Fragment>
          ))}
        </tbody>
      </table>
    </div>
  );
}
