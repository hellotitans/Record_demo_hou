import { CATEGORY_TEMPLATES } from '../lib/templates'
import type { CategoryTemplate } from '../types'

interface CategoryPickerProps {
  selectedId: string | null
  onSelect: (t: CategoryTemplate | null) => void
}

export function CategoryPicker({ selectedId, onSelect }: CategoryPickerProps) {
  return (
    <div>
      <div className="mb-2 text-xs font-semibold text-slate-500">挑个品类，自动带上常见决策维度：</div>
      <div className="flex flex-wrap gap-2">
        {CATEGORY_TEMPLATES.map((t) => {
          const active = t.id === selectedId
          return (
            <button
              key={t.id}
              type="button"
              aria-label={`品类 ${t.name}`}
              aria-pressed={active}
              onClick={() => onSelect(t)}
              className={
                'rounded-full border px-3 py-1.5 text-sm transition ' +
                (active
                  ? 'border-blue-500 bg-blue-50 text-blue-700'
                  : 'border-slate-200 bg-white/70 text-slate-600 hover:border-slate-300')
              }
            >
              <span aria-hidden className="mr-1">
                {t.emoji}
              </span>
              {t.name}
            </button>
          )
        })}
        <button
          type="button"
          aria-label="品类 自定义"
          aria-pressed={selectedId === null && false}
          onClick={() => onSelect(null)}
          className={
            'rounded-full border px-3 py-1.5 text-sm transition ' +
            (selectedId === null
              ? 'border-slate-400 bg-slate-100 text-slate-700'
              : 'border-dashed border-slate-200 bg-white/70 text-slate-400 hover:border-slate-300')
          }
        >
          自定义
        </button>
      </div>
    </div>
  )
}
