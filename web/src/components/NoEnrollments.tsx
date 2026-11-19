import { Link } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import EmptyState from "./EmptyState";

/** Estado vacío cuando el parque no tiene ningún equipo. */
export default function NoEnrollments() {
  const { can } = useAuth();
  return (
    <EmptyState message="Aún no hay dispositivos inscritos.">
      {can("operator") ? (
        <Link to="/inscripcion">Crear un token de inscripción</Link>
      ) : (
        "Un operador o administrador puede inscribir equipos desde la página de inscripción."
      )}
    </EmptyState>
  );
}
