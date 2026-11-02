import { useState, useMemo } from "react";
import {
  CONFIG_TYPE_CONFIG,
  formatFileSize,
  formatUptime,
  type ConfigFile,
  type ConfigItem,
  type ServiceDeployInfo,
} from "../services/configs";

interface ConfigViewerProps {
  /** 配置文件列表 */
  files: ConfigFile[];
  /** 服务部署信息 */
  deployInfo?: ServiceDeployInfo;
  /** 服务名 */
  service?: string;
  /** 加载状态 */
  loading?: boolean;
  /** 服务列表（用于下拉选择） */
  services?: string[];
  /** 服务切换回调 */
  onServiceChange?: (service: string) => void;
  className?: string;
}

/**
 * 配置查看面板组件（T2-24）
 *
 * - 只读查看各服务配置
 * - 左侧文件列表 + 右侧内容展示
 * - 支持环境变量（键值对）和配置文件（原文）两种展示
 * - 部署信息概览卡片
 * - 敏感字段脱敏显示
 * - 搜索过滤配置项
 * - 点击复制值
 */
export function ConfigViewer({
  files,
  deployInfo,
  service = "",
  loading = false,
  services = [],
  onServiceChange,
  className = "",
}: ConfigViewerProps) {
  const [selectedFileId, setSelectedFileId] = useState<string | null>(
    files.length > 0 ? files[0].id : null,
  );
  const [searchKeyword, setSearchKeyword] = useState("");
  const [activeTab, setActiveTab] = useState<"configs" | "deploy">("configs");
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  // 选中的文件
  const selectedFile = useMemo(
    () => files.find((f) => f.id === selectedFileId) || null,
    [files, selectedFileId],
  );

  // 过滤后的配置项
  const filteredItems = useMemo(() => {
    if (!selectedFile?.items || !searchKeyword) return selectedFile?.items || [];
    const kw = searchKeyword.toLowerCase();
    return selectedFile.items.filter(
      (item) =>
        item.key.toLowerCase().includes(kw) ||
        item.value.toLowerCase().includes(kw) ||
        (item.description && item.description.toLowerCase().includes(kw)),
    );
  }, [selectedFile, searchKeyword]);

  // 复制值到剪贴板
  const handleCopy = async (key: string, value: string) => {
    if (value === "********") return; // 脱敏的不能复制
    try {
      await navigator.clipboard.writeText(value);
      setCopiedKey(key);
      setTimeout(() => setCopiedKey(null), 1500);
    } catch {
      // 静默失败
    }
  };

  // 当文件列表变化时，重置选中
  useMemo(() => {
    if (files.length > 0 && !selectedFileId) {
      setSelectedFileId(files[0].id);
    }
  }, [files, selectedFileId]);

  return (
    <div className={`bg-white rounded-xl border border-ink-100 overflow-hidden ${className}`}>
      {/* 头部 */}
      <div className="px-4 py-3 border-b border-ink-100 bg-ink-50/50">
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-2">
            <span className="text-base">⚙️</span>
            <span className="text-[14px] font-bold text-ink-800">服务配置</span>
          </div>
          {services.length > 0 && (
            <select
              value={service}
              onChange={(e) => onServiceChange?.(e.target.value)}
              className="px-2 py-1 text-[12px] bg-white border border-ink-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-brand-500/30 focus:border-brand-400"
            >
              <option value="">选择服务</option>
              {services.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          )}
        </div>

        {/* Tab 切换 */}
        <div className="flex items-center gap-1">
          <button
            onClick={() => setActiveTab("configs")}
            className={`px-3 py-1.5 text-[12px] font-medium rounded-lg transition-colors ${
              activeTab === "configs"
                ? "bg-brand-500 text-white"
                : "text-ink-500 hover:text-ink-700 hover:bg-ink-100"
            }`}
          >
            📄 配置文件
          </button>
          <button
            onClick={() => setActiveTab("deploy")}
            className={`px-3 py-1.5 text-[12px] font-medium rounded-lg transition-colors ${
              activeTab === "deploy"
                ? "bg-brand-500 text-white"
                : "text-ink-500 hover:text-ink-700 hover:bg-ink-100"
            }`}
          >
            🚀 部署信息
          </button>
        </div>
      </div>

      {/* 内容区 */}
      {loading ? (
        <SkeletonView />
      ) : activeTab === "configs" ? (
        <div className="flex" style={{ minHeight: "320px" }}>
          {/* 左侧文件列表 */}
          <div className="w-48 border-r border-ink-100 bg-ink-50/30">
            <ul className="py-1">
              {files.map((file) => (
                <li key={file.id}>
                  <button
                    onClick={() => setSelectedFileId(file.id)}
                    className={`
                      w-full px-3 py-2 text-left transition-colors
                      flex items-center gap-2
                      ${selectedFileId === file.id
                        ? "bg-brand-50 text-brand-700 border-r-2 border-brand-500"
                        : "text-ink-600 hover:bg-ink-100"
                      }
                    `}
                  >
                    <span className="text-sm">{CONFIG_TYPE_CONFIG[file.type].icon}</span>
                    <div className="flex-1 min-w-0">
                      <div className="text-[12px] font-medium truncate">{file.name}</div>
                      <div className="text-[10px] text-ink-400">
                        {formatFileSize(file.size)}
                      </div>
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          </div>

          {/* 右侧内容 */}
          <div className="flex-1 flex flex-col">
            {selectedFile ? (
              <>
                {/* 搜索栏（仅键值对类配置） */}
                {selectedFile.items && (
                  <div className="px-4 py-2 border-b border-ink-100">
                    <input
                      type="text"
                      value={searchKeyword}
                      onChange={(e) => setSearchKeyword(e.target.value)}
                      placeholder="搜索配置项..."
                      className="w-full px-3 py-1.5 text-[12px] bg-ink-50 border border-ink-200 rounded-lg placeholder-ink-400 focus:outline-none focus:ring-2 focus:ring-brand-500/30 focus:border-brand-400"
                    />
                  </div>
                )}

                {/* 文件信息栏 */}
                <div className="px-4 py-2 bg-ink-50/50 border-b border-ink-100 flex items-center justify-between text-[11px] text-ink-500">
                  <span className="font-mono">{selectedFile.path}</span>
                  <span>{formatFileSize(selectedFile.size)}</span>
                </div>

                {/* 内容展示 */}
                <div className="flex-1 overflow-auto" style={{ maxHeight: "360px" }}>
                  {selectedFile.items ? (
                    filteredItems.length === 0 ? (
                      <EmptyConfigState />
                    ) : (
                      <table className="w-full text-[12px]">
                        <thead className="sticky top-0 bg-white">
                          <tr className="text-ink-500 border-b border-ink-100">
                            <th className="px-4 py-2 text-left font-medium">键</th>
                            <th className="px-4 py-2 text-left font-medium">值</th>
                            <th className="px-4 py-2 text-left font-medium">说明</th>
                          </tr>
                        </thead>
                        <tbody>
                          {filteredItems.map((item) => (
                            <ConfigRow
                              key={item.key}
                              item={item}
                              copied={copiedKey === item.key}
                              onCopy={() => handleCopy(item.key, item.value)}
                            />
                          ))}
                        </tbody>
                      </table>
                    )
                  ) : (
                    <pre className="p-4 text-[12px] font-mono text-ink-700 whitespace-pre-wrap overflow-x-auto">
                      {selectedFile.content || "(空文件)"}
                    </pre>
                  )}
                </div>
              </>
            ) : (
              <EmptyFileState />
            )}
          </div>
        </div>
      ) : (
        <DeployInfoPanel deployInfo={deployInfo} />
      )}
    </div>
  );
}

/* ===== 子组件：配置行 ===== */

function ConfigRow({
  item,
  copied,
  onCopy,
}: {
  item: ConfigItem;
  copied: boolean;
  onCopy: () => void;
}) {
  return (
    <tr className="border-b border-ink-50 hover:bg-ink-50/50 group">
      <td className="px-4 py-2 font-mono text-brand-600 font-medium whitespace-nowrap">
        {item.key}
      </td>
      <td className="px-4 py-2 font-mono">
        <div className="flex items-center gap-2">
          <span className={`${item.sensitive ? "text-ink-400" : "text-ink-700"}`}>
            {item.value}
          </span>
          {!item.sensitive && (
            <button
              onClick={onCopy}
              className="opacity-0 group-hover:opacity-100 transition-opacity text-[10px] px-1.5 py-0.5 rounded bg-ink-100 text-ink-500 hover:bg-ink-200 hover:text-ink-700"
              title="复制"
            >
              {copied ? "✓ 已复制" : "复制"}
            </button>
          )}
          {item.sensitive && (
            <span className="text-[9px] px-1 py-0.5 rounded bg-warn-50 text-warn-600 font-medium">
              脱敏
            </span>
          )}
        </div>
      </td>
      <td className="px-4 py-2 text-ink-400 text-[11px]">
        {item.description || "-"}
      </td>
    </tr>
  );
}

/* ===== 子组件：部署信息面板 ===== */

function DeployInfoPanel({ deployInfo }: { deployInfo?: ServiceDeployInfo }) {
  if (!deployInfo) {
    return (
      <div className="py-12 text-center" style={{ minHeight: "320px" }}>
        <div className="text-4xl mb-2">📋</div>
        <div className="text-[13px] text-ink-500 font-medium">暂无部署信息</div>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-4" style={{ minHeight: "320px" }}>
      {/* 基本信息 */}
      <div>
        <h4 className="text-[12px] font-bold text-ink-700 mb-2 flex items-center gap-1.5">
          <span>🚀</span> 基本信息
        </h4>
        <div className="grid grid-cols-2 gap-3">
          <InfoItem label="服务名" value={deployInfo.service} />
          <InfoItem label="命名空间" value={deployInfo.namespace} mono />
          <InfoItem label="镜像" value={`${deployInfo.image}:${deployInfo.image_tag}`} mono />
          <InfoItem
            label="副本状态"
            value={`${deployInfo.ready_replicas} / ${deployInfo.replicas} 就绪`}
            highlight={deployInfo.ready_replicas === deployInfo.replicas ? "ok" : "warn"}
          />
          <InfoItem label="运行时长" value={formatUptime(deployInfo.uptime)} />
          <InfoItem label="端口" value={deployInfo.ports.join(", ")} />
        </div>
      </div>

      {/* 资源配置 */}
      <div>
        <h4 className="text-[12px] font-bold text-ink-700 mb-2 flex items-center gap-1.5">
          <span>💻</span> 资源配置
        </h4>
        <div className="grid grid-cols-2 gap-3">
          <InfoItem label="CPU 请求" value={deployInfo.cpu_request} />
          <InfoItem label="CPU 限制" value={deployInfo.cpu_limit} />
          <InfoItem label="内存请求" value={deployInfo.memory_request} />
          <InfoItem label="内存限制" value={deployInfo.memory_limit} />
        </div>
      </div>

      {/* Pod 列表 */}
      <div>
        <h4 className="text-[12px] font-bold text-ink-700 mb-2 flex items-center gap-1.5">
          <span>📦</span> Pod 列表
        </h4>
        <div className="space-y-1">
          {deployInfo.pods.map((pod) => (
            <div
              key={pod}
              className="flex items-center gap-2 px-3 py-1.5 bg-ink-50 rounded-lg"
            >
              <span className="w-2 h-2 rounded-full bg-ok-500" />
              <span className="text-[12px] font-mono text-ink-700 flex-1">{pod}</span>
              <span className="text-[10px] text-ok-600 font-medium">Running</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/* ===== 子组件：信息项 ===== */

function InfoItem({
  label,
  value,
  mono = false,
  highlight,
}: {
  label: string;
  value: string;
  mono?: boolean;
  highlight?: "ok" | "warn";
}) {
  const valueClass = highlight === "ok"
    ? "text-ok-600"
    : highlight === "warn"
      ? "text-warn-600"
      : "text-ink-700";

  return (
    <div className="bg-ink-50 rounded-lg px-3 py-2">
      <div className="text-[10px] text-ink-400 mb-0.5">{label}</div>
      <div className={`text-[12px] font-medium truncate ${valueClass} ${mono ? "font-mono" : ""}`}>
        {value}
      </div>
    </div>
  );
}

/* ===== 子组件：骨架屏 ===== */

function SkeletonView() {
  return (
    <div className="flex animate-pulse" style={{ minHeight: "320px" }}>
      <div className="w-48 border-r border-ink-100 bg-ink-50/30 py-2 space-y-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="px-3">
            <div className="h-4 w-3/4 bg-ink-100 rounded" />
          </div>
        ))}
      </div>
      <div className="flex-1 p-4 space-y-2">
        <div className="h-4 w-full bg-ink-100 rounded" />
        <div className="h-4 w-5/6 bg-ink-100 rounded" />
        <div className="h-4 w-4/5 bg-ink-100 rounded" />
        <div className="h-4 w-full bg-ink-100 rounded" />
        <div className="h-4 w-3/4 bg-ink-100 rounded" />
      </div>
    </div>
  );
}

/* ===== 子组件：空状态 ===== */

function EmptyConfigState() {
  return (
    <div className="py-10 text-center">
      <div className="text-3xl mb-2">🔍</div>
      <div className="text-[12px] text-ink-500">未找到匹配的配置项</div>
    </div>
  );
}

function EmptyFileState() {
  return (
    <div className="py-12 text-center flex-1 flex items-center justify-center">
      <div>
        <div className="text-3xl mb-2">📄</div>
        <div className="text-[12px] text-ink-500">选择左侧文件查看内容</div>
      </div>
    </div>
  );
}
