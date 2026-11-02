import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { DualDimensionRadarPanel } from "../components/DualDimensionRadarPanel";
import {
  fetchProfileData,
  formatDifficulty,
  difficultyColor,
  formatMode,
  modeIcon,
  updateMyProfile,
  type ProfileData,
  type TrendPoint,
  type TrainingHistoryItem,
  type UserProfile,
} from "../services/profile";
import { COLLAB_LEVELS } from "../services/radar";
import { trainingReviewState } from "../services/trainingHistory";

/**
 * 个人中心页（T4-4 能力雷达与对照入口）
 *
 * 功能：
 * - 用户信息头部（头像、昵称、等级、累计数据）
 * - 双维能力雷达（复用 DualDimensionRadarPanel，本次 vs 历史平均）
 * - 历史趋势折线图（技术分 + 协作分趋势）
 * - 最近训练列表（复盘入口跳 /review/:id）
 * - 人机对照报告入口（完成两种模式训练后显示）
 *
 * 数据：fetchProfileData 聚合 /users/me + /training/sessions/overview
 * + /training/sessions，任一失败展示错误态并允许重试。
 *
 * 访问路径: /profile
 */
export function ProfilePage() {
  const navigate = useNavigate();
  const [data, setData] = useState<ProfileData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetchProfileData();
      setData(result);
    } catch {
      setError("个人数据加载失败，请稍后重试");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return (
      <div className="min-h-screen bg-ink-50 flex items-center justify-center">
        <div className="text-ink-400">加载中...</div>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="min-h-screen bg-ink-50 flex items-center justify-center">
        <div className="text-center">
          <div className="text-ink-500 text-sm mb-4">{error ?? "数据不可用"}</div>
          <button
            onClick={() => void load()}
            className="px-4 h-9 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-sm font-medium transition-colors"
          >
            重新加载
          </button>
        </div>
      </div>
    );
  }

  const { user, radar, trend, recentHistory, canViewComparison } = data;

  return (
    <div className="min-h-screen bg-ink-50">
      {/* 顶部用户信息 */}
      <div className="bg-gradient-to-br from-brand-500 via-brand-600 to-brand-700 text-white">
        <div className="max-w-5xl mx-auto px-6 pt-10 pb-16">
          <div className="flex items-center gap-5">
            {/* 头像 */}
            <div className="w-16 h-16 rounded-full bg-white/20 backdrop-blur flex items-center justify-center text-2xl font-bold border-2 border-white/30 overflow-hidden">
              {user.avatar ? (
                <img src={user.avatar} alt={user.nickname} className="w-full h-full object-cover" />
              ) : (
                user.nickname.slice(0, 1)
              )}
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-3">
                <h1 className="text-xl font-bold">{user.nickname}</h1>
                <span className="px-2 py-0.5 rounded-full bg-white/20 text-xs font-semibold backdrop-blur">
                  {COLLAB_LEVELS[user.collabLevel]?.label || user.collabLevel}
                </span>
              </div>
              <div className="text-white/70 text-sm mt-1">@{user.username}</div>
              {user.bio && (
                <div className="text-white/80 text-xs mt-1.5 leading-relaxed">{user.bio}</div>
              )}
            </div>
            {/* 编辑资料 */}
            <button
              onClick={() => setEditing(true)}
              className="flex-shrink-0 px-3 h-9 rounded-lg bg-white/15 hover:bg-white/25 backdrop-blur text-white text-sm font-medium border border-white/25 transition-colors"
            >
              编辑资料
            </button>
          </div>

          {/* 统计数据 */}
          <div className="grid grid-cols-3 gap-4 mt-6">
            <StatCard label="累计训练" value={user.totalSessions} unit="次" />
            <StatCard label="训练时长" value={Math.round(user.totalMinutes / 60)} unit="小时" />
            <StatCard label="注册时间" value={user.createdAt.slice(5, 10)} unit="" isText />
          </div>
        </div>
      </div>

      {/* 主体内容 */}
      <div className="max-w-5xl mx-auto px-6 -mt-10 pb-10 space-y-6">
        {/* 双维能力雷达 */}
        <DualDimensionRadarPanel data={radar} showCompare size="md" />

        {/* 历史趋势 + 对照入口 */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* 历史趋势（占2列） */}
          <div className="lg:col-span-2 bg-white rounded-2xl border border-ink-100 shadow-sm p-6">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-base font-bold text-ink-900 flex items-center gap-2">
                <span>📈</span> 能力趋势
              </h2>
              <span className="text-xs text-ink-400">最近 {trend.length} 次训练</span>
            </div>
            <TrendChart trend={trend} />
          </div>

          {/* 人机对照入口（占1列） */}
          <div className="bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden">
            {canViewComparison ? (
              <ComparisonEntryCard onClick={() => navigate("/comparison-result")} />
            ) : (
              <ComparisonLockedCard totalSessions={user.totalSessions} />
            )}
          </div>
        </div>

        {/* 最近训练 */}
        <div className="bg-white rounded-2xl border border-ink-100 shadow-sm p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-base font-bold text-ink-900 flex items-center gap-2">
              <span>📋</span> 最近训练
            </h2>
            <button
              onClick={() => navigate("/history")}
              className="text-xs text-brand-600 hover:text-brand-700 font-medium"
            >
              查看全部 →
            </button>
          </div>
          {recentHistory.length === 0 ? (
            <div className="text-center text-ink-400 py-10 text-sm">
              还没有训练记录，去项目大厅开始第一次实训吧
            </div>
          ) : (
            <div className="space-y-2">
              {recentHistory.map((item) => (
                <HistoryRow
                  key={item.id}
                  item={item}
                  onReview={(id) => navigate(`/review/${id}`)}
                  onProgress={(id) => navigate(`/history?session=${id}`)}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* 编辑资料弹窗 */}
      {editing && (
        <EditProfileModal
          user={user}
          onClose={() => setEditing(false)}
          onSaved={(updated) => {
            setData({ ...data, user: { ...user, ...updated } });
            setEditing(false);
          }}
        />
      )}
    </div>
  );
}

/* ===== 子组件 ===== */

/** 编辑资料弹窗：昵称/头像地址/个人简介 */
function EditProfileModal({
  user,
  onClose,
  onSaved,
}: {
  user: UserProfile;
  onClose: () => void;
  onSaved: (updated: Partial<UserProfile>) => void;
}) {
  const [nickname, setNickname] = useState(user.nickname);
  const [avatarUrl, setAvatarUrl] = useState(user.avatar ?? "");
  const [bio, setBio] = useState(user.bio ?? "");
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const nicknameLen = nickname.trim().length;

  const handleSubmit = async () => {
    setFormError(null);
    if (nicknameLen === 0) {
      setFormError("昵称不能为空");
      return;
    }
    if (nickname.length > 50) {
      setFormError("昵称不能超过50个字符");
      return;
    }
    if (bio.length > 200) {
      setFormError("个人简介不能超过200个字符");
      return;
    }

    setSaving(true);
    try {
      const updated = await updateMyProfile({
        nickname: nickname.trim(),
        avatarUrl: avatarUrl.trim(),
        bio: bio.trim(),
      });
      onSaved({
        nickname: updated.nickname,
        avatar: updated.avatar_url || undefined,
        bio: updated.bio || undefined,
      });
    } catch {
      setFormError("保存失败，请稍后重试");
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm px-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md bg-white rounded-2xl shadow-xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-ink-100">
          <h3 className="text-base font-bold text-ink-900">编辑基本资料</h3>
          <button
            onClick={onClose}
            disabled={saving}
            className="w-8 h-8 rounded-lg text-ink-400 hover:bg-ink-50 hover:text-ink-600 transition-colors"
          >
            ✕
          </button>
        </div>

        {/* 表单 */}
        <div className="px-6 py-5 space-y-4">
          {/* 头像预览 */}
          <div className="flex items-center gap-4">
            <div className="w-14 h-14 rounded-full bg-brand-100 flex items-center justify-center text-xl font-bold text-brand-600 overflow-hidden flex-shrink-0">
              {avatarUrl ? (
                <img
                  src={avatarUrl}
                  alt="头像预览"
                  className="w-full h-full object-cover"
                  onError={(e) => {
                    (e.currentTarget as HTMLImageElement).style.display = "none";
                  }}
                />
              ) : (
                (nickname || user.username).slice(0, 1)
              )}
            </div>
            <div className="flex-1">
              <label className="block text-xs font-medium text-ink-600 mb-1">
                头像图片地址
              </label>
              <input
                type="text"
                value={avatarUrl}
                onChange={(e) => setAvatarUrl(e.target.value)}
                placeholder="粘贴图片 URL（暂不支持本地上传）"
                className="w-full h-9 px-3 rounded-lg border border-ink-200 text-sm focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100"
              />
            </div>
          </div>

          {/* 昵称 */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs font-medium text-ink-600">昵称</label>
              <span className="text-[10px] text-ink-400">{nickname.length}/50</span>
            </div>
            <input
              type="text"
              value={nickname}
              onChange={(e) => setNickname(e.target.value)}
              placeholder="给自己起个昵称"
              className="w-full h-9 px-3 rounded-lg border border-ink-200 text-sm focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100"
            />
          </div>

          {/* 个人简介 */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs font-medium text-ink-600">个人简介</label>
              <span className="text-[10px] text-ink-400">{bio.length}/200</span>
            </div>
            <textarea
              value={bio}
              onChange={(e) => setBio(e.target.value)}
              placeholder="介绍一下自己吧（专业方向、擅长领域……）"
              rows={3}
              className="w-full px-3 py-2 rounded-lg border border-ink-200 text-sm resize-none focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100"
            />
          </div>

          {formError && (
            <div className="text-xs text-danger-600 bg-danger-50 rounded-lg px-3 py-2">
              {formError}
            </div>
          )}
        </div>

        {/* 底部按钮 */}
        <div className="flex items-center justify-end gap-3 px-6 py-4 bg-ink-50 border-t border-ink-100">
          <button
            onClick={onClose}
            disabled={saving}
            className="px-4 h-9 rounded-lg bg-white border border-ink-200 text-ink-600 text-sm font-medium hover:bg-ink-50 transition-colors disabled:opacity-50"
          >
            取消
          </button>
          <button
            onClick={() => void handleSubmit()}
            disabled={saving}
            className="px-5 h-9 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-sm font-medium transition-colors disabled:opacity-50"
          >
            {saving ? "保存中..." : "保存"}
          </button>
        </div>
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  unit,
  isText = false,
}: {
  label: string;
  value: number | string;
  unit: string;
  isText?: boolean;
}) {
  return (
    <div className="text-center">
      <div className="text-2xl font-bold leading-tight">
        {value}
        {!isText && <span className="text-sm font-normal ml-0.5 text-white/80">{unit}</span>}
      </div>
      <div className="text-xs text-white/60 mt-0.5">{label}</div>
    </div>
  );
}

/** 历史趋势折线图 */
function TrendChart({ trend }: { trend: TrendPoint[] }) {
  const width = 500;
  const height = 200;
  const padding = { top: 20, right: 20, bottom: 30, left: 40 };
  const chartW = width - padding.left - padding.right;
  const chartH = height - padding.top - padding.bottom;

  if (trend.length === 0) {
    return <div className="text-center text-ink-400 py-10 text-sm">暂无数据</div>;
  }

  const allScores = trend.flatMap((p) => [p.techScore, p.collabScore ?? 0]);
  const maxScore = Math.max(...allScores, 60);
  const minScore = Math.min(...allScores.filter((s) => s > 0), 0);

  const yMax = Math.ceil(maxScore / 10) * 10 + 10;
  const yMin = Math.max(0, Math.floor(minScore / 10) * 10 - 10);
  const yRange = yMax - yMin || 100;

  const xStep = trend.length > 1 ? chartW / (trend.length - 1) : chartW;

  const techPoints = trend.map((p, i) => {
    const x = padding.left + i * xStep;
    const y = padding.top + chartH - ((p.techScore - yMin) / yRange) * chartH;
    return { x, y, score: p.techScore };
  });

  const collabPoints = trend
    .map((p, i) => {
      if (p.collabScore === undefined || p.collabScore === 0) return null;
      const x = padding.left + i * xStep;
      const y = padding.top + chartH - ((p.collabScore - yMin) / yRange) * chartH;
      return { x, y, score: p.collabScore };
    })
    .filter(Boolean) as { x: number; y: number; score: number }[];

  const techPath = techPoints.map((p, i) => `${i === 0 ? "M" : "L"} ${p.x} ${p.y}`).join(" ");
  const collabPath = collabPoints
    .map((p, i) => `${i === 0 ? "M" : "L"} ${p.x} ${p.y}`)
    .join(" ");

  // Y 轴刻度线
  const yTicks = 4;
  const tickLines = [];
  for (let i = 0; i <= yTicks; i++) {
    const y = padding.top + (chartH / yTicks) * i;
    const val = Math.round(yMax - (yRange / yTicks) * i);
    tickLines.push({ y, val });
  }

  return (
    <div>
      <svg viewBox={`0 0 ${width} ${height}`} className="w-full h-auto">
        {/* 网格线 */}
        {tickLines.map((t, i) => (
          <g key={i}>
            <line
              x1={padding.left}
              y1={t.y}
              x2={width - padding.right}
              y2={t.y}
              stroke="#f1f5f9"
              strokeWidth="1"
            />
            <text
              x={padding.left - 8}
              y={t.y + 4}
              fill="#94a3b8"
              fontSize="10"
              textAnchor="end"
            >
              {t.val}
            </text>
          </g>
        ))}

        {/* 技术分折线 */}
        <path
          d={techPath}
          fill="none"
          stroke="rgb(139, 92, 246)"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        {/* 技术分数据点 */}
        {techPoints.map((p, i) => (
          <circle key={i} cx={p.x} cy={p.y} r="4" fill="white" stroke="rgb(139, 92, 246)" strokeWidth="2" />
        ))}

        {/* 协作分折线 */}
        {collabPoints.length > 1 && (
          <path
            d={collabPath}
            fill="none"
            stroke="rgb(16, 185, 129)"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeDasharray="6,4"
          />
        )}
        {/* 协作分数据点 */}
        {collabPoints.map((p, i) => (
          <circle
            key={i}
            cx={p.x}
            cy={p.y}
            r="3.5"
            fill="white"
            stroke="rgb(16, 185, 129)"
            strokeWidth="2"
          />
        ))}

        {/* X 轴标签 */}
        {trend.map((p, i) => (
          <text
            key={i}
            x={padding.left + i * xStep}
            y={height - padding.bottom + 18}
            fill="#94a3b8"
            fontSize="10"
            textAnchor="middle"
          >
            {p.label}
          </text>
        ))}
      </svg>

      {/* 图例 */}
      <div className="flex items-center justify-center gap-6 mt-2">
        <div className="flex items-center gap-1.5 text-xs text-ink-500">
          <span className="w-6 h-0 border-t-2 border-brand-500" />
          技术分
        </div>
        <div className="flex items-center gap-1.5 text-xs text-ink-500">
          <span className="w-6 h-0 border-t-2 border-dashed border-ok-500" />
          协作分
        </div>
      </div>
    </div>
  );
}

/** 人机对照入口卡片（已解锁） */
function ComparisonEntryCard({ onClick }: { onClick: () => void }) {
  return (
    <div className="h-full flex flex-col">
      <div className="p-5 bg-gradient-to-br from-brand-50 to-ok-50 border-b border-ink-100">
        <div className="text-3xl mb-2">🎯</div>
        <h3 className="text-base font-bold text-ink-900">人机对照报告</h3>
        <p className="text-xs text-ink-500 mt-1">对比纯人工与 AI 协作的表现差异</p>
      </div>
      <div className="p-5 flex-1 flex flex-col">
        <div className="space-y-2 mb-4">
          <div className="flex items-center gap-2 text-xs text-ink-600">
            <span className="text-ok-500">✓</span>
            速度提升对比
          </div>
          <div className="flex items-center gap-2 text-xs text-ink-600">
            <span className="text-ok-500">✓</span>
            正确率对比
          </div>
          <div className="flex items-center gap-2 text-xs text-ink-600">
            <span className="text-ok-500">✓</span>
            危险操作对比
          </div>
          <div className="flex items-center gap-2 text-xs text-ink-600">
            <span className="text-ok-500">✓</span>
            AI 智能分析
          </div>
        </div>
        <button
          onClick={onClick}
          className="mt-auto w-full h-10 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-sm font-semibold transition-colors"
        >
          查看对照报告
        </button>
      </div>
    </div>
  );
}

/** 人机对照入口卡片（未解锁） */
function ComparisonLockedCard({ totalSessions }: { totalSessions: number }) {
  // 至少需要 1 次人工 + 1 次 AI 协作 = 2 次
  const target = 2;
  const progress = Math.min(totalSessions, target);
  const percent = Math.round((progress / target) * 100);

  return (
    <div className="h-full flex flex-col">
      <div className="p-5 bg-ink-50 border-b border-ink-100">
        <div className="text-3xl mb-2">🔒</div>
        <h3 className="text-base font-bold text-ink-900">人机对照报告</h3>
        <p className="text-xs text-ink-500 mt-1">完成两种模式训练后解锁</p>
      </div>
      <div className="p-5 flex-1 flex flex-col">
        <div className="mb-4">
          <div className="flex justify-between text-xs text-ink-500 mb-2">
            <span>解锁进度</span>
            <span className="font-semibold text-ink-700">{progress} / {target}</span>
          </div>
          <div className="h-2 bg-ink-100 rounded-full overflow-hidden">
            <div
              className="h-full bg-brand-400 rounded-full transition-all"
              style={{ width: `${percent}%` }}
            />
          </div>
        </div>
        <div className="space-y-2 text-xs text-ink-500">
          <div className="flex items-center gap-2">
            <span>🧑‍💻</span>
            1 次纯人工训练
          </div>
          <div className="flex items-center gap-2">
            <span>🤖</span>
            1 次 AI 协作训练
          </div>
        </div>
        <button
          disabled
          className="mt-auto w-full h-10 rounded-xl bg-ink-100 text-ink-400 text-sm font-medium cursor-not-allowed"
        >
          未解锁
        </button>
      </div>
    </div>
  );
}

/** 历史记录行 */
function HistoryRow({
  item,
  onReview,
  onProgress,
}: {
  item: TrainingHistoryItem;
  onReview: (reviewId: number) => void;
  onProgress: (sessionId: number) => void;
}) {
  const reviewState = trainingReviewState(item);
  const canOpenReview = reviewState === "available";
  const canOpenProgress = reviewState === "pending" || reviewState === "unavailable";
  // 未结束/未评分时 success 与 techScore 为 undefined
  const statusText =
    item.success === undefined ? "未评分" : item.success ? "修复成功" : "未修复";
  const statusClass =
    item.success === undefined
      ? "text-ink-400"
      : item.success
        ? "text-ok-600"
        : "text-danger-500";

  return (
    <div className="flex items-center gap-3 p-3 rounded-xl hover:bg-ink-50 transition-colors">
      {/* 左侧：模式图标 */}
      <div
        className={`w-10 h-10 rounded-xl flex items-center justify-center text-lg flex-shrink-0 ${
          item.mode === "ai_collab" ? "bg-brand-50" : "bg-ink-100"
        }`}
      >
        {modeIcon(item.mode)}
      </div>

      {/* 中间：项目 + 故障 */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-ink-800 truncate">
            {item.defectName}
          </span>
          <span
            className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${difficultyColor(
              item.difficulty,
            )}`}
          >
            {formatDifficulty(item.difficulty)}
          </span>
        </div>
        <div className="flex items-center gap-2 mt-0.5 text-xs text-ink-400">
          <span>{item.projectName}</span>
          <span>·</span>
          <span>{formatMode(item.mode)}</span>
          {item.startTime && (
            <>
              <span>·</span>
              <span>{item.startTime.slice(5, 16)}</span>
            </>
          )}
        </div>
      </div>

      {/* 右侧：得分 + 状态 */}
      <div className="text-right flex-shrink-0">
        <div className="flex items-center gap-1">
          <span
            className={`text-base font-bold ${
              item.techScore === undefined ? "text-ink-300" : "text-brand-600"
            }`}
          >
            {item.techScore ?? "—"}
          </span>
          <span className="text-[10px] text-ink-400">分</span>
        </div>
        <div className={`text-[11px] ${statusClass}`}>{statusText}</div>
      </div>

      {/* 已结束的班次在报告就绪前可先查看后台进度。 */}
      <button
        disabled={!canOpenReview && !canOpenProgress}
        onClick={() => canOpenReview && item.reviewId !== undefined ? onReview(item.reviewId) : onProgress(item.id)}
        className={`text-xs font-medium flex-shrink-0 ml-2 ${
          canOpenReview || canOpenProgress
            ? "text-brand-600 hover:text-brand-700"
            : "text-ink-300 cursor-not-allowed"
        }`}
      >
        {canOpenProgress ? "复盘进度 →" : "复盘 →"}
      </button>
    </div>
  );
}
