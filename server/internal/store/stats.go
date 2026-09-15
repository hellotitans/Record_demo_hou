package store

import (
	"sort"
	"time"
)

// FollowupDueDays 满多少天该回访。
// 与前端 isDueForFollowup 的 3 个月（按 30 天/月 = 90 天）取同一个口径，
// 否则前后端会对"这条到底到期没有"给出不同答案，看板上的待回访数就对不上。
const FollowupDueDays = 90

// Stats 决策档案的聚合视图（P8 后悔率看板的数据源）。
type Stats struct {
	Total      int              `json:"total"`
	Answered   int              `json:"answered"`  // 已回访数
	Pending    int              `json:"pending"`   // 到期但还没回访的数
	RegretYes  int              `json:"regretYes"` // 回访中说后悔的
	RegretNo   int              `json:"regretNo"`  // 回访中说不后悔的
	RegretRate float64          `json:"regretRate"`
	ByCategory []CategoryStat   `json:"byCategory"`
	ByMonth    []MonthStat      `json:"byMonth"`
	Regretted  []DecisionRecord `json:"regretted"`
}

// CategoryStat 单个品类的后悔情况。Category 为空串表示"未分类"。
type CategoryStat struct {
	Category   string  `json:"category"`
	Total      int     `json:"total"`
	Answered   int     `json:"answered"`
	RegretYes  int     `json:"regretYes"`
	RegretRate float64 `json:"regretRate"`
}

// MonthStat 单个月份（YYYY-MM）的决策与后悔情况。Month 为空串表示时间未知。
type MonthStat struct {
	Month      string  `json:"month"`
	Total      int     `json:"total"`
	Answered   int     `json:"answered"`
	RegretYes  int     `json:"regretYes"`
	RegretRate float64 `json:"regretRate"`
}

// ComputeStats 从记录切片算出聚合视图。
//
// 刻意做成不碰锁的纯函数：单测可以直接喂一组记录断言结果，
// 不必先造文件、起 FileStore，也不受持久化代码影响。
//
// 三个容易被做错、这里明确下来的语义：
//  1. **后悔率的分母是"已回访数"，不是"总决策数"**。没回答过的人不能被当成"不后悔"，
//     否则档案越攒越多，后悔率会被慢慢稀释成一个好看但没有意义的数字。
//  2. **小样本的品类照样进排行**。1/1 = 100% 看着吓人，但把它藏起来才是误导；
//     所以每条都带 Answered，让前端把样本量摆在比率旁边，由用户自己判断。
//  3. **时间解析失败的记录归入"未知"桶（空串）而不是丢弃**，
//     这样各分项之和始终等于 Total，看板上不会出现"总数对不上"的疑惑。
func ComputeStats(items []DecisionRecord, now time.Time) Stats {
	st := Stats{
		ByCategory: []CategoryStat{},
		ByMonth:    []MonthStat{},
		Regretted:  []DecisionRecord{},
	}

	byCat := make(map[string]*CategoryStat)
	byMon := make(map[string]*MonthStat)

	for _, it := range items {
		st.Total++

		answered := it.Followup != nil
		regret := answered && it.Followup.Regret == RegretYes

		switch {
		case answered:
			st.Answered++
			if regret {
				st.RegretYes++
			} else {
				st.RegretNo++
			}
		case dueForFollowup(it.CreatedAt, now):
			st.Pending++
		}

		cs := byCat[it.Category]
		if cs == nil {
			cs = &CategoryStat{Category: it.Category}
			byCat[it.Category] = cs
		}
		cs.Total++
		if answered {
			cs.Answered++
		}
		if regret {
			cs.RegretYes++
		}

		mon := monthOf(it.CreatedAt)
		ms := byMon[mon]
		if ms == nil {
			ms = &MonthStat{Month: mon}
			byMon[mon] = ms
		}
		ms.Total++
		if answered {
			ms.Answered++
		}
		if regret {
			ms.RegretYes++
		}

		if regret {
			// 拷一份再放进结果：Followup 是指针，直接共享出去的话
			// 调用方改一下就能脏到存储内部状态。
			rec := it
			f := *it.Followup
			rec.Followup = &f
			st.Regretted = append(st.Regretted, rec)
		}
	}

	st.RegretRate = rate(st.RegretYes, st.Answered)

	for _, cs := range byCat {
		cs.RegretRate = rate(cs.RegretYes, cs.Answered)
		st.ByCategory = append(st.ByCategory, *cs)
	}
	// 后悔率降序 → 已回访数降序 → 总数降序 → 品类名升序。
	// 最后一级按名字排是为了结果稳定：map 遍历顺序随机，
	// 不兜住的话同样的档案会排出不同顺序，测试也就没法断言。
	sort.SliceStable(st.ByCategory, func(i, j int) bool {
		a, b := st.ByCategory[i], st.ByCategory[j]
		if a.RegretRate != b.RegretRate {
			return a.RegretRate > b.RegretRate
		}
		if a.Answered != b.Answered {
			return a.Answered > b.Answered
		}
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Category < b.Category
	})

	for _, ms := range byMon {
		ms.RegretRate = rate(ms.RegretYes, ms.Answered)
		st.ByMonth = append(st.ByMonth, *ms)
	}
	// 时间正序看趋势，但"未知"桶排最后，别插在中间打断阅读。
	sort.SliceStable(st.ByMonth, func(i, j int) bool {
		a, b := st.ByMonth[i].Month, st.ByMonth[j].Month
		if a == "" {
			return false
		}
		if b == "" {
			return true
		}
		return a < b
	})

	// 最近后悔的排最前。answeredAt 全是 RFC3339 UTC，字典序即时序。
	sort.SliceStable(st.Regretted, func(i, j int) bool {
		return st.Regretted[i].Followup.AnsweredAt > st.Regretted[j].Followup.AnsweredAt
	})

	return st
}

// rate 后悔率。分母为 0 时返回 0 —— 一次回访都没有时，
// "后悔率 0%" 和 "没有数据" 是两回事，前端靠 Answered 区分，这里不返回 NaN。
func rate(yes, answered int) float64 {
	if answered == 0 {
		return 0
	}
	return float64(yes) / float64(answered)
}

// dueForFollowup 判断一条未回访的决策是否已到该回访的时间。
// createdAt 解析不出来时不算到期 —— 时间都坏了，主动去烦用户没有意义。
func dueForFollowup(createdAt string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return false
	}
	return now.Sub(t) >= FollowupDueDays*24*time.Hour
}

// monthOf 取 YYYY-MM 作为分月桶的键；解析失败返回空串（"未知"）。
func monthOf(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return ""
	}
	return t.Format("2006-01")
}

// Stats 返回当前档案的聚合视图。
func (s *FileStore) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ComputeStats(s.items, s.now())
}
