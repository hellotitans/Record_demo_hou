package httpsrv

import (
	"net/http"

	"github.com/yourorg/decision-debate/internal/store"
)

// StatsHandler 暴露决策档案的聚合统计（P8 后悔率看板的数据源）：
//
//	GET /api/decisions/stats → {"stats":{...}}
//
// 路径挂在 /api/decisions 之下而不是另开 /api/stats，
// 是为了让"这是决策档案的统计"在 URL 上就自解释，也避免以后别的统计来抢这个短名字。
//
// 纯只读端点：除 GET 外一律 405 并带 Allow。
type StatsHandler struct {
	store *store.FileStore
}

func NewStatsHandler(s *store.FileStore) *StatsHandler {
	return &StatsHandler{store: s}
}

func (h *StatsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, statsResponse{Stats: h.store.Stats()})
}

// statsResponse 与前端 decisionsApi.fetchStats 的解析形状一致。
type statsResponse struct {
	Stats store.Stats `json:"stats"`
}
