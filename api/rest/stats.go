package rest

import (
	"encoding/json"
	"net/http"

	"github.com/emicklei/go-restful/v3"
	"github.com/thoas/stats"
)

type StatsFilter struct {
	stats *stats.Stats
}

func (sf *StatsFilter) Filter(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	beginning, recorder := sf.stats.Begin(resp.ResponseWriter)
	defer sf.stats.End(beginning, stats.WithRecorder(recorder))

	chain.ProcessFilter(req, resp)
}

func (sf *StatsFilter) StatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	b, _ := json.Marshal(sf.stats.Data())

	_, _ = w.Write(b)
}

func NewStatsFilter() *StatsFilter {
	return &StatsFilter{
		stats: stats.New(),
	}
}
