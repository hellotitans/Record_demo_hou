import { motion } from 'framer-motion'
import type { Side } from '../types'
import { SIDE_LABEL } from '../types'

const palette: Record<Side, { from: string; to: string; glow: string }> = {
  data: { from: '#60a5fa', to: '#1e3a8a', glow: 'rgba(59,130,246,0.45)' },
  life: { from: '#fb923c', to: '#7c2d12', glow: 'rgba(249,115,22,0.45)' },
}

// 占位立绘：纯 SVG/CSS 画的风格化头像，零素材依赖，后续可一键替换成真图。
// speaking 时做一个轻微呼吸动效 + 光环，对应"谁正在发言"。
export function Avatar({
  side,
  speaking = false,
  size = 56,
}: {
  side: Side
  speaking?: boolean
  size?: number
}) {
  const p = palette[side]
  return (
    <motion.div
      className="relative flex select-none items-center justify-center rounded-full font-bold text-white"
      style={{
        width: size,
        height: size,
        background: `linear-gradient(135deg, ${p.from}, ${p.to})`,
        boxShadow: speaking ? `0 0 0 4px ${p.glow}` : 'none',
      }}
      animate={speaking ? { scale: [1, 1.07, 1] } : { scale: 1 }}
      transition={speaking ? { repeat: Infinity, duration: 1.1, ease: 'easeInOut' } : { duration: 0.2 }}
      aria-label={SIDE_LABEL[side]}
      title={SIDE_LABEL[side]}
    >
      <span style={{ fontSize: size * 0.36 }}>{side === 'data' ? '数' : '生'}</span>
      {speaking && (
        <span
          className="absolute -bottom-1 rounded-full bg-white px-1.5 py-0.5 text-[10px] font-medium"
          style={{ color: p.to }}
        >
          发言中
        </span>
      )}
    </motion.div>
  )
}
