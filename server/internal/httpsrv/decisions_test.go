package httpsrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yourorg/decision-debate/internal/store"
)

// newDecisionsMux 搭一个与 cmd/server 真实路由一致的 mux，
// 这样测的是"用户真正会打到的那个路径"，而不是 handler 的内部函数。
func newDecisionsMux(t *testing.T) *http.ServeMux {
	t.Helper()
	s, err := store.NewFileStore(t.TempDir() + "/decisions.json")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/decisions", NewDecisionsHandler(s))
	mux.Handle("POST /api/decisions", NewDecisionsHandler(s))
	mux.Handle("POST /api/decisions/{id}/followup", NewDecisionFollowupHandler(s))
	return mux
}

func do(t *testing.T, mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) []store.DecisionRecord {
	t.Helper()
	var out struct {
		Decisions []store.DecisionRecord `json:"decisions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析列表响应失败（body=%s）：%v", w.Body.String(), err)
	}
	return out.Decisions
}

func decodeOne(t *testing.T, w *httptest.ResponseRecorder) store.DecisionRecord {
	t.Helper()
	var out struct {
		Decision store.DecisionRecord `json:"decision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析单条响应失败（body=%s）：%v", w.Body.String(), err)
	}
	return out.Decision
}

const validRec = `{"id":"d1","question":"买房还是租房？","optionA":"买房","optionB":"租房",` +
	`"createdAt":"2026-06-01T10:00:00Z","assumptionCount":2}`

func TestListEmptyArchive(t *testing.T) {
	mux := newDecisionsMux(t)
	w := do(t, mux, "GET", "/api/decisions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d（body=%s）", w.Code, w.Body.String())
	}
	// 空档案必须是 [] 而不是 null，否则前端 .map() 会炸。
	if !strings.Contains(w.Body.String(), `"decisions":[]`) {
		t.Fatalf("空档案应返回 []，实际 %s", w.Body.String())
	}
}

func TestCreateThenList(t *testing.T) {
	mux := newDecisionsMux(t)
	w := do(t, mux, "POST", "/api/decisions", validRec)
	if w.Code != http.StatusCreated {
		t.Fatalf("首次创建期望 201，实际 %d（body=%s）", w.Code, w.Body.String())
	}
	if got := decodeOne(t, w); got.ID != "d1" || got.AssumptionCount != 2 {
		t.Fatalf("创建返回内容不符：%+v", got)
	}

	list := decodeList(t, do(t, mux, "GET", "/api/decisions", ""))
	if len(list) != 1 || list[0].Question != "买房还是租房？" {
		t.Fatalf("回读不符：%+v", list)
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	mux := newDecisionsMux(t)
	do(t, mux, "POST", "/api/decisions", validRec)
	w := do(t, mux, "POST", "/api/decisions", strings.Replace(validRec,
		"买房还是租房？", "买房还是租房？(重试)", 1))
	if w.Code != http.StatusOK {
		t.Fatalf("重复提交期望 200，实际 %d", w.Code)
	}
	list := decodeList(t, do(t, mux, "GET", "/api/decisions", ""))
	if len(list) != 1 {
		t.Fatalf("同 id 不应产生重复记录，实际 %d 条", len(list))
	}
	if list[0].Question != "买房还是租房？(重试)" {
		t.Fatalf("重复提交应覆盖为最新内容，实际 %q", list[0].Question)
	}
}

func TestCreateRejectsBlankFields(t *testing.T) {
	mux := newDecisionsMux(t)
	cases := map[string]string{
		"缺 id":       `{"question":"买房还是租房？"}`,
		"缺 question": `{"id":"d1"}`,
		"坏 JSON":     `{ 这不是 JSON`,
	}
	for name, body := range cases {
		w := do(t, mux, "POST", "/api/decisions", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s 期望 400，实际 %d（body=%s）", name, w.Code, w.Body.String())
		}
	}
}

func TestCreateRejectsOversizedBody(t *testing.T) {
	mux := newDecisionsMux(t)
	big := `{"id":"d1","question":"` + strings.Repeat("x", MaxDecisionBytes) + `"}`
	w := do(t, mux, "POST", "/api/decisions", big)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大 body 期望 413，实际 %d", w.Code)
	}
}

