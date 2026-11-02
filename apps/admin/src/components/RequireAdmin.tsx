// 管理员权限路由守卫（T4-7）
//
// 功能：
//   - 检查登录态
//   - 检查管理员角色
//   - 未登录跳登录页
//   - 无权限显示 403 页面
//   - 加载态骨架屏

import { useEffect, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { tokenStore } from "@quenching/shared";
import { getAdminProfile, hasAdminRole, type AdminUser } from "../services/admin";

interface RequireAdminProps {
  children: React.ReactNode;
  /** 需要的权限标识，不传则只校验管理员角色 */
  permission?: string;
}

/**
 * 管理员权限守卫。
 *
 * 使用方式：
 *   <Route path="/admin" element={<RequireAdmin><AdminLayout /></RequireAdmin>}>
 */
export function RequireAdmin({ children, permission }: RequireAdminProps) {
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<AdminUser | null>(null);
  const [isUnauthorized, setIsUnauthorized] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    const token = tokenStore.get();
    if (!token) {
      setLoading(false);
      setIsUnauthorized(true);
      return;
    }

    // 先检查本地缓存的角色信息
    try {
      const cached = localStorage.getItem("admin_profile");
      if (cached) {
        const cachedUser = JSON.parse(cached) as AdminUser;
        if (hasAdminRole(cachedUser)) {
          setUser(cachedUser);
          setLoading(false);
          // 后台静默刷新
          refreshProfile();
          return;
        }
      }
    } catch (e) {
      // 忽略缓存错误
    }

    refreshProfile();
  }, []);

  const refreshProfile = async () => {
    try {
      const profile = await getAdminProfile();
      localStorage.setItem("admin_profile", JSON.stringify(profile));
      setUser(profile);

      if (!hasAdminRole(profile)) {
        setIsUnauthorized(true);
      }
    } catch (err: unknown) {
      console.error("获取管理员信息失败:", err);
      // 401 则跳登录
      const failure = err as { code?: number; status?: number } | null | undefined;
      if (failure?.code === 10002 || failure?.status === 401) {
        tokenStore.clear();
        setIsUnauthorized(true);
      } else {
        // 其他错误，假设无权限（MVP 阶段保守处理）
        setIsUnauthorized(true);
      }
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-ink-50 flex items-center justify-center">
        <div className="text-center">
          <div className="w-10 h-10 border-4 border-brand-200 border-t-brand-500 rounded-full animate-spin mx-auto mb-3" />
          <div className="text-[13px] text-ink-500">正在验证权限...</div>
        </div>
      </div>
    );
  }

  if (isUnauthorized) {
    // 未登录 → 跳登录页；已登录但无权限 → 显示 403
    const token = tokenStore.get();
    if (!token) {
      return <Navigate to="/login" replace />;
    }
    return <ForbiddenPage onBack={() => navigate("/admin/dashboard")} />;
  }

  // 权限校验
  if (permission && user) {
    const hasPerm = user.permissions?.includes("*") || user.permissions?.includes(permission);
    if (!hasPerm) {
      return <ForbiddenPage onBack={() => navigate(-1)} />;
    }
  }

  return <>{children}</>;
}

function ForbiddenPage({ onBack }: { onBack: () => void }) {
  return (
    <div className="min-h-screen bg-ink-50 flex items-center justify-center">
      <div className="text-center max-w-sm px-6">
        <div className="text-6xl mb-4">🚫</div>
        <div className="text-[20px] font-bold text-ink-800 mb-2">无访问权限</div>
        <div className="text-[13px] text-ink-500 mb-6">
          您的账号没有管理后台的访问权限，请联系管理员开通。
        </div>
        <button
          onClick={onBack}
          className="px-6 py-2.5 bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium rounded-xl transition-colors"
        >
          返回首页
        </button>
      </div>
    </div>
  );
}
