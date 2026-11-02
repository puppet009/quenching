// Admin 后台布局（T4-7）
//
// 侧边导航 + 顶部栏 + 主内容区的经典后台布局。
// 功能：
//   - 左侧可折叠侧边栏导航
//   - 顶部面包屑 + 用户信息 + 退出登录
//   - 响应式适配（移动端收起侧边栏）
//   - 菜单按权限显示/隐藏

import { useState, useEffect } from "react";
import { Outlet, Link, useLocation, useNavigate } from "react-router-dom";
import { Logo, tokenStore } from "@quenching/shared";

export interface AdminMenuItem {
  key: string;
  label: string;
  icon: string;
  path?: string;
  permission?: string;
  children?: AdminMenuItem[];
}

/** 后台菜单配置 */
export const ADMIN_MENU: AdminMenuItem[] = [
  {
    key: "dashboard",
    label: "数据概览",
    icon: "📊",
    path: "/admin",
  },
  {
    key: "content",
    label: "内容管理",
    icon: "📁",
    children: [
      { key: "projects", label: "项目管理", icon: "📦", path: "/admin/projects" },
      { key: "defects", label: "故障剧本", icon: "🐛", path: "/admin/defects" },
      { key: "pitfalls", label: "坑点配置", icon: "🎭", path: "/admin/pitfalls" },
    ],
  },
  {
    key: "sandbox",
    label: "沙箱管理",
    icon: "🖥️",
    children: [
      { key: "resource-pool", label: "资源池", icon: "🖥️", path: "/admin/resources", permission: "*" },
      { key: "sandbox-monitor", label: "沙箱监控", icon: "📈", path: "/admin/sandbox" },
    ],
  },
  {
    key: "feedback",
    label: "用户反馈",
    icon: "💬",
    children: [
      { key: "nps", label: "NPS 反馈", icon: "⭐", path: "/admin/feedback/nps" },
    ],
  },
];

