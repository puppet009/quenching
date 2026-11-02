import { DualDimensionRadarPanel } from "../components/DualDimensionRadarPanel";
import { SkillRadar } from "../components/SkillRadar";
import {
  buildTechRadar,
  buildCollabRadar,
  calcCollabLevel,
  radarAverage,
  type DualDimensionRadar,
} from "../services/radar";

/**
 * 能力雷达组件演示页
 *
 * 展示 SkillRadar 基础组件 + DualDimensionRadarPanel 面板组件
 * 包含多种尺寸和场景的示例
 */
export function RadarDemoPage() {
  // ========== 模拟数据 ==========
  const currentTech = buildTechRadar({
    fault_diagnosis: 78,
    tool_proficiency: 65,
    info_retrieval: 82,
    operation_cost: 70,
    verification: 55,
    time_efficiency: 72,
  });

  const avgTech = buildTechRadar({
    fault_diagnosis: 65,
    tool_proficiency: 58,
    info_retrieval: 70,
    operation_cost: 62,
    verification: 50,
    time_efficiency: 60,
  });

  const currentCollab = buildCollabRadar({
    question_quality: 85,
    info_utilization: 72,
    judgment: 60,
    pitfall_awareness: 45,
    verification_habit: 50,
    independence: 58,
  });

  const avgCollab = buildCollabRadar({
    question_quality: 70,
    info_utilization: 60,
    judgment: 50,
    pitfall_awareness: 35,
    verification_habit: 42,
    independence: 48,
  });

  const techOverall = radarAverage(currentTech);
  const collabOverall = radarAverage(currentCollab);
  const overall = Math.round((techOverall + collabOverall) / 2);
  const level = calcCollabLevel(collabOverall);

  const demoData: DualDimensionRadar = {
    currentTech,
    currentCollab,
    avgTech,
    avgCollab,
    techOverall,
    collabOverall,
    hasCollab: true,
    overall,
    level,
  };

  // 低分段示例
  const beginnerTech = buildTechRadar({
    fault_diagnosis: 35,
    tool_proficiency: 28,
    info_retrieval: 40,
    operation_cost: 45,
    verification: 20,
    time_efficiency: 30,
  });

  return (
    <div className="min-h-screen bg-ink-50 py-10 px-4">
      <div className="page-container">
        {/* 标题 */}
        <div className="text-center mb-10">
          <h1 className="text-2xl font-bold text-ink-900 mb-2">双维能力雷达组件演示</h1>
          <p className="text-ink-500 text-[14px]">
            SkillRadar 基础雷达图 + DualDimensionRadarPanel 双维面板
          </p>
        </div>

        {/* 双维面板（主组件） */}
        <section className="mb-10">
          <h2 className="text-lg font-bold text-ink-800 mb-4">双维能力雷达面板</h2>
          <DualDimensionRadarPanel data={demoData} />
        </section>

        {/* 基础雷达图 - 各种尺寸 */}
        <section className="mb-10">
          <h2 className="text-lg font-bold text-ink-800 mb-4">基础雷达图 - SkillRadar</h2>
          <div className="bg-white rounded-2xl border border-ink-100 p-6">
            <div className="flex flex-wrap justify-around items-end gap-6">
              <div className="text-center">
                <SkillRadar data={currentTech} color="139, 92, 246" size={200} levels={4} />
                <div className="text-[12px] text-ink-500 mt-2">小尺寸 200px</div>
              </div>
              <div className="text-center">
                <SkillRadar
                  data={currentTech}
                  compareData={avgTech}
                  color="139, 92, 246"
                  compareColor="148, 163, 184"
                  size={280}
                />
                <div className="text-[12px] text-ink-500 mt-2">中尺寸 280px + 对比</div>
              </div>
              <div className="text-center">
                <SkillRadar
                  data={currentCollab}
                  compareData={avgCollab}
                  color="16, 185, 129"
                  compareColor="148, 163, 184"
                  size={280}
                  levels={5}
                />
                <div className="text-[12px] text-ink-500 mt-2">协作维 + 对比</div>
              </div>
            </div>
          </div>
        </section>

        {/* 新手 vs 高手对比 */}
        <section className="mb-10">
          <h2 className="text-lg font-bold text-ink-800 mb-4">不同水平对比</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div className="bg-white rounded-2xl border border-ink-100 p-5 text-center">
              <div className="text-[14px] font-semibold text-ink-700 mb-3">新手水平</div>
              <div className="flex justify-center">
                <SkillRadar data={beginnerTech} color="148, 163, 184" size={240} levels={4} />
              </div>
              <div className="text-[13px] text-ink-500 mt-2">
                技术分 <span className="font-semibold">{radarAverage(beginnerTech)}</span>
              </div>
            </div>
            <div className="bg-white rounded-2xl border border-ink-100 p-5 text-center">
              <div className="text-[14px] font-semibold text-ink-700 mb-3">进阶水平</div>
              <div className="flex justify-center">
                <SkillRadar
                  data={currentTech}
                  compareData={beginnerTech}
                  color="139, 92, 246"
                  compareColor="148, 163, 184"
                  size={240}
                  levels={4}
                />
              </div>
              <div className="text-[13px] text-ink-500 mt-2">
                技术分 <span className="font-semibold text-brand-600">{techOverall}</span>
                {" · 对比 +"}
                {techOverall - radarAverage(beginnerTech)} 分
              </div>
            </div>
          </div>
        </section>

        {/* 小尺寸面板 */}
        <section className="mb-10">
          <h2 className="text-lg font-bold text-ink-800 mb-4">小尺寸面板（sm）</h2>
          <div className="max-w-lg">
            <DualDimensionRadarPanel data={demoData} size="sm" />
          </div>
        </section>
      </div>
    </div>
  );
}
