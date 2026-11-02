import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { tokenStore, type AuthUser } from "@quenching/shared";

/**
 * 用户端登录态（apps/web 专用，不放进 shared，避免影响管理后台）。
 *
 * 设计说明：
 * - 后端目前只有 /auth/login、/auth/register，没有 /users/me，
 *   所以登录态以 access token 是否存在为准；昵称/头像在登录注册成功时
 *   随 AuthResult 一并暂存到 localStorage，供导航栏展示。
 * - tokenStore.clear() 本身不触发 React 重渲染，统一走这里的 logout()，
 *   并配合 quenching:auth-changed 事件让所有挂载中的布局同步刷新。
 */

const USER_KEY = "quenching_user";
const AUTH_EVENT = "quenching:auth-changed";

function syncAuthState() {
  window.dispatchEvent(new Event(AUTH_EVENT));
}

/** 读取暂存的用户信息（登录/注册时写入） */
export function loadStoredUser(): AuthUser | null {
  try {
    const raw = localStorage.getItem(USER_KEY);
    return raw ? (JSON.parse(raw) as AuthUser) : null;
  } catch {
    return null;
  }
}

/** 登录/注册成功后保存 token 与用户信息，并通知导航态刷新 */
export function saveSession(accessToken: string, refreshToken: string, user: AuthUser) {
  tokenStore.save(accessToken, refreshToken);
  localStorage.setItem(USER_KEY, JSON.stringify(user));
  syncAuthState();
}

interface AuthContextValue {
  /** 当前是否已登录（access token 存在） */
  isLoggedIn: boolean;
  /** 当前用户信息（极端情况下可能为 null：有 token 但本地暂存丢失） */
  user: AuthUser | null;
  /** 退出登录：清除本地凭证并刷新导航态 */
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue>({
  isLoggedIn: false,
  user: null,
  logout: () => {},
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isLoggedIn, setIsLoggedIn] = useState<boolean>(() => !!tokenStore.get());
  const [user, setUser] = useState<AuthUser | null>(() => loadStoredUser());

  const refresh = useCallback(() => {
    setIsLoggedIn(!!tokenStore.get());
    setUser(loadStoredUser());
  }, []);

  useEffect(() => {
    // 同标签页登录/登出；storage 事件用于多标签页同步。
    window.addEventListener(AUTH_EVENT, refresh);
    window.addEventListener("storage", refresh);
    return () => {
      window.removeEventListener(AUTH_EVENT, refresh);
      window.removeEventListener("storage", refresh);
    };
  }, [refresh]);

  const logout = useCallback(() => {
    tokenStore.clear();
    localStorage.removeItem(USER_KEY);
    syncAuthState();
  }, []);

  const value = useMemo(
    () => ({ isLoggedIn, user, logout }),
    [isLoggedIn, user, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  return useContext(AuthContext);
}
