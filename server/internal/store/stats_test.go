package store

import (
	"path/filepath"
	"testing"
	"time"
)

var statsNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

// 测试辅助：造一条记录。answersAt 为 0 表示还没回访。
func statRec(id, category string, daysAgo int, regret string, answeredDaysAgo int) DecisionRecord {
	r := DecisionRecord{
		ID:        id,
		Question:  "q-" + id,
		OptionA:   "A",
		OptionB:   "B",
		Category:  category,
		CreatedAt: statsNow.AddDate(0, 0, -daysAgo).Format(time.RFC3339),
	}
	if regret != "" {
		r.Followup = &Followup{
			Regret:     regret,
			AnsweredAt: statsNow.AddDate(0, 0, -answeredDaysAgo).Format(time.RFC3339),
		}
	}
	return r
}

func TestComputeStats_Empty(t *testing.T) {
	st := ComputeStats(nil, statsNow)
	if st.Total != 0 || st.Answered != 0 || st.Pending != 0 || st.RegretRate != 0 {
		t.Fatalf("空档案应全为 0，得到 %+v", st)
	}
	// 三个切片必须是空数组而不是 nil：前端直接 .map()，
	// null 会让看板在第一次打开（还没有任何决策）时崩掉。
	if st.ByCategory == nil || st.ByMonth == nil || st.Regretted == nil {
		t.Fatalf("空档案的切片必须是 [] 而不是 nil: byCategory=%v byMonth=%v regretted=%v",
			st.ByCategory, st.ByMonth, st.Regretted)
	}
}

// 后悔率的分母是"已回访数"，不是总决策数 —— 本套件里最重要的一条断言。
func TestComputeStats_RegretRateUsesAnsweredAsDenominator(t *testing.T) {
	items := []DecisionRecord{
		statRec("1", "phone", 10, RegretYes, 1),
		statRec("2", "phone", 10, RegretNo, 1),
		statRec("3", "job", 10, "", 0), // 未回访
	}
	st := ComputeStats(items, statsNow)

	if st.Total != 3 {
		t.Errorf("Total = %d, 期望 3", st.Total)
	}
	if st.Answered != 2 {
		t.Errorf("Answered = %d, 期望 2（未回访的不算）", st.Answered)
	}
	if st.RegretYes != 1 || st.RegretNo != 1 {
		t.Errorf("RegretYes/No = %d/%d, 期望 1/1", st.RegretYes, st.RegretNo)
	}
	// 1/2 = 0.5。若错用总数做分母会得到 1/3 —— 未回访被当成"不后悔"，
	// 档案越攒越多，后悔率就越接近 0，这个数字会彻底失去意义。
	if st.RegretRate != 0.5 {
		t.Errorf("RegretRate = %v, 期望 0.5", st.RegretRate)
	}
}

func TestComputeStats_NoFollowupRateIsZeroNotNaN(t *testing.T) {
	st := ComputeStats([]DecisionRecord{statRec("1", "", 10, "", 0)}, statsNow)
	if st.RegretRate != 0 {
		t.Errorf("无人回访时期望 0（不是 NaN），得到 %v", st.RegretRate)
	}
}

// 90 天边界：到期才算 pending，差一天不算，已回访的永远不算。
func TestComputeStats_PendingBoundary(t *testing.T) {
	cases := []struct {
		name     string
		daysAgo  int
		regret   string
		wantPend int
	}{
		{"差一天不到期", 89, "", 0},
		{"正好到期", 90, "", 1},
		{"远超期", 200, "", 1},
		{"已回访的超期记录不再待回访", 200, RegretNo, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := ComputeStats([]DecisionRecord{statRec("x", "", c.daysAgo, c.regret, 1)}, statsNow)
			if st.Pending != c.wantPend {
				t.Errorf("Pending = %d, 期望 %d", st.Pending, c.wantPend)
			}
		})
	}
}

// createdAt 坏掉时不能把记录丢掉：各分项之和必须始终等于 Total。
func TestComputeStats_BadCreatedAtGoesToUnknownBucket(t *testing.T) {
	bad := statRec("bad", "phone", 10, RegretNo, 1)
	bad.CreatedAt = "not-a-time"
	st := ComputeStats([]DecisionRecord{bad}, statsNow)

	if st.Total != 1 || st.Answered != 1 {
		t.Fatalf("坏时间不该让记录消失，得到 total=%d answered=%d", st.Total, st.Answered)
	}
	if len(st.ByMonth) != 1 || st.ByMonth[0].Month != "" {
		t.Fatalf("坏时间应归入未知桶 \"\"，得到 %+v", st.ByMonth)
	}
	if st.Pending != 0 {
		t.Errorf("时间都坏了不算到期，Pending = %d", st.Pending)
	}
}

