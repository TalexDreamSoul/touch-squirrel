package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/grok-free-register/grok-reg/internal/notify"
)

func (s *Server) notifyStore() *notify.Store {
	path := s.opt.Paths.NotifyFile
	if path == "" {
		path = notify.DefaultPath(s.opt.Paths.Root)
	}
	return notify.New(path)
}

func (s *Server) handleNotifyList(w http.ResponseWriter, r *http.Request) {
	list, err := s.notifyStore().List(true)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":       true,
		"channels": list,
		"events":   notify.KnownEvents,
		"kinds":    []string{notify.KindFeishu, notify.KindSMTP, notify.KindWebhook},
		"store":    s.notifyStore().Path(),
	})
}

func (s *Server) handleNotifyCreate(w http.ResponseWriter, r *http.Request) {
	var body notify.Channel
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	c, err := s.notifyStore().Create(body)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "channel": c})
}

func (s *Server) handleNotifyUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body notify.Channel
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	// decode enabled carefully: if client omits, default keep — but JSON bool false is valid.
	// Frontend always sends enabled.
	c, err := s.notifyStore().Update(id, body)
	if err != nil {
		code := 400
		if strings.Contains(err.Error(), "not found") {
			code = 404
		}
		writeJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "channel": c})
}

func (s *Server) handleNotifyDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.notifyStore().Delete(id); err != nil {
		code := 400
		if strings.Contains(err.Error(), "not found") {
			code = 404
		}
		writeJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.notifyStore().Test(id); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "tested": true})
}
