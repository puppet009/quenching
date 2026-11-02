import { type ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { tokenStore } from "@quenching/shared";

/**
 * 页面级登录拦截。
 *
 * 权限策略（已与产品确认）：公开页面可匿名浏览，仅在「需要身份的动作/页面」拦截。
 * 需要整页登录的路由（个人中心、我的训练、复盘报告、实训工作台）用本组件包裹，
 * 未登录时跳 /login，并通过 location.state.from 携带原目标，登录成功后回跳。
 *
 * 纯动作级拦截（如详情页「开始训练」）不经过本组件，直接在事件里跳转 /login。
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const location = useLocation();

  if (!tokenStore.get()) {
    const from = `${location.pathname}${location.search}`;
    return <Navigate to="/login" state={{ from }} replace />;
  }

  return <>{children}</>;
}