func TestComputeStats_ByCategorySortedByRegretRate(t *testing.T) {
	items := []DecisionRecord{
		statRec("j1", "job", 10, RegretNo, 1),
		statRec("j2", "job", 10, RegretNo, 1),
		statRec("j3", "job", 10, RegretNo, 1),
		statRec("j4", "job", 10, RegretYes, 1), // job: 1/4 = 25%
		statRec("c1", "city", 10, RegretYes, 1),
		statRec("c2", "city", 10, RegretNo, 1), // city: 1/2 = 50%
		statRec("p1", "phone", 10, RegretYes, 1),
		statRec("p2", "phone", 10, RegretYes, 1), // phone: 2/2 = 100%
	}
	st := ComputeStats(items, statsNow)

	want := []struct {
		cat string
		got float64
	}{
		{"phone", 1.0},
		{"city", 0.5},
		{"job", 0.25},
	}
	if len(st.ByCategory) != len(want) {
		t.Fatalf("品类数 = %d, 期望 %d: %+v", len(st.ByCategory), len(want), st.ByCategory)
	}
	for i, w := range want {
		got := st.ByCategory[i]
		if got.Category != w.cat {
			t.Errorf("第 %d 位品类 = %q, 期望 %q", i, got.Category, w.cat)
		}
		if got.RegretRate != w.got {
			t.Errorf("%s 后悔率 = %v, 期望 %v", w.cat, got.RegretRate, w.got)
		}
	}
	// 小样本必须带上样本量：100% 但只有 2 条，和 100% 且有 50 条完全不是一回事。
	if st.ByCategory[0].Answered != 2 || st.ByCategory[0].Total != 2 {
		t.Errorf("phone 桶应带 total=2 answered=2，得到 %+v", st.ByCategory[0])
	}
}

// 老档案（P8 之前落库的）没有 category 字段，读出即空串，应归入"未分类"而不是被丢掉。
func TestComputeStats_RecordsWithoutCategoryAreUncategorized(t *testing.T) {
	items := []DecisionRecord{
		{ID: "old", Question: "q", CreatedAt: statsNow.AddDate(0, 0, -5).Format(time.RFC3339)},
		statRec("new", "phone", 5, RegretYes, 1),
	}
	st := ComputeStats(items, statsNow)

	if st.Total != 2 {
		t.Fatalf("无品类记录不该被丢掉，Total = %d", st.Total)
	}
	if len(st.ByCategory) != 2 {
		t.Fatalf("应有两个桶（未分类 + phone），得到 %d", len(st.ByCategory))
	}
	// phone 后悔率 100% 排第一，未分类 0% 排其后。
	if st.ByCategory[0].Category != "phone" || st.ByCategory[1].Category != "" {
		t.Fatalf("桶顺序/名字不对: %+v", st.ByCategory)
	}
}

// 排序结果必须稳定：map 遍历顺序随机，不兜住的话同一份档案会排出不同顺序。
func TestComputeStats_CategoryOrderIsStable(t *testing.T) {
	items := []DecisionRecord{
		statRec("a1", "a", 10, RegretYes, 1),
		statRec("b1", "b", 10, RegretYes, 1),
		statRec("c1", "c", 10, RegretYes, 1),
	}
	first := ComputeStats(items, statsNow)
	for i := 0; i < 20; i++ {
		got := ComputeStats(items, statsNow)
		for j := range got.ByCategory {
			if got.ByCategory[j].Category != first.ByCategory[j].Category {
				t.Fatalf("同率品类顺序不稳定: 首次 %v, 第 %d 次 %v",
					catNames(first), i, catNames(got))
			}
		}
	}
}

func catNames(st Stats) []string {
	out := make([]string, 0, len(st.ByCategory))
	for _, c := range st.ByCategory {
		out = append(out, c.Category)
	}
	return out
}

func TestComputeStats_ByMonthChronologicalWithUnknownLast(t *testing.T) {
	items := []DecisionRecord{
		statRec("sep", "phone", 5, RegretNo, 1),
		statRec("jul", "phone", 70, RegretNo, 1),
		statRec("aug", "phone", 40, RegretNo, 1),
	}
	unknown := statRec("unk", "phone", 5, RegretNo, 1)
	unknown.CreatedAt = ""
	items = append(items, unknown)

	st := ComputeStats(items, statsNow)
	want := []string{"2026-07", "2026-08", "2026-09", ""}
	if len(st.ByMonth) != len(want) {
		t.Fatalf("月份桶数 = %d, 期望 %d: %+v", len(st.ByMonth), len(want), st.ByMonth)
	}
	for i, w := range want {
		if st.ByMonth[i].Month != w {
			t.Errorf("第 %d 个月份 = %q, 期望 %q", i, st.ByMonth[i].Month, w)
		}
	}
}

