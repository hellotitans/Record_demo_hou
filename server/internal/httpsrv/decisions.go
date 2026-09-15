package httpsrv

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yourorg/decision-debate/internal/store"
)

// MaxDecisionBytes 限制决策档案请求体大小。档案字段都是短文本，
// 64 KiB 留足了余量，同时挡掉恶意的超大 body。
const MaxDecisionBytes = 1 << 16

// DecisionsHandler 处理集合端点：
//
//	GET  /api/decisions  → 列出全部决策（新→旧）
//	POST /api/decisions  → 创建/更新一条决策（按 id 幂等）
//
// 路由由 Go 1.22+ 的增强 ServeMux 按方法分发，这里只处理已匹配到的请求，
// 未匹配到的方法返回 405。
type DecisionsHandler struct {
	store *store.FileStore
}

func NewDecisionsHandler(s *store.FileStore) *DecisionsHandler {
	return &DecisionsHandler{store: s}
}

func (h *DecisionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.list(w, r)
	case http.MethodPost:
		h.create(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// list 返回 {"decisions":[...]}。空档案返回空数组而不是 null ——
// 前端可以直接 .map()，不必先判空。
func (h *DecisionsHandler) list(w http.ResponseWriter, _ *http.Request) {
	items := h.store.List()
	if items == nil {
		items = []store.DecisionRecord{}
	}
	writeJSON(w, http.StatusOK, decisionsResponse{Decisions: items})
}

// create 写入一条决策。同 id 视为同一场辩论：覆盖并返回 200，
// 首次创建返回 201 —— 前端重试不会长出双胞胎。
func (h *DecisionsHandler) create(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxDecisionBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if len(body) > MaxDecisionBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	var rec store.DecisionRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	// 前端可能漏传 createdAt。不补的话 isDueForFollowup 会把空串解析成 NaN，
	// 结果是"永远不弹回访"——一个查不出来的静默 bug，所以在这里兜住。
	if rec.CreatedAt == "" {
		rec.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, existed := h.store.Get(rec.ID)
	saved, err := h.store.Add(rec)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, decisionResponse{Decision: saved})
}

// DecisionFollowupHandler 处理单条决策的回访：
//
//	POST /api/decisions/{id}/followup  {"regret":"yes"|"no"}
type DecisionFollowupHandler struct {
	store *store.FileStore
}

func NewDecisionFollowupHandler(s *store.FileStore) *DecisionFollowupHandler {
	return &DecisionFollowupHandler{store: s}
}

func (h *DecisionFollowupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "decision id is required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxDecisionBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if len(body) > MaxDecisionBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	var req struct {
		Regret string `json:"regret"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}

	rec, err := h.store.MarkFollowup(id, req.Regret)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decisionResponse{Decision: rec})
}

// --- 响应结构：与前端 web/src/lib/decisionsApi.ts 的解析形状一致 ---

type decisionsResponse struct {
	Decisions []store.DecisionRecord `json:"decisions"`
}

type decisionResponse struct {
	Decision store.DecisionRecord `json:"decision"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// writeStoreError 把存储层的哨兵错误翻译成 HTTP 状态码。
// 业务语义（400/404）只在这里集中映射一次，handler 里不必到处判断。
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalidRecord):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		// 落盘失败等：不把内部路径之类的细节暴露给客户端。
		writeJSONError(w, http.StatusInternalServerError, "internal error")
	}
}
