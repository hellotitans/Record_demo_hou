import { describe, it, expect } from 'vitest'
import { CATEGORY_TEMPLATES, findTemplate } from './templates'
import type { CategoryTemplate } from '../types'

describe('品类模板库数据完整性', () => {
  it('品类数量落在 6–10 之间', () => {
    expect(CATEGORY_TEMPLATES.length).toBeGreaterThanOrEqual(6)
    expect(CATEGORY_TEMPLATES.length).toBeLessThanOrEqual(10)
  })

  it('每个品类 id 全局唯一', () => {
    const ids = CATEGORY_TEMPLATES.map((t) => t.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  CATEGORY_TEMPLATES.forEach((t: CategoryTemplate) => {
    it(`「${t.name}」结构合法`, () => {
      // 两个样例选项都不为空
      expect(t.sampleOptions.a.trim()).toBeTruthy()
      expect(t.sampleOptions.b.trim()).toBeTruthy()
      expect(t.sampleOptions.a).not.toEqual(t.sampleOptions.b)

      // 至少 2 个决策维度
      expect(t.dimensions.length).toBeGreaterThanOrEqual(2)

      // 维度 key 在同模板内唯一
      const keys = t.dimensions.map((d) => d.key)
      expect(new Set(keys).size).toBe(keys.length)

      // 权重必须在 (0,1]，且来源合法
      t.dimensions.forEach((d) => {
        expect(d.weight).toBeGreaterThan(0)
        expect(d.weight).toBeLessThanOrEqual(1)
        expect(['template', 'model', 'user']).toContain(d.source)
        expect(d.label.trim()).toBeTruthy()
      })
    })
  })

  it('findTemplate 按 id 命中、未命中返回 undefined', () => {
    expect(findTemplate('phone')?.name).toBe('数码产品购买')
    expect(findTemplate('__nope__')).toBeUndefined()
  })
})
