import {useEffect, useState} from "react";
import {Link} from "react-router-dom";
import {DOMAIN_LABELS, DIFFICULTY_LABELS} from "@quenching/shared";
import {templates, errorMessage, type Template} from "../services/data";

export function OfficialTemplatesPanel({refresh}: {refresh: number}) {
  const [items, setItems] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [keyword, setKeyword] = useState("");
  const [retry, setRetry] = useState(0);

  useEffect(() => {
    let live = true;
    setLoading(true);
    setError("");
    templates()
      .then(data => {if (live) setItems(data ?? []);})
      .catch(err => {if (live) {setItems([]); setError(errorMessage(err));}})
      .finally(() => {if (live) setLoading(false);});
    return () => {live = false;};
  }, [refresh, retry]);

  const query = keyword.trim().toLowerCase();
  const visible = items.filter(item => [item.name, item.slug, ...(item.tech_stack ?? [])].join(" ").toLowerCase().includes(query));

  return (
    <section aria-label="官方模板" className="bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden mb-6">
      <div className="px-5 py-4 border-b border-ink-100">
        <h2 className="font-semibold text-ink-900">官方模板</h2>
        <p className="text-xs text-ink-500 mt-1">查看平台接入的真实项目，按模板维护故障配置。</p>
        <input
          aria-label="搜索官方模板"
          placeholder="搜索模板名称或技术栈"
          value={keyword}
          onChange={event => setKeyword(event.target.value)}
          className="mt-3 w-full h-10 px-3 rounded-xl border border-ink-200 text-sm focus:outline-none focus:border-brand-400"
        />
      </div>
      {loading ? <div className="p-6 text-ink-400 text-sm">加载官方模板…</div> : error ? (
        <div role="alert" className="p-5 text-danger-600 text-sm">
          官方模板加载失败：{error}
          <button className="ml-3 underline" onClick={() => setRetry(value => value + 1)}>重试</button>
        </div>
      ) : visible.length === 0 ? (
        <div className="p-6 text-ink-500 text-sm">{query ? "没有匹配的官方模板" : "暂无官方模板"}</div>
      ) : (
        <div className="divide-y divide-ink-100">
          {visible.map(item => (
            <div key={item.slug} className="p-5 flex flex-col sm:flex-row sm:items-center gap-4">
              <div className="flex-1 min-w-0">
                <div className="font-semibold text-ink-800">{item.name}</div>
                <div className="text-xs text-ink-500 mt-1">{item.slug} · {DOMAIN_LABELS[item.domain as keyof typeof DOMAIN_LABELS] ?? item.domain} · {item.difficulty ? DIFFICULTY_LABELS[item.difficulty] : "未设难度"} · {item.services.length} 个服务</div>
                <p className="text-xs text-ink-500 mt-2">{item.description}</p>
                <div className="flex flex-wrap gap-1 mt-2">
                  {(item.tech_stack ?? []).map(tech => <span key={tech} className="px-2 py-1 rounded bg-ink-50 text-xs text-ink-600">{tech}</span>)}
                </div>
              </div>
              <Link
                to={`/admin/defects?template=${encodeURIComponent(item.slug)}`}
                className="self-start sm:self-auto px-3 py-2 rounded-lg text-sm text-brand-600 bg-brand-50 hover:bg-brand-100"
              >查看故障</Link>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
