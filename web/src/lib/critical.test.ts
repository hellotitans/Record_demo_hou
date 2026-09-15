import { describe, it, expect } from 'vitest'
import type { Assumption } from '../types'
import {
  evalAssumption,
  sideHolds,
  computeWinner,
  groupByVariable,
  groupRange,
  flipHint,
  defaultValues,
} from './critical'

// 测试夹具构造器：未给字段用合理默认。
const mk = (over: Partial<Assumption>): Assumption => ({
  id: 'x',
  side: 'data',
  statement: 's',
  variable: 'v',
  operator: '>=',
  value: 3,
  unit: '%',
  ...over,
})

describe('evalAssumption', () => {
  it('>= 在阈值及之上成立', () => {
    const a = mk({ operator: '>=', value: 3 })
    expect(evalAssumption(a, 3)).toBe(true)
    expect(evalAssumption(a, 4)).toBe(true)
    expect(evalAssumption(a, 2.9)).toBe(false)
  })
  it('> 严格大于', () => {
    const a = mk({ operator: '>', value: 3 })
    expect(evalAssumption(a, 3)).toBe(false)
    expect(evalAssumption(a, 3.1)).toBe(true)
  })
  it('<= 与 <', () => {
    expect(evalAssumption(mk({ operator: '<=', value: 40 }), 40)).toBe(true)
    expect(evalAssumption(mk({ operator: '<=', value: 40 }), 41)).toBe(false)
    expect(evalAssumption(mk({ operator: '<', value: 40 }), 40)).toBe(false)
    expect(evalAssumption(mk({ operator: '<', value: 40 }), 39)).toBe(true)
  })
  it('未知算子视为成立，避免脏数据误翻转', () => {
    expect(evalAssumption(mk({ operator: '~', value: 3 }), 0)).toBe(true)
  })
})

describe('sideHolds', () => {
  it('该方无假设时返回 false', () => {
    expect(sideHolds([mk({ side: 'life' })], {}, 'data')).toBe(false)
  })
  it('仅当该方全部假设成立才为 true', () => {
    const as = [
      mk({ side: 'data', variable: 'x', operator: '>=', value: 3 }),
      mk({ id: '2', side: 'data', variable: 'y', operator: '<=', value: 10 }),
    ]
    expect(sideHolds(as, { x: 3, y: 10 }, 'data')).toBe(true)
    expect(sideHolds(as, { x: 2, y: 10 }, 'data')).toBe(false)
  })
})

describe('computeWinner', () => {
  const as = [
    mk({ side: 'data', variable: '涨幅', operator: '>=', value: 3 }),
    mk({ id: 'l', side: 'life', variable: '月供', operator: '<=', value: 40 }),
  ]
  it('两边都站在假设线上 → 平局', () => {
    expect(computeWinner(as, { 涨幅: 3, 月供: 40 }).winner).toBe('tie')
  })
  it('数据派破 → 生活派胜', () => {
    const r = computeWinner(as, { 涨幅: 2, 月供: 40 })
    expect(r.winner).toBe('life')
    expect(r.dataHolds).toBe(false)
  })
  it('生活派破 → 数据派胜', () => {
    expect(computeWinner(as, { 涨幅: 3, 月供: 50 }).winner).toBe('data')
  })
  it('双方皆破 → both-broken', () => {
    expect(computeWinner(as, { 涨幅: 1, 月供: 99 }).winner).toBe('both-broken')
  })
  it('同变量共享：越过中界即翻转', () => {
    const shared = [
      mk({ side: 'data', variable: 'p', operator: '>=', value: 3 }),
      mk({ id: 'l', side: 'life', variable: 'p', operator: '<=', value: 3 }),
    ]
    expect(computeWinner(shared, { p: 3 }).winner).toBe('tie')
    expect(computeWinner(shared, { p: 4 }).winner).toBe('data')
    expect(computeWinner(shared, { p: 2 }).winner).toBe('life')
  })
})

describe('groupByVariable / groupRange', () => {
  it('同变量假设归并到一组', () => {
    const as = [mk({ variable: 'p', side: 'data' }), mk({ id: 'l', variable: 'p', side: 'life' })]
    const g = groupByVariable(as)
    expect(g).toHaveLength(1)
    expect(g[0].assumptions).toHaveLength(2)
  })
  it('单变量范围跨越并包含阈值', () => {
    const r = groupRange({ variable: '涨幅', assumptions: [mk({ variable: '涨幅', value: 3 })] })
    expect(r.min).toBeLessThan(3)
    expect(r.max).toBeGreaterThan(3)
    expect(r.step).toBeGreaterThan(0)
  })
  it('共享变量范围覆盖两个阈值', () => {
    const r = groupRange({
      variable: 'p',
      assumptions: [mk({ variable: 'p', value: 3 }), mk({ variable: 'p', value: 8 })],
    })
    expect(r.min).toBeLessThanOrEqual(3)
    expect(r.max).toBeGreaterThanOrEqual(8)
  })
})

describe('flipHint', () => {
  it('朝正确方向措辞', () => {
    expect(flipHint(mk({ operator: '>=', value: 3, unit: '%' }))).toContain('低于 3%')
    expect(flipHint(mk({ operator: '<=', value: 40, unit: '%' }))).toContain('高于 40%')
    expect(flipHint(mk({ operator: '>', value: 3, unit: '%' }))).toContain('≤ 3%')
    expect(flipHint(mk({ operator: '<', value: 40, unit: '%' }))).toContain('≥ 40%')
  })
})

describe('defaultValues', () => {
  it('单变量取阈值、同变量取中界', () => {
    expect(defaultValues([mk({ variable: '涨幅', value: 3 })])['涨幅']).toBe(3)
    const shared = [
      mk({ variable: 'p', value: 3 }),
      mk({ id: 'l', variable: 'p', value: 8 }),
    ]
    expect(defaultValues(shared)['p']).toBe(5.5)
  })
})
