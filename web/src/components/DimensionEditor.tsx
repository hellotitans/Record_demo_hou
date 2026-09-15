import { useState } from 'react'
import type { Dimension } from '../types'

interface DimensionEditorProps {
  dimensions: Dimension[]
  onChange: (next: Dimension[]) => void
}

// 新增维度的自增 key，保证同列表内唯一（测试里连续添加也不会撞 key）。
let customSeq = 0
function nextCustomKey(): string {
  customSeq += 1
  return `custom-${customSeq}`
}

export function DimensionEditor({ dimensions, onChange }: DimensionEditorProps) {
  const [draftLabel, setDraftLabel] = useState('')

  function updateWeight(key: string, weight: number) {
    onChange(dimensions.map((d) => (d.key === key ? { ...d, weight } : d)))
  }

  function remove(key: string) {
    onChange(dimensions.filter((d) => d.key !== key))
  }

  function add() {
    const label = draftLabel.trim() || '自定义维度'
    onChange([
      ...dimensions,
      { key: nextCustomKey(), label, weight: 0.2, source: 'user' },
    ])
    setDraftLabel('')
  }

  if (dimensions.length === 0) {
    return (
      <div className="rounded-2xl border border-dashed border-slate-200 bg-white/50 p-4 text-center text-xs text-slate-400">
        还没有决策维度。选个品类会自动带上预设，也可以自己加。
      </div>
    )
  }

  return (
    <div className="space-y-3 rounded-2xl border border-slate-200 bg-white/70 p-4">
      <div className="text-xs font-semibold text-slate-500">
        决策维度（拖动权重，影响两派论证重点）
      </div>
      {dimensions.map((d) => (
        <div key={d.key} className="flex items-center gap-3" data-testid="dim-row">
          <input
            aria-label={`维度名 ${d.key}`}
            className="w-28 shrink-0 rounded-lg border border-slate-200 px-2 py-1 text-sm outline-none focus:border-blue-400"
            value={d.label}
            onChange={(e) => onChange(dimensions.map((x) => (x.key === d.key ? { ...x, label: e.target.value } : x)))}
          />
          <input
            type="range"
            min={0}
            max={100}
            step={5}
            value={Math.round(d.weight * 100)}
            aria-label={`权重 ${d.label}`}
            className="flex-1 accent-blue-500"
            onChange={(e) => updateWeight(d.key, Number(e.target.value) / 100)}
          />
          <span className="w-10 text-right text-xs tabular-nums text-slate-600" data-testid="dim-pct">
            {Math.round(d.weight * 100)}%
          </span>
          <button
            type="button"
            aria-label={`删除维度 ${d.label}`}
            onClick={() => remove(d.key)}
            className="rounded-lg px-2 py-1 text-xs text-rose-500 hover:bg-rose-50"
          >
            删
          </button>
        </div>
      ))}
      <div className="flex items-center gap-2 pt-1">
        <input
          aria-label="新维度名"
          placeholder="加一条维度，如：通勤时间"
          className="flex-1 rounded-lg border border-slate-200 px-2 py-1 text-sm outline-none focus:border-blue-400"
          value={draftLabel}
          onChange={(e) => setDraftLabel(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') add()
          }}
        />
        <button
          type="button"
          onClick={add}
          className="rounded-lg border border-slate-300 px-3 py-1 text-sm text-slate-600 hover:bg-slate-100"
        >
          添加维度
        </button>
      </div>
    </div>
  )
}
