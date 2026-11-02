// 管理后台独立登录页（g9 拆分）
//
// 与用户端登录页的区别：
//   - 仅登录，无注册入口（后台账号由系统开通）
//   - 登录成功后立即拉取 /admin/profile 校验管理员角色
//   - 非管理员账号提示无权限，不进入后台
//   - 登录态在 admin 应用内闭环，不依赖用户端
import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Logo, PasswordInput, login, tokenStore } from "@quenching/shared";
import { getAdminProfile, hasAdminRole } from "../services/admin";

export function LoginPage() {
  const navigate = useNavigate();
  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    if (!account.trim()) {
      setError("请输入手机号或邮箱");
      return;
    }
    if (password.length < 8) {
      setError("密码至少 8 位");
      return;
    }
    setLoading(true);
    try {
      // 1) 换取 token（http 拦截器已解包统一响应体）
      const data = await login(account, password);
      tokenStore.save(data.token.access_token, data.token.refresh_token);

      // 2) 校验管理员角色
      const profile = await getAdminProfile();
      if (!hasAdminRole(profile)) {
        tokenStore.clear();
        setError("该账号没有管理后台访问权限，请联系管理员开通");
        return;
      }
      localStorage.setItem("admin_profile", JSON.stringify(profile));
      navigate("/admin/dashboard", { replace: true });
    } catch (err: unknown) {
      // 角色校验失败等情况，清理可能已保存的 token，避免被守卫误判为“已登录无权限”
      tokenStore.clear();
      const ae = err as { response?: { data?: { msg?: string } }; code?: number };
      if (ae.code === 10002 || ae.response?.data?.msg) {
        setError(ae.response?.data?.msg || "账号或密码错误");
      } else {
        setError("网络错误，请稍后重试");
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-ink-50 flex items-center justify-center p-4">
      <div className="w-full max-w-md bg-white rounded-2xl border border-ink-200 shadow-sm p-8 md:p-10">
        <div className="flex flex-col items-center mb-8">
          <Logo size="lg" />
          <h1 className="mt-4 text-lg font-bold text-ink-800">淬火管理后台</h1>
          <p className="text-[13px] text-ink-500 mt-1">仅限管理员登录，账号由系统开通</p>
        </div>

        <form onSubmit={submit} className="space-y-4">
          <label className="block">
            <span className="block text-xs text-ink-500 mb-1.5">账号（手机号 / 邮箱）</span>
            <input
              className="form-input"
              placeholder="请输入手机号或邮箱"
              value={account}
              onChange={(e) => setAccount(e.target.value)}
              autoComplete="username"
            />
          </label>
          <div>
            <label htmlFor="admin-password" className="block text-xs text-ink-500 mb-1.5">密码</label>
            <PasswordInput
              id="admin-password"
              placeholder="请输入密码"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
            />
          </div>

          {error && <p className="text-[13px] text-danger">{error}</p>}

          <button
            type="submit"
            disabled={loading}
            className="w-full h-11 rounded-lg bg-brand-500 hover:bg-brand-600 text-white font-semibold text-[15px] transition-colors disabled:opacity-60"
          >
            {loading ? "正在验证…" : "登 录"}
          </button>
        </form>
      </div>
    </div>
  );
}