func TestCreateFillsMissingCreatedAt(t *testing.T) {
	mux := newDecisionsMux(t)
	w := do(t, mux, "POST", "/api/decisions", `{"id":"d1","question":"买房还是租房？"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("期望 201，实际 %d（body=%s）", w.Code, w.Body.String())
	}
	// 漏传 createdAt 会让前端的 isDueForFollowup 算出 NaN → 永不弹回访，
	// 是个查不出来的静默 bug，所以服务端必须补上。
	if got := decodeOne(t, w); got.CreatedAt == "" {
		t.Fatal("服务端应补全 createdAt")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	mux := newDecisionsMux(t)
	// 增强 ServeMux 会自己拦方法不匹配并返回 405 + Allow。
	w := do(t, mux, "DELETE", "/api/decisions", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE 集合期望 405，实际 %d", w.Code)
	}
	w = do(t, mux, "GET", "/api/decisions/d1/followup", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 回访端点期望 405，实际 %d", w.Code)
	}
}

func TestFollowupRoundTrip(t *testing.T) {
	mux := newDecisionsMux(t)
	do(t, mux, "POST", "/api/decisions", validRec)

	w := do(t, mux, "POST", "/api/decisions/d1/followup", `{"regret":"yes"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("回访期望 200，实际 %d（body=%s）", w.Code, w.Body.String())
	}
	rec := decodeOne(t, w)
	if rec.Followup == nil || rec.Followup.Regret != "yes" || rec.Followup.AnsweredAt == "" {
		t.Fatalf("回访未正确落上：%+v", rec.Followup)
	}

	// 列表里必须能看到回访结果 —— 这是"跨设备"的意义所在。
	list := decodeList(t, do(t, mux, "GET", "/api/decisions", ""))
	if len(list) != 1 || list[0].Followup == nil || list[0].Followup.Regret != "yes" {
		t.Fatalf("列表中回访不可见：%+v", list)
	}
}

func TestFollowupUnknownID(t *testing.T) {
	mux := newDecisionsMux(t)
	w := do(t, mux, "POST", "/api/decisions/nope/followup", `{"regret":"yes"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知 id 期望 404，实际 %d（body=%s）", w.Code, w.Body.String())
	}
}

func TestFollowupRejectsBadInput(t *testing.T) {
	mux := newDecisionsMux(t)
	do(t, mux, "POST", "/api/decisions", validRec)
	cases := map[string]string{
		"非法 regret": `{"regret":"maybe"}`,
		"空 regret":  `{}`,
		"坏 JSON":    `{ nope`,
	}
	for name, body := range cases {
		w := do(t, mux, "POST", "/api/decisions/d1/followup", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s 期望 400，实际 %d（body=%s）", name, w.Code, w.Body.String())
		}
	}
	// 非法输入不能污染已有记录
	list := decodeList(t, do(t, mux, "GET", "/api/decisions", ""))
	if list[0].Followup != nil {
		t.Fatal("非法回访不应写入")
	}
}

// TestHandlerRejectsMethodWhenUsedWithoutMux 覆盖 handler 自带的 405 分支。
// 走 mux 时是 mux 拦的方法（见 TestMethodNotAllowed），但 handler 也可能被
// 直接挂在别的路由上，那时这个分支就是唯一的防线。
func TestHandlerRejectsMethodWhenUsedWithoutMux(t *testing.T) {
	s, err := store.NewFileStore(t.TempDir() + "/decisions.json")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	w := httptest.NewRecorder()
	NewDecisionsHandler(s).ServeHTTP(w, httptest.NewRequest("PUT", "/api/decisions", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("集合 handler 对 PUT 期望 405，实际 %d", w.Code)
	}
	if w.Header().Get("Allow") == "" {
		t.Fatal("405 应带 Allow 头")
	}

	w = httptest.NewRecorder()
	NewDecisionFollowupHandler(s).ServeHTTP(w, httptest.NewRequest("GET", "/api/decisions/d1/followup", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("回访 handler 对 GET 期望 405，实际 %d", w.Code)
	}
}

// TestFollowupRejectsEmptyID 覆盖通配参数缺失的情况（handler 被挂在非 {id} 路由时）。
func TestFollowupRejectsEmptyID(t *testing.T) {
	s, err := store.NewFileStore(t.TempDir() + "/decisions.json")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/decisions//followup", strings.NewReader(`{"regret":"yes"}`))
	NewDecisionFollowupHandler(s).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺 id 期望 400，实际 %d（body=%s）", w.Code, w.Body.String())
	}
}

func TestRejectsUnknownPath(t *testing.T) {
	mux := newDecisionsMux(t)
	w := do(t, mux, "GET", "/api/decisions/d1/followup/extra", "")
	if w.Code == http.StatusOK {
		t.Fatalf("未注册的路径不应返回 200")
	}
}
