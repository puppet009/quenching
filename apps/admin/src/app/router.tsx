import { createBrowserRouter, Navigate } from "react-router-dom";
import { AdminLayout } from "../components/AdminLayout";
import { RequireAdmin } from "../components/RequireAdmin";
import { LoginPage } from "../pages/LoginPage";
import { AdminDashboardPage } from "../pages/AdminDashboardPage";
import { ProjectAdminPage } from "../pages/ProjectAdminPage";
import { DefectAdminPage } from "../pages/DefectAdminPage";
import { PitfallAdminPage } from "../pages/PitfallAdminPage";
import { SandboxMonitorPage } from "../pages/SandboxMonitorPage";
import { ResourcesPage } from "../pages/ResourcesPage";
import { NpsAdminPage } from "../pages/NpsAdminPage";

export const router = createBrowserRouter([
  // 管理后台独立登录页（在 admin 应用内闭环）
  { path: "/login", element: <LoginPage /> },

  // ===== 管理后台（T4-7） =====
  {
    path: "/admin",
    element: (
      <RequireAdmin>
        <AdminLayout />
      </RequireAdmin>
    ),
    children: [
      { index: true, element: <Navigate to="/admin/dashboard" replace /> },
      { path: "dashboard", element: <AdminDashboardPage /> },
      { path: "projects", element: <ProjectAdminPage /> },
      { path: "defects", element: <DefectAdminPage /> },
      { path: "pitfalls", element: <PitfallAdminPage /> },
      { path: "sandbox", element: <SandboxMonitorPage /> },
      { path: "resources", element: <RequireAdmin permission="*"><ResourcesPage /></RequireAdmin> },
      {
        path: "feedback",
        children: [
          { index: true, element: <Navigate to="/admin/feedback/nps" replace /> },
          { path: "nps", element: <NpsAdminPage /> },
        ],
      },
    ],
  },

  // 其余路径一律进入后台
  { path: "/", element: <Navigate to="/admin/dashboard" replace /> },
  { path: "*", element: <Navigate to="/admin/dashboard" replace /> },
]);
