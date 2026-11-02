import { FormEvent, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { ApiError, Logo, PasswordInput, login, register } from "@quenching/shared";
import { saveSession } from "../auth/auth";

type Mode = "login" | "register";

/** 仅允许回跳到站内路径，避免 open redirect */
function safeRedirect(from: unknown): string {
  if (typeof from === "string" && from.startsWith("/") && !from.startsWith("//")) {
    return from;
  }
  return "/projects";
}

export function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const locationState = (location.state as { mode?: Mode; from?: string } | null) ?? null;
  const initialMode = locationState?.mode === "register" ? "register" : "login";
  // 登录拦截时携带的原目标（RequireAuth / 动作拦截写入 state.from）
  const redirectTo = safeRedirect(locationState?.from);
  const [mode, setMode] = useState<Mode>(initialMode);
  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [nickname, setNickname] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const clientValidate = (): string => {
    if (!account.trim()) return "请输入手机号或邮箱";
    if (password.length < 8) return "密码至少 8 位";
    if (mode === "register") {
      if (!/[a-zA-Z]/.test(password) || !/\d/.test(password)) return "密码需同时包含字母和数字";
      if (password !== confirm) return "两次输入的密码不一致";
    }
    return "";
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    const err = clientValidate();
    if (err) {
      setError(err);
      return;
    }
    setLoading(true);
    try {
      // http 响应拦截器已解包统一响应体，await 得到的即 AuthResult
      const data =
        mode === "login"
          ? await login(account, password)
          : await register(account, password, nickname);
      saveSession(data.token.access_token, data.token.refresh_token, data.user);
      // 回跳到登录拦截前的目标页；默认进入项目大厅
      navigate(redirectTo, { replace: true });
    } catch (err: unknown) {
      const ae = err as { response?: { data?: { msg?: string } } };
      setError(err instanceof ApiError ? err.message : ae.response?.data?.msg || "网络错误，请稍后重试");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-ink-50 flex items-center justify-center p-4">
      <div className="w-full max-w-4xl bg-white rounded-2xl border border-ink-200 shadow-sm grid md:grid-cols-[1.05fr_1fr] overflow-hidden">
        {/* 品牌区 */}
        <div className="hidden md:flex flex-col justify-between p-8 bg-gradient-to-br from-white to-brand-50 border-r border-ink-200">
          <Logo size="lg" />
          <div className="my-8">
            <h2 className="text-xl font-bold text-ink-800 leading-relaxed">
              在真实故障里<br />练出驾驭 AI 的判断力
            </h2>
            <p className="text-sm text-ink-500 mt-2">工程师生产风险实战与人机协作训练平台</p>
          </div>
          <div className="space-y-2.5 text-[13px] text-ink-700">
            <Feature color="bg-ok">真实沙箱故障注入，安全演练</Feature>
            <Feature color="bg-ai">AI 助教全程引导，不直接给答案</Feature>
            <Feature color="bg-brand-500">双维能力雷达，量化排障与协作成长</Feature>
          </div>
          <p className="text-[11px] text-ink-400 mt-6">MVP 内测版 · 数据仅用于训练评估</p>
        </div>

        {/* 表单区 */}
        <div className="p-8 md:p-10">
          <div className="flex justify-center mb-7 md:hidden"><Logo size="md" /></div>
          <div className="flex gap-7 border-b border-ink-200 mb-7">
            <Tab active={mode === "login"} onClick={() => { setMode("login"); setError(""); }}>登录</Tab>
            <Tab active={mode === "register"} onClick={() => { setMode("register"); setError(""); }}>注册</Tab>
          </div>

          <form onSubmit={submit} className="space-y-4">
            <Field label="账号（手机号 / 邮箱）" htmlFor="auth-account">
              <input
                id="auth-account"
                className="form-input"
                placeholder="请输入手机号或邮箱"
                value={account}
                onChange={(e) => setAccount(e.target.value)}
                autoComplete="username"
              />
            </Field>
            <Field label="密码" htmlFor="auth-password">
              <PasswordInput
                key={mode}
                id="auth-password"
                placeholder="8-32位，含字母和数字"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={mode === "login" ? "current-password" : "new-password"}
              />
            </Field>
            {mode === "register" && (
              <>
                <Field label="确认密码" htmlFor="auth-confirm-password">
                  <PasswordInput
                    id="auth-confirm-password"
                    passwordLabel="确认密码"
                    placeholder="再次输入密码"
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    autoComplete="new-password"
                  />
                </Field>
                <Field label="昵称（选填）" htmlFor="auth-nickname">
                  <input
                    id="auth-nickname"
                    className="form-input"
                    placeholder="留空将自动生成"
                    value={nickname}
                    onChange={(e) => setNickname(e.target.value)}
                    maxLength={50}
                  />
                </Field>
              </>
            )}

            {mode === "login" && (
              <div className="flex justify-between items-center text-xs text-ink-500 pt-1">
                <label className="flex items-center gap-1.5">
                  <input type="checkbox" className="accent-brand-500" /> 记住我
                </label>
                <span className="text-ink-400">忘记密码？（内测后开放）</span>
              </div>
            )}

            {error && <p className="text-[13px] text-danger">{error}</p>}

            <button
              type="submit"
              disabled={loading}
              className="w-full h-11 rounded-lg bg-brand-500 hover:bg-brand-600 text-white font-semibold text-[15px] transition-colors disabled:opacity-60"
            >
              {loading ? "请稍候…" : mode === "login" ? "登 录" : "注册并登录"}
            </button>

            <p className="text-center text-[13px] text-ink-500">
              {mode === "login" ? (
                <>还没有账号？<button type="button" className="text-brand-500 font-semibold" onClick={() => { setMode("register"); setError(""); }}>立即注册</button></>
              ) : (
                <>已有账号？<button type="button" className="text-brand-500 font-semibold" onClick={() => { setMode("login"); setError(""); }}>去登录</button></>
              )}
            </p>
          </form>
        </div>
      </div>
    </div>
  );
}

function Tab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`pb-2.5 text-[15px] font-semibold relative ${active ? "text-ink-800" : "text-ink-400"}`}
    >
      {children}
      {active && <span className="absolute left-0 right-0 -bottom-px h-[2.5px] bg-brand-500 rounded" />}
    </button>
  );
}

function Field({ label, htmlFor, children }: { label: string; htmlFor: string; children: React.ReactNode }) {
  return (
    <div>
      <label htmlFor={htmlFor} className="block text-xs text-ink-500 mb-1.5">{label}</label>
      {children}
    </div>
  );
}

function Feature({ color, children }: { color: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2.5">
      <span className={`w-[7px] h-[7px] rounded-full mt-[5px] ${color} flex-none`} />
      <span>{children}</span>
    </div>
  );
}
