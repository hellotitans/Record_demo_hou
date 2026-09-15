package httpsrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yourorg/decision-debate/internal/store"
)

// newStatsMux 搭一个与 cmd/server 真实路由一致的 mux（含 stats），
// 并把 store 交还给调用方以便造数据。
func newStatsMux(t *testing.T) (*http.ServeMux, *store.FileStore) {
	t.Helper()
	s, err := store.NewFileStore(t.TempDir() + "/decisions.json")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/decisions", NewDecisionsHandler(s))
	mux.Handle("POST /api/decisions", NewDecisionsHandler(s))
	mux.Handle("POST /api/decisions/{id}/followup", NewDecisionFollowupHandler(s))
	mux.Handle("GET /api/decisions/stats", NewStatsHandler(s))
	return mux, s
}

func decodeStats(t *testing.T, w *httptest.ResponseRecorder) store.Stats {
	t.Helper()
	var out struct {
		Stats store.Stats `json:"stats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析 stats 响应失败: %v (body=%s)", err, w.Body.String())
	}
	return out.Stats
}

// 造一条决策。daysAgo 控制 createdAt，用来造"到期未回访"。
func seedDecision(t *testing.T, mux http.Handler, id, category string, daysAgo int) {
	t.Helper()
	created := time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	body := `{"id":"` + id + `","question":"q-` + id + `","optionA":"A","optionB":"B",` +
		`"createdAt":"` + created + `","assumptionCount":2,"category":"` + category + `"}`
	w := do(t, mux, http.MethodPost, "/api/decisions", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("造数据 %s 失败: 期望 201，得到 %d (%s)", id, w.Code, w.Body.String())
	}
}

func seedFollowup(t *testing.T, mux http.Handler, id, regret string) {
	t.Helper()
	w := do(t, mux, http.MethodPost, "/api/decisions/"+id+"/followup", `{"regret":"`+regret+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("造回访 %s 失败: 期望 200，得到 %d (%s)", id, w.Code, w.Body.String())
	}
}

// 空档案必须返回 200 + 空数组：看板在用户还没有任何决策时会打开，
// 返回 null 会让前端 .map() 直接崩掉。
func TestStatsEmptyArchive(t *testing.T) {
	mux, _ := newStatsMux(t)
	w := do(t, mux, http.MethodGet, "/api/decisions/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d (%s)", w.Code, w.Body.String())
	}
	for _, field := range []string{`"byCategory":[]`, `"byMonth":[]`, `"regretted":[]`} {
		if !strings.Contains(w.Body.String(), field) {
			t.Errorf("空档案响应里应有 %s，实际: %s", field, w.Body.String())
		}
	}
	st := decodeStats(t, w)
	if st.Total != 0 || st.RegretRate != 0 {
		t.Errorf("空档案应全 0，得到 %+v", st)
	}
}

// 走真实 HTTP 往返验证聚合结果：造 3 条（1 后悔 / 1 不后悔 / 1 未回访）。
func TestStatsAggregatesOverHTTP(t *testing.T) {
	mux, _ := newStatsMux(t)
	seedDecision(t, mux, "d1", "phone", 10)
	seedDecision(t, mux, "d2", "phone", 10)
	seedDecision(t, mux, "d3", "job", 10)
	seedFollowup(t, mux, "d1", "yes")
	seedFollowup(t, mux, "d2", "no")

	st := decodeStats(t, do(t, mux, http.MethodGet, "/api/decisions/stats", ""))
	if st.Total != 3 || st.Answered != 2 || st.RegretYes != 1 || st.RegretNo != 1 {
		t.Fatalf("聚合不对: %+v", st)
	}
	if st.RegretRate != 0.5 {
		t.Errorf("RegretRate = %v, 期望 0.5", st.RegretRate)
	}
	if len(st.ByCategory) != 2 {
		t.Fatalf("品类桶数 = %d, 期望 2: %+v", len(st.ByCategory), st.ByCategory)
	}
	// phone: 2 条全回访、1 条后悔 → 50%；job 未回访 → 0% 排在后。
	if st.ByCategory[0].Category != "phone" || st.ByCategory[0].RegretRate != 0.5 {
		t.Errorf("phone 桶 = %+v", st.ByCategory[0])
	}
	if st.ByCategory[1].Category != "job" || st.ByCategory[1].RegretRate != 0 {
		t.Errorf("job 桶 = %+v", st.ByCategory[1])
	}
	// 后悔列表里应只有 d1。
	if len(st.Regretted) != 1 || st.Regretted[0].ID != "d1" {
		t.Errorf("后悔列表 = %+v, 期望只有 d1", st.Regretted)
	}
}

