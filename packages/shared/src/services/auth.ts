import { http } from "./http";

export interface AuthUser {
  id: number;
  account_type: "phone" | "email";
  nickname: string;
  avatar_url?: string;
}

export interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface AuthResult {
  user: AuthUser;
  token: TokenPair;
}

// 响应拦截器已解包统一响应体（resolve payload），这里把返回类型断言为 AuthResult。
export function register(account: string, password: string, nickname?: string) {
  return http.post("/auth/register", {
    account,
    password,
    nickname: nickname || undefined,
  }) as unknown as Promise<AuthResult>;
}

export function login(account: string, password: string) {
  return http.post("/auth/login", {
    account,
    password,
  }) as unknown as Promise<AuthResult>;
}
