// Package store 决策档案的持久化层。
//
// P7 引入：把 P6·c 纯前端的 localStorage 档案搬到服务端，
// 让同一份"你后悔吗"数据能跨设备访问。
//
// 设计取舍（与个人工作台的定位一致）：
//   - 单文件 JSON，零第三方依赖：部署就是拷一个文件，不需要数据库。
//   - 写入用"临时文件 + rename"原子替换：进程被杀也不会留下半截 JSON。
//   - 单用户无鉴权：这是个人工具，不是多租户 SaaS。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrInvalidRecord 记录字段不合法。调用方可用 errors.Is 判定后转成 400。
var ErrInvalidRecord = errors.New("store: invalid decision record")

// ErrNotFound 找不到对应 id 的决策。调用方可用 errors.Is 判定后转成 404。
var ErrNotFound = errors.New("store: decision not found")

// RegretYes / RegretNo 是回访结果的合法取值。
const (
	RegretYes = "yes"
	RegretNo  = "no"
)

// Followup 三个月后的回访结果。
type Followup struct {
	Regret     string `json:"regret"`     // RegretYes 或 RegretNo
	AnsweredAt string `json:"answeredAt"` // RFC3339
}

// DecisionRecord 与前端 web/src/types.ts 的 DecisionRecord 字段一一对应。
// 改任一侧都要同步另一侧，httpsrv 的接口测试会守住这个契约。
type DecisionRecord struct {
	ID              string    `json:"id"` // 即 dilemma_id
	Question        string    `json:"question"`
	OptionA         string    `json:"optionA"`
	OptionB         string    `json:"optionB"`
	CreatedAt       string    `json:"createdAt"` // RFC3339
	AssumptionCount int       `json:"assumptionCount"`
	Followup        *Followup `json:"followup,omitempty"`
	// Category 决策品类（P8 引入，用于后悔率按品类拆解）。
	// omitempty 且不做取值白名单：品类模板在前端（templates.ts），
	// 后端不该被前端的模板清单绑住，新增品类无需改后端。
	// 老档案没有这个字段，读出即空串，归入"未分类"桶。
	Category string `json:"category,omitempty"`
}

// Validate 校验必填字段。后端不做内容清洗，只挡掉会让档案失去意义的空值。
func (r DecisionRecord) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidRecord)
	}
	if r.Question == "" {
		return fmt.Errorf("%w: question is required", ErrInvalidRecord)
	}
	return nil
}

// FileStore 单文件 JSON 存储。所有方法并发安全。
// items 按"新→旧"排列，Add 会把新记录置顶。
type FileStore struct {
	mu    sync.Mutex
	path  string
	items []DecisionRecord
	now   func() time.Time // 可注入，便于单测不依赖真实时钟
}

// NewFileStore 打开（必要时惰性创建）存储文件。
// 文件不存在视为空档案；文件存在但 JSON 损坏则返回错误 —— 这种时候
// 宁可启动失败让人来修，也不能静默清空用户的历史决策。
func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, now: time.Now}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.items = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: read %s: %w", s.path, err)
	}
	if len(raw) == 0 {
		s.items = nil
		return nil
	}
	var items []DecisionRecord
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("store: parse %s: %w", s.path, err)
	}
	s.items = items
	return nil
}

// List 返回全部记录（新→旧）。返回副本，调用方改动不会脏到内部状态。
func (s *FileStore) List() []DecisionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DecisionRecord, len(s.items))
	copy(out, s.items)
	return out
}

// Get 按 id 查找。
func (s *FileStore) Get(id string) (DecisionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return DecisionRecord{}, false
}

// Add 写入一条决策并落盘。同 id 视为同一场辩论：去重后置顶，
// 这样重复的 done 帧或前端重试都不会让档案里长出一堆双胞胎。
func (s *FileStore) Add(rec DecisionRecord) (DecisionRecord, error) {
	if err := rec.Validate(); err != nil {
		return DecisionRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]DecisionRecord, 0, len(s.items)+1)
	for _, it := range s.items {
		if it.ID != rec.ID {
			kept = append(kept, it)
		}
	}
	s.items = append([]DecisionRecord{rec}, kept...)
	if err := s.persist(); err != nil {
		return DecisionRecord{}, err
	}
	return rec, nil
}

// MarkFollowup 记录回访结果。已回访的允许覆盖 —— 用户三个月后改主意很正常，
// 第二次回答才是更接近真相的那一次。
func (s *FileStore) MarkFollowup(id string, regret string) (DecisionRecord, error) {
	if regret != RegretYes && regret != RegretNo {
		return DecisionRecord{}, fmt.Errorf("%w: regret must be %q or %q, got %q",
			ErrInvalidRecord, RegretYes, RegretNo, regret)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Followup = &Followup{
				Regret:     regret,
				AnsweredAt: s.now().UTC().Format(time.RFC3339),
			}
			rec := s.items[i]
			if err := s.persist(); err != nil {
				return DecisionRecord{}, err
			}
			return rec, nil
		}
	}
	return DecisionRecord{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// persist 原子落盘：先写同目录临时文件，再 rename 覆盖。
// 调用方必须已持有 s.mu。
func (s *FileStore) persist() error {
	raw, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode: %w", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(s.path)
	// 允许 DECISIONS_FILE 指向尚不存在的子目录（如 ./data/decisions.json），
	// 直接建出来比让用户对着一个 open 错误猜路径方便。
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("store: mkdir %s: %w", dir, err)
		}
	}

	tmp, err := os.CreateTemp(dir, ".decisions-*.tmp")
	if err != nil {
		return fmt.Errorf("store: create temp: %w", err)
	}
	tmpName := tmp.Name()
	// 任何一步失败都要清掉临时文件，否则目录里会慢慢堆满垃圾。
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("store: rename: %w", err)
	}
	tmpName = "" // 已被 rename 移走，别再删
	return nil
}