// 到期未回访的计入 pending；已回访的超期记录不再算。
func TestStatsPendingCount(t *testing.T) {
	mux, _ := newStatsMux(t)
	seedDecision(t, mux, "old", "phone", 120) // 未回访且超期
	seedDecision(t, mux, "fresh", "phone", 5) // 未回访但未到期
	seedDecision(t, mux, "done", "phone", 200)
	seedFollowup(t, mux, "done", "no") // 超期但已回访

	st := decodeStats(t, do(t, mux, http.MethodGet, "/api/decisions/stats", ""))
	if st.Pending != 1 {
		t.Errorf("Pending = %d, 期望 1（只有 old）", st.Pending)
	}
	if st.Answered != 1 || st.Total != 3 {
		t.Errorf("Answered/Total = %d/%d, 期望 1/3", st.Answered, st.Total)
	}
}

// P8 新增字段必须真的经 HTTP 落盘并回读出来。
func TestStatsCarriesCategory(t *testing.T) {
	mux, s := newStatsMux(t)
	seedDecision(t, mux, "c1", "rent", 3)
	seedFollowup(t, mux, "c1", "yes")

	// 先看落盘层有没有真的存下 category。
	got, ok := s.Get("c1")
	if !ok || got.Category != "rent" {
		t.Fatalf("category 没落盘: %+v (ok=%v)", got, ok)
	}
	st := decodeStats(t, do(t, mux, http.MethodGet, "/api/decisions/stats", ""))
	if len(st.ByCategory) != 1 || st.ByCategory[0].Category != "rent" {
		t.Fatalf("category 没进统计: %+v", st.ByCategory)
	}
	if st.ByCategory[0].RegretRate != 1 {
		t.Errorf("rent 后悔率 = %v, 期望 1", st.ByCategory[0].RegretRate)
	}
}

// 只读端点：非 GET 一律 405。
// 注意走 mux 时这个 405 是 ServeMux 自己生成的（它还会把 HEAD 一起列进 Allow，
// 因为注册了 GET 就自动支持 HEAD），比 handler 里手写的那份更准。
func TestStatsRejectsNonGet(t *testing.T) {
	mux, _ := newStatsMux(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := do(t, mux, m, "/api/decisions/stats", "{}")
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s 期望 405，得到 %d", m, w.Code)
		}
		if allow := w.Header().Get("Allow"); !strings.Contains(allow, "GET") {
			t.Errorf("%s 的 Allow = %q, 应包含 GET", m, allow)
		}
	}
}

// 直接调 handler（绕过 mux）时，自己的 405 分支要给出精确的 Allow: GET。
// 这条同时覆盖上面走 mux 时到不了的那段代码。
func TestStatsHandlerOwnMethodGuard(t *testing.T) {
	_, s := newStatsMux(t)
	h := NewStatsHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/decisions/stats", nil))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("期望 405，得到 %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET" {
		t.Errorf("Allow = %q, 期望 GET", allow)
	}
}

// 未注册路径 404：证明 stats 没把 /api/decisions 的其他子路径吃掉。
func TestStatsUnknownPathIsNotFound(t *testing.T) {
	mux, _ := newStatsMux(t)
	w := do(t, mux, http.MethodGet, "/api/decisions/stats/extra", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，得到 %d", w.Code)
	}
	// 集合端点本身仍要正常，没被 stats 路由抢走。
	if w := do(t, mux, http.MethodGet, "/api/decisions", ""); w.Code != http.StatusOK {
		t.Errorf("/api/decisions 期望 200，得到 %d", w.Code)
	}
}