export function AdminLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [username, setUsername] = useState("管理员");

  // 从 localStorage 读取用户信息（MVP 阶段占位）
  useEffect(() => {
    try {
      const userInfo = localStorage.getItem("user_info");
      if (userInfo) {
        const user = JSON.parse(userInfo);
        setUsername(user.nickname || "管理员");
      }
    } catch (e) {
      // 忽略
    }
  }, []);

  // 路由变化时关闭移动端菜单
  useEffect(() => {
    setMobileMenuOpen(false);
  }, [location.pathname]);

  // 退出登录
  const handleLogout = () => {
    if (!confirm("确定要退出登录吗？")) return;
    tokenStore.clear();
    localStorage.removeItem("user_info");
    localStorage.removeItem("admin_profile");
    navigate("/login");
  };

  // 判断菜单项是否激活
  const isMenuItemActive = (item: AdminMenuItem): boolean => {
    if (item.path && location.pathname === item.path) return true;
    if (item.children) {
      return item.children.some((child) => location.pathname.startsWith(child.path || ""));
    }
    return false;
  };

  // 当前页面对应的面包屑
  const getBreadcrumbs = (): string[] => {
    const crumbs: string[] = ["管理后台"];
    for (const item of ADMIN_MENU) {
      if (item.path && location.pathname === item.path) {
        crumbs.push(item.label);
        break;
      }
      if (item.children) {
        const child = item.children.find((c) => location.pathname.startsWith(c.path || ""));
        if (child) {
          crumbs.push(item.label, child.label);
          break;
        }
      }
    }
    return crumbs;
  };

  return (
    <div className="min-h-screen bg-ink-50 flex">
      {/* 移动端遮罩 */}
      {mobileMenuOpen && (
        <div
          className="fixed inset-0 bg-black/40 z-40 lg:hidden"
          onClick={() => setMobileMenuOpen(false)}
        />
      )}

      {/* 侧边栏 */}
      <aside
        className={`
          fixed lg:static inset-y-0 left-0 z-50
          bg-white border-r border-ink-200
          flex flex-col
          transition-all duration-200
          ${collapsed ? "w-16" : "w-56"}
          ${mobileMenuOpen ? "translate-x-0" : "-translate-x-full lg:translate-x-0"}
        `}
      >
        {/* Logo 区 */}
        <div className="h-14 flex items-center px-4 border-b border-ink-100 flex-shrink-0">
          <Logo size="sm" />
          {!collapsed && (
            <span className="ml-2 font-bold text-ink-800 text-[15px]">淬火后台</span>
          )}
        </div>

        {/* 菜单 */}
        <nav className="flex-1 overflow-y-auto py-3 px-2">
          {ADMIN_MENU.map((item) => (
            <div key={item.key} className="mb-1">
              {/* 一级菜单（无子菜单时直接跳转） */}
              {item.path ? (
                <Link
                  to={item.path}
                  className={`
                    flex items-center gap-3 px-3 py-2.5 rounded-lg text-[13px] transition-colors
                    ${isMenuItemActive(item)
                      ? "bg-brand-50 text-brand-700 font-medium"
                      : "text-ink-600 hover:bg-ink-50 hover:text-ink-800"
                    }
                  `}
                >
                  <span className="text-lg">{item.icon}</span>
                  {!collapsed && <span>{item.label}</span>}
                </Link>
              ) : (
                <div className="px-3 py-2">
                  {!collapsed && (
                    <div className="text-[11px] font-bold text-ink-400 uppercase tracking-wider mb-1.5 px-1">
                      {item.icon} {item.label}
                    </div>
                  )}
                  {collapsed && (
                    <div className="text-lg text-center" title={item.label}>
                      {item.icon}
                    </div>
                  )}
                  {item.children && !collapsed && (
                    <div className="space-y-0.5">
                      {item.children.map((child) => (
                        <Link
                          key={child.key}
                          to={child.path || "#"}
                          className={`
                            flex items-center gap-2 px-3 py-2 rounded-lg text-[12.5px] transition-colors
                            ${isMenuItemActive(child)
                              ? "bg-brand-50 text-brand-700 font-medium"
                              : "text-ink-500 hover:bg-ink-50 hover:text-ink-700"
                            }
                          `}
                        >
                          <span className="text-sm">{child.icon}</span>
                          <span>{child.label}</span>
                        </Link>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </nav>

        {/* 底部折叠按钮 */}
        <div className="border-t border-ink-100 p-2 flex-shrink-0 hidden lg:block">
          <button
            onClick={() => setCollapsed(!collapsed)}
            className="w-full flex items-center justify-center py-2 rounded-lg text-ink-400 hover:bg-ink-50 hover:text-ink-600 transition-colors"
            title={collapsed ? "展开菜单" : "收起菜单"}
          >
            {collapsed ? "→" : "← 收起"}
          </button>
        </div>
      </aside>

      {/* 主区域 */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* 顶部栏 */}
        <header className="h-14 bg-white border-b border-ink-200 flex items-center justify-between px-4 lg:px-6 sticky top-0 z-30">
          <div className="flex items-center gap-3">
            {/* 移动端菜单按钮 */}
            <button
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              className="lg:hidden p-1.5 rounded-lg hover:bg-ink-100 text-ink-500"
            >
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M3 6h18M3 12h18M3 18h18" />
              </svg>
            </button>

            {/* 面包屑 */}
            <div className="flex items-center gap-2 text-[13px] text-ink-500">
              {getBreadcrumbs().map((crumb, idx, arr) => (
                <span key={idx} className="flex items-center gap-2">
                  <span className={idx === arr.length - 1 ? "text-ink-700 font-medium" : ""}>
                    {crumb}
                  </span>
                  {idx < arr.length - 1 && <span className="text-ink-300">/</span>}
                </span>
              ))}
            </div>
          </div>

          {/* 用户区 */}
          <div className="flex items-center gap-3">
            <div className="hidden sm:flex items-center gap-2">
              <div className="w-8 h-8 rounded-full bg-brand-100 text-brand-600 flex items-center justify-center text-[13px] font-bold">
                {username.charAt(0)}
              </div>
              <div className="text-[13px]">
                <div className="font-medium text-ink-700">{username}</div>
              </div>
            </div>
            <button
              onClick={handleLogout}
              className="text-[12px] text-ink-400 hover:text-danger-600 transition-colors"
            >
              退出
            </button>
          </div>
        </header>

        {/* 内容区 */}
        <main className="flex-1 p-4 lg:p-6 overflow-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
