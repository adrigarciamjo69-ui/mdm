import { useState, type ReactNode } from "react";
import { Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "./api/client";
import { AuthProvider } from "./auth/AuthContext";
import Layout from "./components/Layout";
import { NotFound, RequireAuth, RequireRole } from "./components/Guards";
import Login from "./pages/Login";
import Summary from "./pages/Summary";
import Inventory, { DevicesRedirect } from "./pages/Inventory";
import AssetDetail, { NewAsset } from "./pages/AssetDetail";
import AssetImport from "./pages/AssetImport";
import Trash from "./pages/Trash";
import DeviceDetail from "./pages/DeviceDetail";
import Groups from "./pages/Groups";
import Enrollment from "./pages/Enrollment";
import Admins from "./pages/Admins";
import Audit from "./pages/Audit";
import Policies from "./pages/Policies";
import PolicyEdit from "./pages/PolicyEdit";
import Activity from "./pages/Activity";
import BatchDetail from "./pages/BatchDetail";
import Scripts from "./pages/Scripts";
import ScriptEdit from "./pages/ScriptEdit";
import Settings from "./pages/Settings";
import AndroidIntegration from "./pages/AndroidIntegration";
import Alerts, { AlertDetail } from "./pages/Alerts";
import { AlertRuleEdit, AlertRuleList } from "./pages/AlertRules";
import Notifications from "./pages/Notifications";
import Reports, { ReportView } from "./pages/Reports";
import Automations, { AutomationEdit } from "./pages/Automations";
import Software from "./pages/Software";
import AppEdit from "./pages/AppEdit";
import Deployments from "./pages/Deployments";
import DeploymentEdit from "./pages/DeploymentEdit";
import Patches from "./pages/Patches";
import PatchPolicyEdit from "./pages/PatchPolicyEdit";

function newQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // Los errores 4xx no se reintentan: repetirlos no cambia la respuesta.
        retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
        refetchOnWindowFocus: true,
        staleTime: 10_000,
      },
    },
  });
}

export function AppProviders({ children }: { children: ReactNode }) {
  const [client] = useState(newQueryClient);
  return (
    <QueryClientProvider client={client}>
      <AuthProvider>{children}</AuthProvider>
    </QueryClientProvider>
  );
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<RequireAuth><Layout /></RequireAuth>}>
        <Route index element={<Summary />} />
        <Route path="dispositivos" element={<DevicesRedirect />} />
        <Route path="inventario" element={<Inventory />} />
        <Route path="inventario/nuevo" element={<RequireRole min="operator"><NewAsset /></RequireRole>} />
        <Route path="inventario/importar" element={<RequireRole min="operator"><AssetImport /></RequireRole>} />
        <Route path="inventario/papelera" element={<Trash />} />
        <Route path="inventario/:id" element={<AssetDetail />} />
        <Route path="dispositivos/:id" element={<DeviceDetail />} />
        <Route path="grupos" element={<Groups />} />
        <Route path="politicas" element={<Policies />} />
        <Route path="politicas/:id" element={<PolicyEdit />} />
        <Route path="actividad" element={<Activity />} />
        <Route path="actividad/:id" element={<BatchDetail />} />
        <Route path="scripts" element={<Scripts />} />
        <Route path="scripts/:id" element={<ScriptEdit />} />
        <Route path="software" element={<Software />} />
        <Route path="software/:id" element={<AppEdit />} />
        <Route path="despliegues" element={<Deployments />} />
        <Route path="despliegues/:id" element={<DeploymentEdit />} />
        <Route path="parches" element={<Patches />} />
        <Route path="parches/politicas/:id" element={<PatchPolicyEdit />} />
        <Route path="ajustes" element={<RequireRole min="admin"><Settings /></RequireRole>} />
        <Route path="integraciones/android" element={<RequireRole min="admin"><AndroidIntegration /></RequireRole>} />
        <Route path="integraciones/avisos" element={<RequireRole min="admin"><Notifications /></RequireRole>} />
        <Route path="alertas" element={<Alerts />} />
        <Route path="alertas/reglas" element={<RequireRole min="admin"><AlertRuleList /></RequireRole>} />
        <Route path="alertas/reglas/:id" element={<RequireRole min="admin"><AlertRuleEdit /></RequireRole>} />
        <Route path="alertas/:id" element={<AlertDetail />} />
        <Route path="informes" element={<Reports />} />
        <Route path="informes/:name" element={<ReportView />} />
        <Route path="automatizaciones" element={<Automations />} />
        <Route path="automatizaciones/:id" element={<AutomationEdit />} />
        <Route path="inscripcion" element={<RequireRole min="operator"><Enrollment /></RequireRole>} />
        <Route path="administradores" element={<RequireRole min="admin"><Admins /></RequireRole>} />
        <Route path="auditoria" element={<RequireRole min="admin"><Audit /></RequireRole>} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}
