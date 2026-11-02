import { useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { Logo } from "@quenching/shared";
import { useAuth } from "../auth/auth";
import { siteInfo } from "../config/site";

/**
 * 全站统一布局壳（用户端）
 *
 * - 未登录也可见五项主导航：首页 / 项目大厅 / 实训环境 / 挑战赛 / 论坛
 * - 挑战赛、论坛本期不做，挂「即将上线」角标并指向占位页
 * - /training 为实训环境入口页（开启实训 + 最近实训），桌面工作台在 /training/:id
 * - 右侧按登录态切换：登录/注册 或 用户下拉（个人中心 / 我的训练 / 退出登录）
 * - 窄屏折叠为汉堡菜单；页脚统一收编于此
 */

interface NavItem {
  label: string;
  to: string;
  end?: boolean;
  badge?: string;
}

const NAV_ITEMS: NavItem[] = [
  { label: "首页", to: "/", end: true },
  { label: "项目大厅", to: "/projects" },
  { label: "实训环境", to: "/training" },
  { label: "挑战赛", to: "/arena", badge: "即将上线" },
  { label: "论坛", to: "/forum", badge: "即将上线" },
];

export function AppLayout() {
  const { isLoggedIn, user, logout } = useAuth();
  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const userMenuRef = useRef<HTMLDivElement>(null);

  const displayName = user?.nickname || "学员";
  const avatarChar = displayName.slice(0, 1);

  // 点击外部 / Esc 关闭用户下拉
  useEffect(() => {
    if (!userMenuOpen) return;
    const onPointerDown = (e: MouseEvent) => {
      if (userMenuRef.current && !userMenuRef.current.contains(e.target as Node)) {
        setUserMenuOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setUserMenuOpen(false);
    };
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [userMenuOpen]);

  const handleLogout = () => {
    setUserMenuOpen(false);
    setMenuOpen(false);
    logout();
    navigate("/");
  };

  return (
    <div className="min-h-screen flex flex-col bg-ink-50 text-ink-800">
      {/* ===== 顶部导航 ===== */}
      <header className="sticky top-0 z-50 bg-white/80 backdrop-blur-md border-b border-ink-100">
        <div className="page-container h-16 flex items-center justify-between gap-4">
          {/* Logo */}
          <Link to="/" className="flex items-center shrink-0" aria-label="淬火首页">
            <Logo size="md" />
          </Link>

          {/* 桌面端主导航 */}
          <nav className="hidden md:flex items-center h-16">
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  `relative h-16 flex items-center px-4 text-[14px] font-medium transition-colors ${
                    isActive
                      ? "text-brand-600"
                      : "text-ink-600 hover:text-ink-900"
                  }`
                }
              >
                {({ isActive }) => (
                  <>
                    <span className="flex items-center gap-1.5">
                      {item.label}
                      {item.badge && (
                        <span className="px-1.5 py-0.5 rounded-md bg-ink-50 border border-ink-200 text-ink-400 text-[10px] font-medium leading-none">
                          {item.badge}
                        </span>
                      )}
                    </span>
                    {isActive && (
                      <span className="absolute bottom-0 left-4 right-4 h-[2.5px] rounded-full bg-brand-500" />
                    )}
                  </>
                )}
              </NavLink>
            ))}
          </nav>

          {/* 桌面端右侧：登录态 */}
          <div className="hidden md:flex items-center gap-3 shrink-0">
            {isLoggedIn ? (
              <div className="relative" ref={userMenuRef}>
                <button
                  type="button"
                  onClick={() => setUserMenuOpen((v) => !v)}
                  className="flex items-center gap-2 h-9 pl-1 pr-2.5 rounded-full border border-ink-200 hover:border-brand-300 hover:bg-brand-50/50 transition-colors"
                  aria-haspopup="menu"
                  aria-expanded={userMenuOpen}
                >
                  <span className="w-7 h-7 rounded-full bg-brand-500 text-white text-[13px] font-bold flex items-center justify-center">
                    {avatarChar}
                  </span>
                  <span className="text-[13px] font-medium text-ink-800 max-w-[120px] truncate">
                    {displayName}
                  </span>
                  <svg
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    className={`text-ink-400 transition-transform ${userMenuOpen ? "rotate-180" : ""}`}
                  >
                    <path d="m6 9 6 6 6-6" />
                  </svg>
                </button>

                {userMenuOpen && (
                  <div
                    role="menu"
                    className="absolute right-0 mt-2 w-44 bg-white rounded-xl border border-ink-100 shadow-lg shadow-ink-900/5 py-1.5"
                  >
                    <MenuLink to="/profile" onClick={() => setUserMenuOpen(false)}>
                      个人中心
                    </MenuLink>
                    <MenuLink to="/history" onClick={() => setUserMenuOpen(false)}>
                      我的训练
                    </MenuLink>
                    <div className="my-1.5 border-t border-ink-100" />
                    <button
                      type="button"
                      role="menuitem"
                      onClick={handleLogout}
                      className="w-full text-left px-4 py-2 text-[13px] font-medium text-danger hover:bg-danger-50 transition-colors"
                    >
                      退出登录
                    </button>
                  </div>
                )}
              </div>
            ) : (
              <>
                <Link
                  to="/login"
                  className="h-9 px-4 rounded-lg text-[14px] font-medium text-ink-600 hover:text-ink-900 transition-colors inline-flex items-center justify-center"
                >
                  登录
                </Link>
                <Link
                  to="/login"
                  state={{ mode: "register" }}
                  className="h-9 px-4 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-[14px] font-semibold inline-flex items-center transition-colors"
                >
                  免费注册
                </Link>
              </>
            )}
          </div>

          {/* 移动端汉堡按钮 */}
          <button
            type="button"
            className="md:hidden w-9 h-9 -mr-2 flex items-center justify-center text-ink-700"
            onClick={() => setMenuOpen((v) => !v)}
            aria-label={menuOpen ? "关闭菜单" : "打开菜单"}
            aria-expanded={menuOpen}
          >
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              {menuOpen ? (
                <>
                  <path d="M18 6 6 18" />
                  <path d="m6 6 12 12" />
                </>
              ) : (
                <>
                  <path d="M4 7h16" />
                  <path d="M4 12h16" />
                  <path d="M4 17h16" />
                </>
              )}
            </svg>
          </button>
        </div>

        {/* 移动端展开菜单 */}
        {menuOpen && (
          <div className="md:hidden border-t border-ink-100 bg-white">
            <nav className="page-container py-3 flex flex-col">
              {NAV_ITEMS.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  onClick={() => setMenuOpen(false)}
                  className={({ isActive }) =>
                    `h-11 flex items-center justify-between rounded-lg px-3 text-[14px] font-medium ${
                      isActive ? "text-brand-600 bg-brand-50" : "text-ink-700 hover:bg-ink-50"
                    }`
                  }
                >
                  <span>{item.label}</span>
                  {item.badge && (
                    <span className="px-1.5 py-0.5 rounded-md bg-ink-50 border border-ink-200 text-ink-400 text-[10px] font-medium">
                      {item.badge}
                    </span>
                  )}
                </NavLink>
              ))}

              <div className="my-3 border-t border-ink-100" />

              {isLoggedIn ? (
                <>
                  <MenuLink to="/profile" onClick={() => setMenuOpen(false)}>个人中心</MenuLink>
                  <MenuLink to="/history" onClick={() => setMenuOpen(false)}>我的训练</MenuLink>
                  <button
                    type="button"
                    onClick={handleLogout}
                    className="mt-1 h-11 text-left px-3 rounded-lg text-[14px] font-medium text-danger hover:bg-danger-50 transition-colors"
                  >
                    退出登录
                  </button>
                </>
              ) : (
                <div className="flex items-center gap-3 pb-1">
                  <Link
                    to="/login"
                    onClick={() => setMenuOpen(false)}
                    className="flex-1 h-10 rounded-lg border border-ink-200 text-ink-700 text-[14px] font-medium inline-flex items-center justify-center"
                  >
                    登录
                  </Link>
                  <Link
                    to="/login"
                    state={{ mode: "register" }}
                    onClick={() => setMenuOpen(false)}
                    className="flex-1 h-10 rounded-lg bg-brand-500 text-white text-[14px] font-semibold inline-flex items-center justify-center"
                  >
                    免费注册
                  </Link>
                </div>
              )}
            </nav>
          </div>
        )}
      </header>

      {/* ===== 页面内容 ===== */}
      <main className="flex-1">
        <Outlet />
      </main>

      {/* ===== 统一页脚 ===== */}
      <footer className="bg-ink-900 border-t border-ink-800">
        <div className="page-container py-14">
          <div className="grid gap-10 md:grid-cols-12">
            {/* 品牌简介 */}
            <div className="md:col-span-5">
              <div className="flex items-center gap-3">
                <span className="w-9 h-9 bg-brand-500 text-white font-bold flex items-center justify-center rounded-lg text-base">淬</span>
                <span className="text-ink-200 font-semibold text-lg">淬火 Quenching</span>
              </div>
              <p className="mt-4 text-[13.5px] text-ink-400 leading-7 max-w-sm">
                AI 时代的工程师生产风险实战与人机协作训练平台。用全真故障仿真，练出驾驭 AI 的工程决策力。
              </p>
            </div>

            {/* 产品导航 */}
            <div className="md:col-span-3">
              <h3 className="text-[13px] font-semibold text-ink-200 mb-4">产品</h3>
              <ul className="space-y-2.5 text-[13px] text-ink-400">
                <li><Link to="/projects" className="hover:text-ink-100 transition-colors">项目大厅</Link></li>
                <li><Link to="/training" className="hover:text-ink-100 transition-colors">实训环境</Link></li>
                <li><Link to="/arena" className="hover:text-ink-100 transition-colors">挑战赛</Link></li>
                <li><Link to="/forum" className="hover:text-ink-100 transition-colors">论坛</Link></li>
              </ul>
            </div>

            {/* 法律与帮助 */}
            <div className="md:col-span-2">
              <h3 className="text-[13px] font-semibold text-ink-200 mb-4">法律</h3>
              <ul className="space-y-2.5 text-[13px] text-ink-400">
                <li><Link to="/legal/terms" className="hover:text-ink-100 transition-colors">服务协议</Link></li>
                <li><Link to="/legal/privacy" className="hover:text-ink-100 transition-colors">隐私政策</Link></li>
                <li>
                  <a href={`mailto:${siteInfo.contactEmail}`} className="hover:text-ink-100 transition-colors">
                    联系我们
                  </a>
                </li>
              </ul>
            </div>

            {/* 联系方式 */}
            <div className="md:col-span-2">
              <h3 className="text-[13px] font-semibold text-ink-200 mb-4">联系</h3>
              <p className="text-[13px] text-ink-400 break-all">{siteInfo.contactEmail}</p>
            </div>
          </div>
        </div>

        {/* 备案与版权栏 */}
        <div className="border-t border-ink-800">
          <div className="page-container py-5 flex flex-col md:flex-row flex-wrap items-center justify-center md:justify-between gap-x-6 gap-y-2">
            <p className="text-[12px] text-ink-500">
              © {siteInfo.copyrightYear} 淬火 Quenching. All rights reserved.
              {siteInfo.companyName && <span className="ml-2">{siteInfo.companyName}</span>}
            </p>

            <div className="flex flex-wrap items-center justify-center gap-x-5 gap-y-2 text-[12px] text-ink-500">
              {/* ICP 备案 */}
              {siteInfo.icp ? (
                <a
                  href={siteInfo.icpUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:text-ink-200 transition-colors"
                >
                  {siteInfo.icp}
                </a>
              ) : (
                <span className="text-ink-600">ICP 备案信息待补充</span>
              )}

              {/* 公安备案 */}
              {siteInfo.policeRecord && (
                <a
                  href={siteInfo.policeUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 hover:text-ink-200 transition-colors"
                >
                  {/* 公安备案徽标（红盾轮廓） */}
                  <svg width="14" height="16" viewBox="0 0 14 16" fill="none" aria-hidden="true">
                    <path
                      d="M7 1 1.8 2.9v4.1c0 3.2 2.2 6.2 5.2 7 3-.8 5.2-3.8 5.2-7V2.9L7 1Z"
                      fill="#c62f2f"
                      stroke="#c62f2f"
                      strokeWidth="0.6"
                      strokeLinejoin="round"
                    />
                    <path d="M5 8l1.4 1.4L9.2 6.6" stroke="#fff" strokeWidth="1.1" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  {siteInfo.policeRecord}
                </a>
              )}
            </div>
          </div>
        </div>
      </footer>
    </div>
  );
}

/** 下拉菜单项（桌面用户菜单与移动端菜单共用） */
function MenuLink({
  to,
  onClick,
  children,
}: {
  to: string;
  onClick?: () => void;
  children: React.ReactNode;
}) {
  return (
    <Link
      to={to}
      role="menuitem"
      onClick={onClick}
      className="block px-4 py-2 text-[13px] font-medium text-ink-700 hover:bg-ink-50 hover:text-ink-900 transition-colors"
    >
      {children}
    </Link>
  );
}
