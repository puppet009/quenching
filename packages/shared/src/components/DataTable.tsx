// 通用后台表格组件（T4-7）
//
// 功能：
//   - 统一的表格样式
//   - 支持自定义列渲染
//   - 空状态 / 加载态
//   - 顶部操作栏（标题 + 操作按钮）
//   - 筛选栏插槽
//   - 分页（集成 Pagination 组件）

import { ReactNode } from "react";
import { Pagination } from "./Pagination";

export interface DataTableColumn<T> {
  key: string;
  title: string;
  width?: string;
  align?: "left" | "center" | "right";
  render?: (row: T, index: number) => ReactNode;
  dataIndex?: keyof T;
}

interface DataTableProps<T> {
  /** 表格标题 */
  title?: string;
  /** 表格描述 */
  description?: string;
  /** 列配置 */
  columns: DataTableColumn<T>[];
  /** 数据 */
  data: T[];
  /** 行 key 字段 */
  rowKey: keyof T | ((row: T) => string);
  /** 加载中 */
  loading?: boolean;
  /** 空状态文字 */
  emptyText?: string;
  /** 顶部操作区（右侧） */
  toolbar?: ReactNode;
  /** 筛选区 */
  filter?: ReactNode;
  /** 当前页码 */
  currentPage?: number;
  /** 总页数 */
  totalPages?: number;
  /** 总条数 */
  totalItems?: number;
  /** 每页条数 */
  pageSize?: number;
  /** 页码变化 */
  onPageChange?: (page: number) => void;
  /** 行点击 */
  onRowClick?: (row: T) => void;
  className?: string;
}

export function DataTable<T>({
  title,
  description,
  columns,
  data,
  rowKey,
  loading = false,
  emptyText = "暂无数据",
  toolbar,
  filter,
  currentPage,
  totalPages,
  totalItems,
  pageSize,
  onPageChange,
  onRowClick,
  className = "",
}: DataTableProps<T>) {
  const getRowKey = (row: T, idx: number): string => {
    if (typeof rowKey === "function") return rowKey(row);
    return String(row[rowKey] ?? idx);
  };

  const alignClass = (align?: string) => {
    switch (align) {
      case "center": return "text-center";
      case "right": return "text-right";
      default: return "text-left";
    }
  };

  return (
    <div className={`bg-white rounded-xl border border-ink-100 overflow-hidden ${className}`}>
      {/* 顶部标题栏 */}
      {(title || toolbar) && (
        <div className="px-5 py-4 border-b border-ink-50 flex items-center justify-between">
          <div>
            {title && <h3 className="text-[15px] font-bold text-ink-800">{title}</h3>}
            {description && <p className="text-[12px] text-ink-500 mt-0.5">{description}</p>}
          </div>
          {toolbar && <div className="flex items-center gap-2">{toolbar}</div>}
        </div>
      )}

      {/* 筛选区 */}
      {filter && (
        <div className="px-5 py-3 border-b border-ink-50 bg-ink-50/30">
          {filter}
        </div>
      )}

      {/* 表格 */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="bg-ink-50/80 border-b border-ink-100">
              {columns.map((col) => (
                <th
                  key={col.key}
                  className={`px-4 py-2.5 text-xs font-semibold text-ink-500 ${alignClass(col.align)}`}
                  style={col.width ? { width: col.width, minWidth: col.width } : undefined}
                >
                  {col.title}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={columns.length} className="text-center py-12 text-ink-400">
                  <div className="flex items-center justify-center gap-2">
                    <div className="w-4 h-4 border-2 border-brand-200 border-t-brand-500 rounded-full animate-spin" />
                    加载中...
                  </div>
                </td>
              </tr>
            ) : data.length === 0 ? (
              <tr>
                <td colSpan={columns.length} className="text-center py-12 text-ink-400">
                  <div className="text-3xl mb-2">📭</div>
                  {emptyText}
                </td>
              </tr>
            ) : (
              data.map((row, idx) => (
                <tr
                  key={getRowKey(row, idx)}
                  className={`border-b border-ink-50 transition-colors ${
                    onRowClick ? "cursor-pointer hover:bg-ink-50/50" : "hover:bg-ink-50/30"
                  }`}
                  onClick={() => onRowClick?.(row)}
                >
                  {columns.map((col) => (
                    <td
                      key={col.key}
                      className={`px-4 py-3 ${alignClass(col.align)}`}
                    >
                      {col.render
                        ? col.render(row, idx)
                        : col.dataIndex
                          ? (row[col.dataIndex] as unknown as ReactNode)
                          : null}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* 分页 */}
      {totalPages !== undefined && totalPages > 0 && (
        <div className="px-5 py-3 border-t border-ink-50">
          <Pagination
            currentPage={currentPage || 1}
            totalPages={totalPages}
            totalItems={totalItems || 0}
            pageSize={pageSize || 10}
            onPageChange={onPageChange || (() => {})}
          />
        </div>
      )}
    </div>
  );
}
