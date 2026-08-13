package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/hunter"
	"github.com/grok-free-register/grok-reg/internal/notify"
)

func (s *Server) handleHunterSnapshot(w http.ResponseWriter, _ *http.Request) {
	snap, err := s.hunter.Store.Snapshot(true)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	channels, _ := s.notifyStore().List(true)
	smtp := make([]notify.Channel, 0)
	for _, c := range channels {
		if c.Kind == notify.KindSMTP {
			smtp = append(smtp, c)
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "snapshot": snap, "smtp_channels": smtp, "store": s.hunter.Store.Path(), "local_networks": hunter.LocalNetworkCIDRs()})
}

func (s *Server) handleHunterConfig(w http.ResponseWriter, r *http.Request) {
	var body hunter.Config
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if err := s.hunter.Store.SaveConfig(body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	cfg, _ := s.hunter.Store.Config(true)
	writeJSON(w, 200, map[string]any{"ok": true, "config": cfg})
}

func (s *Server) handleHunterDiscover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sources []string `json:"sources"`
	}
	if err := decodeJSONBody(r, &body); err != nil && err != io.EOF {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	report, err := s.hunter.Discover(ctx, body.Sources)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": len(report.Errors) == 0, "report": report})
}

func (s *Server) handleHunterDiscoverNetwork(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	report, err := s.hunter.DiscoverNetwork(ctx)
	if err != nil {
		writeJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": len(report.Errors) == 0, "report": report})
}

func (s *Server) handleHunterImport(w http.ResponseWriter, r *http.Request) {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	var (
		count int
		err   error
	)
	if strings.Contains(contentType, "text/csv") {
		count, err = s.hunter.ImportCSV(r.Body)
	} else {
		var body struct {
			Items []map[string]interface{} `json:"items"`
		}
		if decodeErr := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); decodeErr != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
			return
		}
		count, err = s.hunter.Import(hunter.ParseLocalJSON(body.Items))
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "imported": count})
}

func (s *Server) handleHunterFindingStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	finding, err := s.hunter.Store.SetFindingStatus(r.PathValue("id"), body.Status)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "finding": finding})
}

func (s *Server) handleHunterProbe(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	finding, err := s.hunter.ProbeFinding(ctx, r.PathValue("id"))
	if err != nil {
		writeJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "finding": finding})
}

func (s *Server) handleHunterDraftCreate(w http.ResponseWriter, r *http.Request) {
	var body hunter.Draft
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	draft, err := s.hunter.Store.CreateDraft(body)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "draft": draft})
}

func (s *Server) handleHunterDraftApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Operator string `json:"operator"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	draft, err := s.hunter.Store.ApproveDraft(r.PathValue("id"), body.Operator)
	if err != nil {
		writeJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "draft": draft})
}

func (s *Server) handleHunterDraftSend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	draft, err := s.hunter.Store.BeginSend(id)
	if err != nil {
		writeJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	channel, err := s.notifyStore().Get(draft.ChannelID, false)
	if err == nil && channel.Kind != notify.KindSMTP {
		err = fmt.Errorf("draft channel must be SMTP")
	}
	if err == nil && !channel.Enabled {
		err = fmt.Errorf("SMTP channel is disabled")
	}
	if err == nil {
		cfg := make(map[string]string, len(channel.Config))
		for k, v := range channel.Config {
			cfg[k] = v
		}
		cfg["to"] = draft.To
		channel.Config = cfg
		err = notify.Send(channel, "hunter.notice", draft.Subject, draft.Body)
	}
	if err != nil {
		_, _ = s.hunter.Store.MarkSendFailed(id, err.Error())
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	draft, err = s.hunter.Store.MarkSent(id)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "mail sent but audit persistence failed: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "draft": draft})
}
