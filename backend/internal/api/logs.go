package api

import "sub-store/internal/store"

type LogHandler struct{ db *store.Store }

func (h *LogHandler) get(c *Context) {
	var logs interface{}
	if ok := h.db.ReadInto(store.KeyLogs, &logs); !ok {
		logs = []interface{}{}
	}
	success(c, logs)
}

func (h *LogHandler) clear(c *Context) {
	_ = h.db.Write(store.KeyLogs, []interface{}{})
	successNoData(c)
}
