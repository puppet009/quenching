// 品牌 Logo（方案B）：橙色「淬」图标 + 中文「淬火」+ 小号英文 QUENCHING。
// size 控制整体大小；英文在窄场景可隐藏。
export function Logo({ size = "md", showEn = true }: { size?: "sm" | "md" | "lg"; showEn?: boolean }) {
  const box = size === "sm" ? "w-7 h-7 text-sm rounded-lg" : size === "lg" ? "w-10 h-10 text-lg rounded-xl" : "w-8 h-8 text-base rounded-lg";
  const cn = size === "sm" ? "text-[15px]" : size === "lg" ? "text-xl" : "text-base";
  return (
    <div className="flex items-center gap-2.5 select-none">
      <span className={`${box} bg-brand-500 text-white font-bold flex items-center justify-center`}>淬</span>
      <span className="flex flex-col leading-none">
        <span className={`${cn} font-bold text-ink-800`}>淬火</span>
        {showEn && (
          <span className="text-[9px] tracking-[0.18em] text-ink-400 font-medium mt-0.5">QUENCHING</span>
        )}
      </span>
    </div>
  );
}
