// P8 后悔率看板：把 P7 存下来的回访数据变成一眼能读懂的"决策质量"视图。
//
// 两块刻意的设计取舍：
//  1. **比率旁边永远挂着样本量**（"后悔 1/2"而不是孤零零的"50%"）。
//     个人工具的数据量天然很小，1/1 = 100% 和 40/40 = 100% 完全不是一回事；
//     只给百分比会让人对噪声过度反应。
//  2. **条形图用纯 CSS 宽度，不引图表库**。
//     这里最多十来个数据点，图表库带来的是体积和依赖，不是可读性。

import { findTemplate } from '../lib/templates'
import type { DecisionStats } from '../types'

function pct(rate: number): string {
  return Math.round(rate * 100) + '%'
}

/** 品类 id → 中文名（带 emoji）；空串是"未分类"，未知 id 原样显示。 */
function categoryLabel(id: string): string {
  if (!id) return '未分类'
  const t = findTemplate(id)
  return t ? `${t.emoji} ${t.name}` : id
}

function ymd(iso: string | undefined): string {
  if (!iso) return '—'
  return Number.isNaN(new Date(iso).getTime()) ? '—' : iso.slice(0, 10)
}

function Card({ value, label }: { value: string; label: string }) {
  return (
    <div className="rounded-2xl bg-slate-50 px-3 py-3 text-center">
      <div className="text-xl font-semibold text-slate-800">{value}</div>
      <div className="mt-0.5 text-xs text-slate-500">{label}</div>
    </div>
  )
}

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="mt-4">
      <div className="mb-2 flex items-baseline justify-between">
        <h3 className="text-sm font-semibold text-slate-700">{title}</h3>
        {hint && <span className="text-xs text-slate-400">{hint}</span>}
      </div>
      {children}
    </div>
  )
}

export function StatsBoard({ stats }: { stats: DecisionStats | null }) {
  // 后端不可用或还没攒下任何决策时整块不渲染。
  // 看板是锦上添花：它缺席不该让首页看起来像坏了。
  if (!stats || stats.total === 0) return null

  const maxMonthTotal = Math.max(1, ...stats.byMonth.map((m) => m.total))

  return (
    <section className="rounded-3xl border border-slate-200 bg-white/70 p-5 shadow-sm">
      <h2 className="text-sm font-semibold text-slate-700">决策复盘</h2>
      <p className="mt-0.5 text-xs text-slate-400">后悔率只统计已经回访过的决策</p>

      <div className="mt-3 grid grid-cols-4 gap-2">
        <Card value={String(stats.total)} label="总决策" />
        <Card value={String(stats.answered)} label="已回访" />
        <Card value={pct(stats.regretRate)} label="后悔率" />
        <Card value={String(stats.pending)} label="待回访" />
      </div>

      {stats.byCategory.length > 0 && (
        <Section title="按品类后悔率" hint="后悔 / 已回访">
          <div className="space-y-2">
            {stats.byCategory.map((c) => (
              <div key={c.category}>
                <div className="flex items-baseline justify-between text-xs text-slate-600">
                  <span className="truncate">{categoryLabel(c.category)}</span>
                  <span className="ml-2 shrink-0 tabular-nums text-slate-500">
                    {c.regretYes}/{c.answered}
                    <span className="ml-1 text-slate-400">（{c.total} 条）</span>
                  </span>
                </div>
                <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-slate-100">
                  <div
                    className="h-full rounded-full bg-rose-400"
                    style={{ width: `${Math.round(c.regretRate * 100)}%` }}
                  />
                </div>
              </div>
            ))}
          </div>
        </Section>
      )}

      {stats.byMonth.length > 0 && (
        <Section title="月度决策与后悔趋势" hint="按月 · 后悔率">
          <div className="space-y-2">
            {stats.byMonth.map((m) => (
              <div key={m.month} className="flex items-center gap-2">
                <span className="w-16 shrink-0 text-xs tabular-nums text-slate-500">
                  {m.month || '未知'}
                </span>
                <div className="h-2 flex-1 overflow-hidden rounded-full bg-slate-100">
                  {/* 条形长度表示当月决策量，底色里的深色部分表示其中的后悔比例。 */}
                  <div
                    className="h-full rounded-full bg-slate-300"
                    style={{ width: `${Math.round((m.total / maxMonthTotal) * 100)}%` }}
                  >
                    <div
                      className="h-full rounded-full bg-rose-400"
                      style={{ width: `${Math.round(m.regretRate * 100)}%` }}
                    />
                  </div>
                </div>
                <span className="w-20 shrink-0 text-right text-xs tabular-nums text-slate-500">
                  {m.total} 条 · {pct(m.regretRate)}
                </span>
              </div>
            ))}
          </div>
        </Section>
      )}

      {stats.regretted.length > 0 && (
        <Section title="最后悔的决策" hint={`${stats.regretted.length} 条`}>
          <ul className="space-y-1.5">
            {stats.regretted.map((r) => (
              <li key={r.id} className="rounded-xl bg-rose-50/60 px-3 py-2">
                <div className="truncate text-xs text-slate-700">{r.question}</div>
                <div className="mt-0.5 text-[11px] text-slate-400">
                  {ymd(r.createdAt)} · {categoryLabel(r.category ?? '')}
                </div>
              </li>
            ))}
          </ul>
        </Section>
      )}
    </section>
  )
}
