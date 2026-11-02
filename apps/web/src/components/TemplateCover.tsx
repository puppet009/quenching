import { useState } from "react";
import { domainGroupOf, domainLabel, type Template } from "@quenching/shared";

/** One template-owned cover. Older templates and failed images keep a usable fallback. */
export function TemplateCover({ template, className = "", compact = false }: {
  template: Template;
  className?: string;
  compact?: boolean;
}) {
  const [failedURL, setFailedURL] = useState("");
  const cover = template.cover;
  const group = domainGroupOf(template.domain);
  if (!cover || failedURL === cover.url) {
    return (
      <div className={`flex items-center justify-center bg-gradient-to-br ${group?.gradient ?? "from-brand-500 to-brand-400"} ${className}`}>
        <span className="text-6xl font-bold text-white/30" aria-hidden="true">{domainLabel(template.domain).charAt(0)}</span>
      </div>
    );
  }
  return (
    <div className={`relative overflow-hidden bg-[#eef2f1] ${className}`}>
      <div className="absolute inset-0 bg-gradient-to-br from-white/70 via-transparent to-ink-100/60" aria-hidden="true" />
      <img
        src={cover.url}
        alt={cover.alt}
        width={cover.width}
        height={cover.height}
        decoding="async"
        loading={compact ? "lazy" : "eager"}
        onError={() => setFailedURL(cover.url)}
        className={compact
          ? "relative mx-auto mt-4 w-[150px] rounded-xl border-4 border-white shadow-xl"
          : "relative mx-auto h-full w-auto max-w-full object-contain p-3"}
      />
    </div>
  );
}
