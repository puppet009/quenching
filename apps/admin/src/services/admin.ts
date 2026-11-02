// 管理员用户信息扩展
import { http } from "@quenching/shared";
import type { AuthUser } from "@quenching/shared";

export type UserRole = "user" | "admin" | "content_editor";

export interface AdminUser extends AuthUser {
  roles: UserRole[];
  permissions: string[];
}

/** 获取当前登录用户的管理员信息（含角色权限） */
export function getAdminProfile() {
  return http.get("/admin/profile") as unknown as Promise<AdminUser>;
}

/** 检查是否有管理员权限 */
export function hasAdminRole(user: { roles?: string[] }): boolean {
  if (!user.roles || user.roles.length === 0) return false;
  return user.roles.includes("admin") || user.roles.includes("content_editor");
}

/** 检查是否有指定权限 */
export function hasPermission(user: { permissions?: string[] }, permission: string): boolean {
  if (!user.permissions) return false;
  if (user.permissions.includes("*")) return true;
  return user.permissions.includes(permission);
}
