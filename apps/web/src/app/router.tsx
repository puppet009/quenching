import { createBrowserRouter } from "react-router-dom";
import { AppLayout } from "../components/AppLayout";
import { HomePage } from "../pages/HomePage";
import { ProjectsPage } from "../pages/ProjectsPage";
import { ProjectDetailPage } from "../pages/ProjectDetailPage";
import { TemplateDetailPage } from "../pages/TemplateDetailPage";
import { ComingSoonPage } from "../pages/ComingSoonPage";
import { TrainingEntryPage } from "../pages/TrainingEntryPage";
import { ChatDemoPage } from "../pages/ChatDemoPage";
import { RadarDemoPage } from "../pages/RadarDemoPage";
import { ReviewDemoPage } from "../pages/ReviewDemoPage";
import { MetricsDemoPage } from "../pages/MetricsDemoPage";
import { LoginPage } from "../pages/LoginPage";
import { ComparisonResultPage } from "../pages/ComparisonResultPage";
import { ProfilePage } from "../pages/ProfilePage";
import { TrainingHistoryPage } from "../pages/TrainingHistoryPage";
import { ReviewPage } from "../pages/ReviewPage";
import { TrainingPage } from "../pages/TrainingPage";
import { LegalPage } from "../pages/LegalPage";
import { RequireAuth } from "../auth/RequireAuth";

// 用户端路由；管理后台已物理拆分到 apps/admin（独立 SPA）。
// 权限策略：公开页匿名可浏览；需要身份的页面用 RequireAuth 包裹，
// 未登录跳 /login 并携带原目标，登录成功后回跳。
export const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  {
    element: <AppLayout />,
    children: [
      // 公开页面
      { path: "/", element: <HomePage /> },
      { path: "/projects", element: <ProjectsPage /> },
      { path: "/projects/:id", element: <ProjectDetailPage /> },
      { path: "/projects/t/:slug", element: <TemplateDetailPage /> },
      // 实训环境入口页（介绍 + 开启实训 + 最近实训）；真正工作台在 /training/:id
      { path: "/training", element: <TrainingEntryPage /> },
      {
        path: "/arena",
        element: (
          <ComingSoonPage
            title="挑战赛"
            description="限时故障排位与同场竞技玩法即将开放，先用项目大厅里的实战项目热热身吧。"
          />
        ),
      },
      {
        path: "/forum",
        element: (
          <ComingSoonPage
            title="论坛"
            description="排障经验交流与踩坑分享社区正在建设中，敬请期待。"
          />
        ),
      },
      // 法律条款（公开页）
      { path: "/legal/:kind", element: <LegalPage /> },
      // 需要登录
      { path: "/profile", element: <RequireAuth><ProfilePage /></RequireAuth> },
      { path: "/history", element: <RequireAuth><TrainingHistoryPage /></RequireAuth> },
      { path: "/review/:id", element: <RequireAuth><ReviewPage /></RequireAuth> },
      // 人机对照（由个人中心/复盘流程进入）
      { path: "/comparison-result", element: <RequireAuth><ComparisonResultPage /></RequireAuth> },
    ],
  },
  // 实训工作台：全屏沉浸式，不套统一导航壳；需要登录
  {
    path: "/training/:id",
    element: <RequireAuth><TrainingPage /></RequireAuth>,
  },
  // Demo 页（无正式入口，保留独立全屏形态）
  { path: "/chat-demo", element: <ChatDemoPage /> },
  { path: "/radar-demo", element: <RadarDemoPage /> },
  { path: "/review-demo", element: <ReviewDemoPage /> },
  { path: "/metrics-demo", element: <MetricsDemoPage /> },
]);