func TestComputeStats_RegrettedOnlyYesAndNewestFirst(t *testing.T) {
	items := []DecisionRecord{
		statRec("old-yes", "phone", 100, RegretYes, 30),
		statRec("no", "phone", 100, RegretNo, 20),
		statRec("new-yes", "job", 100, RegretYes, 2),
		statRec("unanswered", "job", 100, "", 0),
	}
	st := ComputeStats(items, statsNow)

	if len(st.Regretted) != 2 {
		t.Fatalf("后悔列表应只有 2 条 yes，得到 %d: %+v", len(st.Regretted), st.Regretted)
	}
	if st.Regretted[0].ID != "new-yes" || st.Regretted[1].ID != "old-yes" {
		t.Errorf("应按 answeredAt 倒序（最近的后悔在前），得到 %q, %q",
			st.Regretted[0].ID, st.Regretted[1].ID)
	}
}

// Regretted 里的 Followup 必须深拷贝，否则调用方改一下就能脏到存储内部。
func TestComputeStats_RegrettedFollowupIsCopied(t *testing.T) {
	items := []DecisionRecord{statRec("x", "phone", 10, RegretYes, 1)}
	st := ComputeStats(items, statsNow)

	st.Regretted[0].Followup.Regret = RegretNo
	if items[0].Followup.Regret != RegretYes {
		t.Fatalf("改动返回值脏到了原始数据: %q", items[0].Followup.Regret)
	}
}

// FileStore.Stats() 走真实存储：写入 → 回访 → 聚合，验证三件事串起来是对的。
func TestFileStore_Stats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	s.now = func() time.Time { return statsNow }

	for _, r := range []DecisionRecord{
		statRec("1", "phone", 10, "", 0),
		statRec("2", "phone", 10, "", 0),
		statRec("3", "job", 120, "", 0),
	} {
		if _, err := s.Add(r); err != nil {
			t.Fatalf("Add(%s): %v", r.ID, err)
		}
	}
	// 一条后悔、一条不后悔、一条超期未回访。
	if _, err := s.MarkFollowup("1", RegretYes); err != nil {
		t.Fatalf("MarkFollowup: %v", err)
	}
	if _, err := s.MarkFollowup("2", RegretNo); err != nil {
		t.Fatalf("MarkFollowup: %v", err)
	}

	st := s.Stats()
	if st.Total != 3 || st.Answered != 2 || st.RegretYes != 1 {
		t.Fatalf("聚合不对: %+v", st)
	}
	if st.RegretRate != 0.5 {
		t.Errorf("RegretRate = %v, 期望 0.5", st.RegretRate)
	}
	// "3" 是 120 天前建的且未回访 → 待回访。
	if st.Pending != 1 {
		t.Errorf("Pending = %d, 期望 1", st.Pending)
	}
	// 品类维度也要真的从落盘数据里读出来。
	if len(st.ByCategory) != 2 {
		t.Fatalf("品类桶数 = %d, 期望 2: %+v", len(st.ByCategory), st.ByCategory)
	}
	if st.ByCategory[0].Category != "phone" || st.ByCategory[0].RegretRate != 0.5 {
		t.Errorf("phone 桶 = %+v, 期望 rate 0.5", st.ByCategory[0])
	}
}

// 重开能读回：统计必须反映真实落盘内容，而不是只活在内存里。
func TestFileStore_StatsSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.json")
	s1, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if _, err := s1.Add(statRec("1", "phone", 10, "", 0)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s1.MarkFollowup("1", RegretYes); err != nil {
		t.Fatalf("MarkFollowup: %v", err)
	}

	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("重开失败: %v", err)
	}
	st := s2.Stats()
	if st.Total != 1 || st.RegretYes != 1 || st.RegretRate != 1 {
		t.Fatalf("重开后统计不对: %+v", st)
	}
	// category 也要真的落了盘（P8 新增字段的持久化）。
	got, ok := s2.Get("1")
	if !ok || got.Category != "phone" {
		t.Fatalf("重开后 category 丢失: %+v (ok=%v)", got, ok)
	}
}
