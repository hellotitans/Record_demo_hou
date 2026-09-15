package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newStore 在临时目录里开一个空档案，并在测试结束后自动清理。
func newStore(t *testing.T) *FileStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decisions.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

func rec(id, question string) DecisionRecord {
	return DecisionRecord{
		ID:              id,
		Question:        question,
		OptionA:         "买房",
		OptionB:         "租房",
		CreatedAt:       "2026-06-01T10:00:00Z",
		AssumptionCount: 2,
	}
}

func TestFileNotExistIsEmpty(t *testing.T) {
	s := newStore(t)
	if got := s.List(); len(got) != 0 {
		t.Fatalf("空档案应返回 0 条，实际 %d 条", len(got))
	}
}

func TestAddAndList(t *testing.T) {
	s := newStore(t)
	if _, err := s.Add(rec("d1", "买房还是租房？")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("期望 1 条，实际 %d 条", len(list))
	}
	if list[0].ID != "d1" || list[0].AssumptionCount != 2 {
		t.Fatalf("字段回读不符：%+v", list[0])
	}
}

func TestAddPrependsNewest(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add(rec("old", "先问的"))
	_, _ = s.Add(rec("new", "后问的"))
	list := s.List()
	if len(list) != 2 || list[0].ID != "new" || list[1].ID != "old" {
		t.Fatalf("新记录应置顶，实际顺序：%+v", list)
	}
}

func TestAddDeduplicatesByID(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add(rec("d1", "买房还是租房？"))
	_, _ = s.Add(rec("d1", "买房还是租房？(重复提交)"))
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("同 id 应去重成 1 条，实际 %d 条", len(list))
	}
	if list[0].Question != "买房还是租房？(重复提交)" {
		t.Fatalf("去重后应保留最新内容，实际：%q", list[0].Question)
	}
}

func TestAddRejectsBlankFields(t *testing.T) {
	s := newStore(t)
	if _, err := s.Add(rec("", "没有 id")); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("缺 id 应返回 ErrInvalidRecord，实际 %v", err)
	}
	if _, err := s.Add(rec("d1", "")); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("缺 question 应返回 ErrInvalidRecord，实际 %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatal("非法记录不应落库")
	}
}

func TestMarkFollowup(t *testing.T) {
	s := newStore(t)
	// 注入时钟，让 answeredAt 可断言。
	s.now = func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) }
	_, _ = s.Add(rec("d1", "买房还是租房？"))

	got, err := s.MarkFollowup("d1", RegretYes)
	if err != nil {
		t.Fatalf("MarkFollowup: %v", err)
	}
	if got.Followup == nil || got.Followup.Regret != RegretYes {
		t.Fatalf("回访未落上：%+v", got)
	}
	if got.Followup.AnsweredAt != "2026-09-02T12:00:00Z" {
		t.Fatalf("answeredAt 应为注入时钟的 RFC3339，实际 %q", got.Followup.AnsweredAt)
	}
	// 落库后重新 List 也要能看到（说明真的持久化了，不止改了内存）
	if list := s.List(); len(list) != 1 || list[0].Followup == nil {
		t.Fatalf("回访未持久化：%+v", list)
	}
}

func TestMarkFollowupCanOverride(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add(rec("d1", "买房还是租房？"))
	_, _ = s.MarkFollowup("d1", RegretYes)
	got, err := s.MarkFollowup("d1", RegretNo)
	if err != nil {
		t.Fatalf("二次回访不应报错：%v", err)
	}
	if got.Followup.Regret != RegretNo {
		t.Fatalf("改主意后应记录最新答案，实际 %q", got.Followup.Regret)
	}
}

func TestMarkFollowupUnknownID(t *testing.T) {
	s := newStore(t)
	if _, err := s.MarkFollowup("nope", RegretYes); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未知 id 应返回 ErrNotFound，实际 %v", err)
	}
}

func TestMarkFollowupRejectsBadRegret(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add(rec("d1", "买房还是租房？"))
	if _, err := s.MarkFollowup("d1", "maybe"); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("非法 regret 应返回 ErrInvalidRecord，实际 %v", err)
	}
	if list := s.List(); list[0].Followup != nil {
		t.Fatal("非法回访不应写入")
	}
}

func TestGet(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add(rec("d1", "买房还是租房？"))
	got, ok := s.Get("d1")
	if !ok || got.ID != "d1" {
		t.Fatalf("Get 命中失败：%+v ok=%v", got, ok)
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("未知 id 的 Get 应返回 false")
	}
}

// TestListReturnsCopy 守住"返回副本"这个约定：
// 调用方改返回值不能脏到内部状态，否则并发下会出现莫名其妙的数据漂移。
func TestListReturnsCopy(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add(rec("d1", "买房还是租房？"))
	list := s.List()
	list[0].Question = "被外部改坏了"
	if got := s.List(); got[0].Question != "买房还是租房？" {
		t.Fatalf("外部改动污染了内部状态：%q", got[0].Question)
	}
}

// TestPersistenceAcrossReopen 是持久化最该守住的一条：
// 重开一个 FileStore 必须能读回之前写进去的东西，否则这层就是假的。
func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.json")
	s1, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if _, err := s1.Add(rec("d1", "买房还是租房？")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s1.MarkFollowup("d1", RegretNo); err != nil {
		t.Fatalf("MarkFollowup: %v", err)
	}

	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("重开失败：%v", err)
	}
	list := s2.List()
	if len(list) != 1 {
		t.Fatalf("重开后应读回 1 条，实际 %d 条", len(list))
	}
	if list[0].ID != "d1" || list[0].Followup == nil || list[0].Followup.Regret != RegretNo {
		t.Fatalf("重开后字段不符：%+v", list[0])
	}
}

func TestCorruptFileFailsFast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.json")
	if err := os.WriteFile(path, []byte("{ 这不是 JSON"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// 宁可启动失败让人来修，也不能静默清空用户的历史决策。
	if _, err := NewFileStore(path); err == nil {
		t.Fatal("损坏文件应报错，不应静默当空档案")
	}
}

func TestEmptyFileIsEmptyArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("空文件应视为空档案，实际报错：%v", err)
	}
	if len(s.List()) != 0 {
		t.Fatal("空文件应读到 0 条")
	}
}

// TestConcurrentAdd 用 -race 跑时会暴露数据竞争。并发写必须全部落库且不丢。
func TestConcurrentAdd(t *testing.T) {
	s := newStore(t)
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = s.Add(rec("d"+string(rune('A'+i%26))+string(rune('0'+i/26)), "问题 "+string(rune('A'+i%26))))
		}(i)
	}
	wg.Wait()
	if got := len(s.List()); got != n {
		t.Fatalf("并发写入应落库 %d 条，实际 %d 条", n, got)
	}
	// 并发写完后文件必须仍然可解析（原子写的意义所在）
	s2, err := NewFileStore(s.path)
	if err != nil {
		t.Fatalf("并发写后重开失败：%v", err)
	}
	if len(s2.List()) != n {
		t.Fatalf("重开后条数不符：%d", len(s2.List()))
	}
}

// TestNoTempLeftovers 守住清理逻辑：写完后目录里不应残留 .decisions-*.tmp。
func TestNoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(filepath.Join(dir, "decisions.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	_, _ = s.Add(rec("d1", "买房还是租房？"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("残留临时文件：%s", e.Name())
		}
	}
}
