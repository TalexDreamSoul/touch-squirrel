package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/grok-free-register/grok-reg/internal/acctpool"
	"github.com/grok-free-register/grok-reg/internal/bridge"
	"github.com/grok-free-register/grok-reg/internal/cluster"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/daemon"
	"github.com/grok-free-register/grok-reg/internal/degrade"
	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/hunter"
	"github.com/grok-free-register/grok-reg/internal/jobs"
	"github.com/grok-free-register/grok-reg/internal/localpool"
	"github.com/grok-free-register/grok-reg/internal/patrol"
	"github.com/grok-free-register/grok-reg/internal/plugin"
	"github.com/grok-free-register/grok-reg/internal/runmetrics"
	"github.com/grok-free-register/grok-reg/internal/state"
	"github.com/grok-free-register/grok-reg/internal/statuspage"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
	"github.com/grok-free-register/grok-reg/internal/transfer"
)

// Options configures the panel HTTP server.
type Options struct {
	Paths home.Paths
	Addr  string // e.g. :8787
	Token string // empty = no auth (dev only)
	WebFS fs.FS  // static panel assets (index.html at root)
}

type Server struct {
	opt            Options
	mux            *http.ServeMux
	transfer       *transfer.Service
	patrol         *patrol.Service
	degrade        *degrade.Service
	cluster        *cluster.Service
	status         *statuspage.Service
	localPool      *localpool.Service
	accounts       *acctpool.Store
	hunter         *hunter.Service
	plugins        *plugin.Manager
	market         *plugin.Market
	registerMu     sync.Mutex
	registerCancel context.CancelFunc
}

func New(opt Options) *Server {
	s := &Server{opt: opt, mux: http.NewServeMux()}
	s.plugins = plugin.NewManager(opt.Paths.PluginsDir, opt.Paths.EnabledFile, plugin.ResolveInTreeRoot())
	marketCache := opt.Paths.MarketCache
	if marketCache == "" && opt.Paths.Root != "" {
		marketCache = filepath.Join(opt.Paths.Root, "market-cache")
	}
	s.market = plugin.NewMarket(marketCache, s.plugins)
	s.transfer = transfer.NewService(opt.Paths.ExportsDir, opt.Paths.TmpDir, opt.Paths.UploadCache,
		func() (string, string, transfer.Defaults) {
			cfg, _ := config.Load(opt.Paths.Config)
			return cfg.CPAManagementBase, cfg.CPAManagementKey, transfer.Defaults{
				UploadConcurrency: cfg.UploadConcurrency,
				UploadBatchSize:   cfg.UploadBatchSize,
				ExportBatchSize:   cfg.ExportBatchSize,
				ExportConcurrency: cfg.ExportConcurrency,
				TimeoutMs:         cfg.CPAUploadTimeoutSec * 1000,
				RetryLimit:        cfg.CPAUploadRetries,
			}
		})
	s.patrol = patrol.New(opt.Paths.PatrolState,
		func() config.Config {
			cfg, _ := config.Load(opt.Paths.Config)
			return cfg
		},
		func(cfg config.Config) patrol.ManagementAPI {
			return cpa.NewClient(cfg.CPAManagementBase, cfg.CPAManagementKey, max(cfg.CPAUploadTimeoutSec, 30))
		},
		func(target int) error {
			_, _, _, err := s.ensurePipelineStart(target)
			return err
		})
	s.patrol.SetPipelineChecker(s.pipelineRunning)
	degradeState := opt.Paths.DegradeState
	if degradeState == "" && opt.Paths.Root != "" {
		degradeState = filepath.Join(opt.Paths.Root, "degrade-state.json")
	}
	s.degrade = degrade.New(degradeState,
		func() config.Config {
			cfg, _ := config.Load(opt.Paths.Config)
			return cfg
		},
		func(cfg config.Config) degrade.ManagementAPI {
			return cpa.NewClient(cfg.CPAManagementBase, cfg.CPAManagementKey, max(cfg.CPAUploadTimeoutSec, 30))
		})
	s.cluster = cluster.New(opt.Paths.ClusterState, func() config.Config {
		cfg, _ := config.Load(opt.Paths.Config)
		return cfg
	})
	s.cluster.SetPoolProvider(func() cluster.PoolSnapshot {
		o := s.patrol.Overview()
		return cluster.PoolSnapshot{
			Healthy:       o.Healthy,
			RateLimited:   o.RateLimited,
			Dead:          o.Dead,
			Disabled:      o.Disabled,
			Total:         o.Total,
			QuotaEstimate: o.QuotaEstimate,
		}
	})
	s.cluster.SetStartFn(func(target int) error {
		_, _, _, err := s.ensurePipelineStart(target)
		return err
	})
	s.cluster.SetRunningFn(s.pipelineRunning)
	s.cluster.SetUploadFn(func() (int, int, error) {
		// Best-effort: slaves still use panel CPA auto-upload when enabled;
		// report zeros here — full batch upload is via grok upload / transfer.
		return 0, 0, nil
	})
	s.status = statuspage.New(opt.Paths.StatusLayout, func() config.Config {
		cfg, _ := config.Load(opt.Paths.Config)
		return cfg
	}, opt.Paths.Outputs)
	s.status.SetPoolLister(func() ([]cpa.AuthMeta, error) {
		cfg, _ := config.Load(opt.Paths.Config)
		return cpa.NewClient(cfg.CPAManagementBase, cfg.CPAManagementKey, max(cfg.CPAUploadTimeoutSec, 30)).List()
	})
	s.status.SetPatrolSnap(func() (healthy, rateLimited, dead, disabled, total int) {
		o := s.patrol.Overview()
		return o.Healthy, o.RateLimited, o.Dead, o.Disabled, o.Total
	})
	s.status.SetClusterSnap(func() map[string]any {
		st := s.cluster.Status()
		return map[string]any{
			"role":          st.Role,
			"need":          st.Need,
			"pool_target":   st.PoolTarget,
			"slaves_online": len(filterOnline(st.Nodes)),
			"slaves_total":  len(st.Nodes),
			"nodes":         st.Nodes,
		}
	})
	s.localPool = localpool.New(opt.Paths.LocalPool)
	hunterPath := opt.Paths.HunterFile
	if hunterPath == "" {
		hunterPath = filepath.Join(opt.Paths.Root, "hunter.json")
	}
	s.hunter = hunter.NewService(hunterPath)
	if st, err := acctpool.Open(opt.Paths.AccountsDB); err == nil {
		s.accounts = st
		// one-shot auto-migration of legacy local-pool + tavily keys
		_, _ = st.AutoMigrate(acctpool.MigrateOptions{
			LocalPoolDir:   opt.Paths.LocalPool,
			TavilyKeysPath: tavilypool.DefaultStatePath(opt.Paths.Root),
		})
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.withAuth(s.withCORS(s.mux))
}

func (s *Server) ListenAndServe() error {
	if err := s.opt.Paths.EnsureBase(); err != nil {
		return err
	}
	// ensure default config exists for first boot
	if _, err := os.Stat(s.opt.Paths.Config); os.IsNotExist(err) {
		cfg := config.Defaults()
		// Docker-friendly defaults when env hints present
		if v := os.Getenv("REGISTER_PROXY"); v != "" {
			cfg.RegisterProxy = v
			cfg.HTTPProxy = v
			cfg.HTTPSProxy = v
		}
		if v := os.Getenv("FLARESOLVERR_URL"); v != "" {
			cfg.FlareSolverrURL = v
		}
		if v := os.Getenv("CLEARANCE_PROXY"); v != "" {
			cfg.ClearanceProxy = v
		}
		if v := os.Getenv("CLEARANCE_ENABLED"); v != "" {
			cfg.ClearanceEnabled = v == "1" || strings.EqualFold(v, "true")
		}
		_ = config.Save(s.opt.Paths.Config, cfg)
	}
	srv := &http.Server{
		Addr:              s.opt.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Background job pruning (upload 2h / export 7d TTL) + pool patrol loop.
	bgCtx, stopBg := context.WithCancel(context.Background())
	defer stopBg()
	s.transfer.UploadJobs.StartPruner(bgCtx, 15*time.Minute)
	s.transfer.ExportJobs.StartPruner(bgCtx, 15*time.Minute)
	s.patrol.Start(bgCtx)
	s.cluster.Start(bgCtx)
	s.status.Start(bgCtx)

	// Graceful shutdown: cancel running jobs, flush the upload cache.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		s.transfer.UploadJobs.CancelAll()
		s.transfer.ExportJobs.CancelAll()
		s.transfer.Cache.Flush()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return nil
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("POST /api/start", s.handleStart)
	s.mux.HandleFunc("POST /api/stop", s.handleStop)
	s.mux.HandleFunc("GET /api/logs", s.handleLogs)
	s.mux.HandleFunc("GET /api/runs", s.handleRuns)
	s.mux.HandleFunc("GET /api/register-metrics", s.handleRegisterMetrics)
	s.mux.HandleFunc("GET /api/runs/{id}/files", s.handleRunFiles)
	s.mux.HandleFunc("GET /api/runs/{id}/log", s.handleRunLog)
	s.mux.HandleFunc("GET /api/runs/{id}/metrics", s.handleRunMetrics)
	s.mux.HandleFunc("DELETE /api/runs/{id}", s.handleRunDelete)
	s.mux.HandleFunc("GET /api/runs/{id}/download", s.handleDownload)
	s.mux.HandleFunc("GET /api/runs/{id}/file", s.handleFile)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	s.mux.HandleFunc("GET /api/infrastructure/export", s.handleInfrastructureExport)
	s.mux.HandleFunc("POST /api/infrastructure/import", s.handleInfrastructureImport)

	// transfer: batch upload
	s.mux.HandleFunc("POST /api/transfer/prepare", s.handleTransferPrepare)
	s.mux.HandleFunc("GET /api/transfer/jobs", s.handleTransferJobs)
	s.mux.HandleFunc("GET /api/transfer/jobs/{id}", s.handleTransferJobGet)
	s.mux.HandleFunc("POST /api/transfer/jobs/{id}/start", s.handleTransferJobStart)
	s.mux.HandleFunc("POST /api/transfer/jobs/{id}/retry-failed", s.handleTransferJobRetryFailed)
	s.mux.HandleFunc("POST /api/transfer/jobs/{id}/cancel", s.handleTransferJobCancel)
	s.mux.HandleFunc("GET /api/transfer/jobs/{id}/events", s.handleTransferJobEvents)
	s.mux.HandleFunc("GET /api/transfer/cache", s.handleTransferCacheGet)
	s.mux.HandleFunc("DELETE /api/transfer/cache", s.handleTransferCacheDelete)

	// transfer: batch export
	s.mux.HandleFunc("POST /api/export/preview", s.handleExportPreview)
	s.mux.HandleFunc("POST /api/export/start", s.handleExportStart)
	s.mux.HandleFunc("GET /api/export/jobs", s.handleExportJobs)
	s.mux.HandleFunc("GET /api/export/jobs/{id}", s.handleExportJobGet)
	s.mux.HandleFunc("GET /api/export/jobs/{id}/events", s.handleExportJobEvents)
	s.mux.HandleFunc("POST /api/export/jobs/{id}/cancel", s.handleExportJobCancel)
	s.mux.HandleFunc("POST /api/export/jobs/{id}/retry-failed", s.handleExportJobRetryFailed)
	s.mux.HandleFunc("GET /api/export/jobs/{id}/parts/{filename}", s.handleExportPart)
	s.mux.HandleFunc("GET /api/export/jobs/{id}/download-all", s.handleExportDownloadAll)

	// pool: remote list + connectivity + patrol
	s.mux.HandleFunc("POST /api/pool/test-connection", s.handlePoolTestConnection)
	s.mux.HandleFunc("GET /api/pool/files", s.handlePoolFiles)
	s.mux.HandleFunc("GET /api/pool/list", s.handleUnifiedPoolList)
	s.mux.HandleFunc("GET /api/pool/pull", s.handleUnifiedPoolPull)
	s.mux.HandleFunc("GET /api/pool/overview", s.handlePoolOverview)
	s.mux.HandleFunc("POST /api/pool/patrol", s.handlePoolPatrol)
	s.mux.HandleFunc("GET /api/pool/patrol/history", s.handlePoolPatrolHistory)
	s.mux.HandleFunc("GET /api/pool/logs", s.handlePoolLogs)
	s.mux.HandleFunc("GET /api/degrade/overview", s.handleDegradeOverview)
	s.mux.HandleFunc("GET /api/degrade/accounts", s.handleDegradeAccounts)
	s.mux.HandleFunc("POST /api/degrade/scan", s.handleDegradeScan)
	s.mux.HandleFunc("POST /api/degrade/isolate", s.handleDegradeIsolate)
	s.mux.HandleFunc("POST /api/pool/cleanup", s.handlePoolCleanup)

	// cluster / federation (master–slave)
	// Public federation endpoints: auth via CLUSTER_PUBLIC_TOKEN (optional), not PANEL_TOKEN.
	s.mux.HandleFunc("GET /api/federation/info", s.handleFederationInfo)
	s.mux.HandleFunc("GET /api/federation/infrastructure", s.handleFederationInfrastructure)
	s.mux.HandleFunc("POST /api/federation/heartbeat", s.handleFederationHeartbeat)
	s.mux.HandleFunc("POST /api/federation/report", s.handleFederationReport)
	s.mux.HandleFunc("GET /api/federation/pool", s.handleFederationPoolList)
	s.mux.HandleFunc("GET /api/federation/pool/pull", s.handleFederationPoolPull)
	s.mux.HandleFunc("GET /api/public/status", s.handlePublicStatus)
	s.mux.HandleFunc("POST /api/public/status", s.handlePublicStatus)
	s.mux.HandleFunc("GET /api/public/status.json", s.handlePublicStatusJSON)
	s.mux.HandleFunc("GET /api/status/layout", s.handleStatusLayoutGet)
	s.mux.HandleFunc("PUT /api/status/layout", s.handleStatusLayoutPut)
	s.mux.HandleFunc("POST /api/status/probe-now", s.handleStatusProbeNow)
	// Admin (panel token)
	s.mux.HandleFunc("GET /api/cluster/status", s.handleClusterStatus)
	s.mux.HandleFunc("POST /api/cluster/kick", s.handleClusterKick)

	// local credential pool (register results)
	s.mux.HandleFunc("GET /api/local-pool", s.handleLocalPoolList)
	s.mux.HandleFunc("POST /api/local-pool/import", s.handleLocalPoolImport)
	s.mux.HandleFunc("POST /api/local-pool/sync", s.handleLocalPoolSync)

	// touch-squirrel host: plugins / artifacts / tavily key pool / notifications
	s.mux.HandleFunc("GET /api/plugins", s.handlePluginsList)
	s.mux.HandleFunc("POST /api/plugins/{id}/enable", s.handlePluginEnable)
	s.mux.HandleFunc("POST /api/plugins/{id}/disable", s.handlePluginDisable)
	s.mux.HandleFunc("GET /api/plugin-repositories", s.handlePluginRepositoriesList)
	s.mux.HandleFunc("POST /api/plugin-repositories", s.handlePluginRepositoryAdd)
	s.mux.HandleFunc("DELETE /api/plugin-repositories/{id}", s.handlePluginRepositoryDelete)
	s.mux.HandleFunc("POST /api/plugin-repositories/{id}/sync", s.handlePluginRepositorySync)
	s.mux.HandleFunc("POST /api/plugin-repositories/sync", s.handlePluginRepositoriesSync)
	s.mux.HandleFunc("GET /api/plugin-market", s.handlePluginMarketList)
	s.mux.HandleFunc("POST /api/plugin-market/{repository}/plugins/{id}/install", s.handlePluginMarketInstall)
	s.mux.HandleFunc("GET /api/artifacts", s.handleArtifactsList)
	s.mux.HandleFunc("GET /api/tavily/keys", s.handleTavilyKeysList)
	s.mux.HandleFunc("POST /api/tavily/keys", s.handleTavilyKeysAdd)
	s.mux.HandleFunc("POST /api/tavily/keys/{id}/status", s.handleTavilyKeyStatus)

	// host notifications (feishu / smtp / webhook)
	s.mux.HandleFunc("GET /api/notifications", s.handleNotifyList)
	s.mux.HandleFunc("POST /api/notifications", s.handleNotifyCreate)
	s.mux.HandleFunc("PUT /api/notifications/{id}", s.handleNotifyUpdate)
	s.mux.HandleFunc("DELETE /api/notifications/{id}", s.handleNotifyDelete)
	s.mux.HandleFunc("POST /api/notifications/{id}/test", s.handleNotifyTest)

	// hunter: passive discovery, authorized read-only probes, reviewed disclosure mail
	s.mux.HandleFunc("GET /api/hunter", s.handleHunterSnapshot)
	s.mux.HandleFunc("PUT /api/hunter/config", s.handleHunterConfig)
	s.mux.HandleFunc("POST /api/hunter/discover", s.handleHunterDiscover)
	s.mux.HandleFunc("POST /api/hunter/discover-network", s.handleHunterDiscoverNetwork)
	s.mux.HandleFunc("POST /api/hunter/import", s.handleHunterImport)
	s.mux.HandleFunc("PUT /api/hunter/findings/{id}/status", s.handleHunterFindingStatus)
	s.mux.HandleFunc("POST /api/hunter/findings/{id}/probe", s.handleHunterProbe)
	s.mux.HandleFunc("POST /api/hunter/drafts", s.handleHunterDraftCreate)
	s.mux.HandleFunc("POST /api/hunter/drafts/{id}/approve", s.handleHunterDraftApprove)
	s.mux.HandleFunc("POST /api/hunter/drafts/{id}/send", s.handleHunterDraftSend)

	if s.opt.WebFS != nil {
		// Next export lives under out/ inside embed.FS
		staticRoot := s.opt.WebFS
		if sub, err := fs.Sub(s.opt.WebFS, "out"); err == nil {
			staticRoot = sub
		}
		s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" || path == "/" {
				path = "index.html"
			}
			// Prefer explicit files over FileServer redirects (Next uses trailingSlash).
			candidates := []string{path}
			if strings.HasSuffix(path, "/") {
				candidates = append(candidates, path+"index.html")
			} else {
				candidates = append(candidates, path+"/index.html", path+".html")
			}
			// _next static assets keep exact path
			for _, candidate := range candidates {
				candidate = strings.TrimPrefix(candidate, "/")
				data, err := fs.ReadFile(staticRoot, candidate)
				if err != nil {
					continue
				}
				ctype := contentTypeFor(candidate)
				w.Header().Set("Content-Type", ctype)
				_, _ = w.Write(data)
				return
			}
			// SPA fallback
			data, err := fs.ReadFile(staticRoot, "index.html")
			if err != nil {
				http.Error(w, "panel missing", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
		})
	} else {
		s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("touch-squirrel panel API up. Mount web assets or open /api/health\n"))
		})
	}
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Panel-Token, X-Status-Password, X-Cluster-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// health + static panel assets always open so the login form can load.
		// Federation endpoints use optional CLUSTER_PUBLIC_TOKEN instead of PANEL_TOKEN.
		if r.URL.Path == "/api/health" ||
			strings.HasPrefix(r.URL.Path, "/api/federation/") ||
			r.URL.Path == "/api/public/status" ||
			r.URL.Path == "/api/public/status.json" ||
			!strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if s.opt.Token == "" {
			if strings.HasPrefix(r.URL.Path, "/api/hunter") && !isLoopbackRemote(r.RemoteAddr) {
				writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "hunter API requires PANEL_TOKEN for non-loopback access"})
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		tok := extractToken(r)
		if tok == "" || tok != s.opt.Token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"ok":    false,
				"error": "unauthorized",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("X-Panel-Token"); h != "" {
		return strings.TrimSpace(h)
	}
	if h := r.Header.Get("Authorization"); h != "" {
		h = strings.TrimSpace(h)
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return h
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	upTotal, upRunning := s.transfer.UploadJobs.Counts()
	exTotal, exRunning := s.transfer.ExportJobs.Counts()
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"service": "touch-squirrel-panel",
		"time":    time.Now().UTC().Format(time.RFC3339),
		"auth":    s.opt.Token != "",
		"jobs": map[string]any{
			"upload": map[string]int{"total": upTotal, "running": upRunning},
			"export": map[string]int{"total": exTotal, "running": exRunning},
		},
	})
}

// decodeJSONBody decodes a bounded JSON request body; empty bodies are OK.
func decodeJSONBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func (s *Server) reconcile(snap state.Snapshot) state.Snapshot {
	if snap.Status == state.StatusRunning {
		pid := snap.PID
		if pid == 0 {
			if p, err := daemon.ReadPID(s.opt.Paths.PID); err == nil {
				pid = p
				snap.PID = p
			}
		}
		if pid != 0 && !daemon.PIDAlive(pid) {
			snap.Status = state.StatusFailed
			if snap.Error == "" {
				snap.Error = "进程意外退出但未写入终态"
			}
			snap.PhaseDetail = "意外终止"
			snap.PID = 0
		}
	}
	if snap.Status == state.StatusStopped {
		detail := strings.TrimSpace(snap.PhaseDetail)
		switch {
		case snap.Error != "":
			snap.Status = state.StatusFailed
		case strings.Contains(detail, "手动停止") || strings.Contains(detail, "手动终止"):
			snap.Status = state.StatusCancelled
			snap.Error = firstNonEmpty(snap.Error, "用户手动终止")
		case strings.HasPrefix(detail, "完成"):
			snap.Status = state.StatusCompleted
		}
	}
	return snap
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := state.NewStore(s.opt.Paths.State)
	snap, err := st.Load()
	if err != nil && !os.IsNotExist(err) {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if os.IsNotExist(err) {
		snap = state.Snapshot{Status: state.StatusStopped}
	}
	reconciled := s.reconcile(snap)
	if reconciled != snap {
		_ = st.Set(func(current *state.Snapshot) { *current = reconciled })
	}
	writeJSON(w, 200, map[string]any{"ok": true, "status": reconciled})
}

type startReq struct {
	Target int            `json:"target"`
	Plugin string         `json:"plugin"` // registrar plugin id; default xai-accounts
	Type   string         `json:"type"`   // account type alias: xai|tavily
	Config map[string]any `json:"config"` // plugin-specific config (bridge plugins)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if req.Target <= 0 {
		req.Target = 10
	}
	target, err := config.ClampTarget(req.Target)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	pluginID := strings.TrimSpace(req.Plugin)
	if pluginID == "" {
		switch strings.ToLower(strings.TrimSpace(req.Type)) {
		case "tavily", "tavily-registrar":
			pluginID = "tavily-registrar"
		case "xai", "xai-accounts", "":
			pluginID = "xai-accounts"
		default:
			pluginID = strings.TrimSpace(req.Type)
		}
	}
	if pluginID == "" {
		pluginID = "xai-accounts"
	}

	// Resolve plugin manifest and dispatch by runtime.
	mgr := plugin.NewManager(s.opt.Paths.PluginsDir, s.opt.Paths.EnabledFile, plugin.ResolveInTreeRoot())
	it, err := mgr.Get(pluginID)
	if err != nil {
		writeJSON(w, 400, map[string]any{
			"ok":     false,
			"error":  fmt.Sprintf("插件 %s 未找到", pluginID),
			"plugin": pluginID,
		})
		return
	}
	if !it.Enabled {
		writeJSON(w, 400, map[string]any{
			"ok":     false,
			"error":  fmt.Sprintf("插件 %s 未启用", pluginID),
			"plugin": pluginID,
		})
		return
	}
	if !it.Manifest.HasKind(plugin.KindRegistrar) {
		writeJSON(w, 400, map[string]any{
			"ok":     false,
			"error":  fmt.Sprintf("插件 %s 不是 registrar", pluginID),
			"plugin": pluginID,
		})
		return
	}

	switch pluginID {
	case "xai-accounts":
		s.handleXAIStart(w, r, target, pluginID)
		return
	default:
		if it.Manifest.Runtime == plugin.RuntimeBridge && it.Manifest.Entry.Bridge != "" {
			s.handleBridgeStart(w, r, target, pluginID, it, req.Config)
			return
		}
		writeJSON(w, 400, map[string]any{
			"ok":     false,
			"error":  fmt.Sprintf("注册类型 %s 暂未接入流水线（runtime=%s）", pluginID, it.Manifest.Runtime),
			"plugin": pluginID,
		})
		return
	}
}

// handleXAIStart starts the legacy xai-accounts pipeline (backward compat).
func (s *Server) handleXAIStart(w http.ResponseWriter, r *http.Request, target int, pluginID string) {
	runID, pid, logPath, err := s.ensurePipelineStart(target)
	if err != nil {
		code := 500
		if strings.Contains(err.Error(), "already running") {
			code = 409
		}
		writeJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"run_id": runID,
		"pid":    pid,
		"target": target,
		"plugin": pluginID,
		"type":   "xai",
		"log":    logPath,
		"output": filepath.Join(s.opt.Paths.Outputs, runID),
	})
}

// handleBridgeStart starts a bridge-type plugin subprocess as a tracked job.
func (s *Server) handleBridgeStart(w http.ResponseWriter, r *http.Request, target int, pluginID string, it plugin.Installed, pluginCfg map[string]any) {
	if s.pipelineRunning() {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "已有注册任务运行中"})
		return
	}

	runID := home.NewRunID()
	outputDir := filepath.Join(s.opt.Paths.Outputs, runID)
	logPath := filepath.Join(s.opt.Paths.LogsDir, fmt.Sprintf("run-%s.log", runID))
	_ = os.MkdirAll(s.opt.Paths.LogsDir, 0o700)
	_ = os.MkdirAll(outputDir, 0o700)

	bridgePath := filepath.Join(it.Root, it.Manifest.Entry.Bridge)

	// Build env: inherit host config for BitBrowser, Clash, captcha keys, and
	// bridge roots (grok-register-panel / reg-factory checkouts).
	cfg, _ := config.Load(s.opt.Paths.Config)
	metrics := runmetrics.New(outputDir, runID, pluginID, runmetrics.NewEnvironment(string(cfg.EmailMode), cfg.TurnstileProvider, cfg.ResinPlatform, cfg.RegisterProxy))
	go metrics.SetEgressIP(runmetrics.DetectEgressIP(cfg.RegisterProxy, 3*time.Second))
	st := state.NewStore(s.opt.Paths.State)
	_ = st.Set(func(snapshot *state.Snapshot) {
		*snapshot = state.Snapshot{
			Status: state.StatusRunning, RunID: runID, Plugin: pluginID, Target: target,
			Phase: state.PhaseIdle, PhaseDetail: fmt.Sprintf("bridge: %s", pluginID),
			LogPath: logPath, OutputDir: outputDir, StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
	})
	bridgeCtx, cancelBridge := context.WithCancel(context.Background())
	s.registerMu.Lock()
	s.registerCancel = cancelBridge
	s.registerMu.Unlock()
	bridgeEnv := map[string]string{}
	if pool := firstNonEmpty(cfg.BridgeOutlookPoolDir, os.Getenv("OUTLOOK_POOL_DIR")); pool != "" {
		bridgeEnv["OUTLOOK_POOL_DIR"] = pool
	}
	if root := firstNonEmpty(cfg.BridgeRegFactoryRoot, os.Getenv("REG_FACTORY_ROOT")); root != "" {
		bridgeEnv["REG_FACTORY_ROOT"] = root
	}
	if root := firstNonEmpty(cfg.BridgeGrokPanelRoot, os.Getenv("GROK_PANEL_ROOT")); root != "" {
		bridgeEnv["GROK_PANEL_ROOT"] = root
	}
	if cfg.CPAManagementKey != "" {
		bridgeEnv["CPA_MGMT_KEY"] = cfg.CPAManagementKey
	}

	// 平台能力注入：邮箱 provider / MailRouter / Resin。
	// 平台 config.env 统一管理这些能力，新建任务时透传给 bridge 子进程，
	// runner.py 据此调用平台能力（而非 reg-factory 自己的 .env）。
	setEnv := func(k, v string) {
		if v != "" {
			bridgeEnv[k] = v
		}
	}
	// 邮箱 provider（用 reg-factory 认识的 env key）
	setEnv("TEMP_EMAIL_PROVIDER", string(cfg.EmailMode))
	setEnv("EMAIL_MODE", string(cfg.EmailMode))
	setEnv("YYDS_API_KEY", cfg.YYDSKey)
	setEnv("YYDS_DEFAULT_DOMAIN", cfg.YYDSDomain)
	setEnv("MOEMAIL_API_KEY", cfg.MoeMailKey)
	setEnv("MOEMAIL_BASE_URL", cfg.MoeMailBase)
	setEnv("MOEMAIL_DOMAIN", cfg.MoeMailDomain)
	setEnv("MOEMAIL_EXPIRY_MS", strconv.FormatInt(cfg.MoeMailExpiryMS, 10))
	setEnv("DUCKMAIL_API_KEY", cfg.DuckMailKey)
	setEnv("DUCKMAIL_API_BASE", cfg.DuckMailBase)
	setEnv("MAILNEST_API_KEY", cfg.MailNestKey)
	setEnv("MAILNEST_PROJECT_CODE", cfg.MailNestProjectCode)
	setEnv("CLOUDMAIL_PASSWORD", cfg.CloudMailPassword)
	setEnv("CLOUDFLARE_API_KEY", cfg.CloudflareKey)
	setEnv("CLOUDFLARE_CUSTOM_AUTH", cfg.CloudflareCustomAuth)

	// MailRouter（平台统一邮箱路由）
	setEnv("MAIL_ROUTER_URL", cfg.MailRouterURL)
	setEnv("MAIL_ROUTER_API_KEY", cfg.MailRouterAPIKey)
	setEnv("MAIL_ROUTER_DOMAIN", cfg.MailRouterDomain)

	// Resin（平台统一代理出口）
	setEnv("RESIN_PROXY", cfg.ResinProxy)
	setEnv("RESIN_TOKEN", cfg.ResinToken)
	setEnv("RESIN_PLATFORM", cfg.ResinPlatform)

	// Pass through common captcha envs.
	for _, k := range []string{
		"YESCAPTCHA_API_KEY", "YESCAPTCHA_API_BASE",
		"CAPSOLVER_API_KEY", "EZCAPTCHA_API_KEY", "EZCAPTCHA_API_BASE",
		"BITBROWSER_API", "CLASH_PROXY", "CLASH_SECRET", "CLASH_API",
		"FINGERPRINT_BROWSER",
	} {
		if v := os.Getenv(k); v != "" {
			bridgeEnv[k] = v
		}
	}

	// Spawn bridge as a tracked background job.
	if pluginCfg == nil {
		pluginCfg = map[string]any{}
	}
	bridgeMgr := jobs.NewManager(pluginID, 24*time.Hour)
	go func() {
		defer func() {
			s.registerMu.Lock()
			s.registerCancel = nil
			s.registerMu.Unlock()
		}()
		result, runErr := bridge.Run(bridgeCtx, bridgeMgr, bridge.Config{
			PluginID:    pluginID,
			BridgePath:  bridgePath,
			PythonExe:   firstNonEmpty(cfg.BridgePythonExe, os.Getenv("GROK_PYTHON")),
			Target:      target,
			PluginCfg:   pluginCfg,
			Env:         bridgeEnv,
			OutputDir:   outputDir,
			ArtifactDir: filepath.Join(s.opt.Paths.Root, "artifacts"),
			LogPath:     logPath,
			Metrics:     metrics,
		})
		terminalErr := runErr
		if terminalErr == nil {
			terminalErr = result.Error
		}
		terminalStatus := state.StatusCompleted
		metricStatus := "completed"
		phaseDetail := fmt.Sprintf("完成 %d/%d", result.OK, result.Total)
		if terminalErr != nil {
			terminalStatus = state.StatusFailed
			metricStatus = "failed"
			phaseDetail = "意外终止"
			if terminalErr == context.Canceled {
				terminalStatus = state.StatusCancelled
				metricStatus = "cancelled"
				phaseDetail = "用户手动终止"
			}
		}
		metrics.Finish(metricStatus, terminalErr)
		_ = state.NewStore(s.opt.Paths.State).Set(func(snapshot *state.Snapshot) {
			snapshot.Status = terminalStatus
			snapshot.Done = result.OK + result.Fail
			snapshot.FailCount = result.Fail
			snapshot.Phase = state.PhaseIdle
			snapshot.PhaseDetail = phaseDetail
			if terminalErr != nil {
				snapshot.Error = terminalErr.Error()
			}
		})
		if terminalErr != nil {
			msg := fmt.Sprintf("bridge %s run failed: %v\n", pluginID, terminalErr)
			_, _ = fmt.Fprint(os.Stderr, msg)
			if f, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); ferr == nil {
				_, _ = f.WriteString(msg)
				_ = f.Close()
			}
		}
	}()

	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"run_id": runID,
		"target": target,
		"plugin": pluginID,
		"type":   pluginID,
		"log":    logPath,
		"output": outputDir,
	})
}

// ensurePipelineStart starts the detached registration worker. Shared by the
// manual /api/start endpoint and the auto-refill controller.
func (s *Server) ensurePipelineStart(target int) (runID string, pid int, logPath string, err error) {
	if p, err := daemon.ReadPID(s.opt.Paths.PID); err == nil && daemon.PIDAlive(p) {
		return "", 0, "", fmt.Errorf("already running (pid %d)", p)
	}

	if _, err := os.Stat(s.opt.Paths.Config); os.IsNotExist(err) {
		_ = config.Save(s.opt.Paths.Config, config.Defaults())
	}

	runID = home.NewRunID()
	_ = os.MkdirAll(s.opt.Paths.LogsDir, 0o700)
	logPath = filepath.Join(s.opt.Paths.LogsDir, fmt.Sprintf("run-%s.log", runID))

	st := state.NewStore(s.opt.Paths.State)
	_ = st.Set(func(snap *state.Snapshot) {
		*snap = state.Snapshot{
			Status: state.StatusRunning, RunID: runID, Plugin: "xai-accounts", Target: target,
			Phase: state.PhaseIdle, PhaseDetail: "启动中", LogPath: logPath,
			OutputDir: filepath.Join(s.opt.Paths.Outputs, runID), StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
	})

	pid, err = daemon.StartBackground(target, runID)
	if err != nil {
		_ = st.Set(func(snap *state.Snapshot) {
			snap.Status = state.StatusFailed
			snap.Error = err.Error()
			snap.PhaseDetail = "启动失败"
		})
		return "", 0, "", err
	}
	_ = daemon.WritePID(s.opt.Paths.PID, pid)
	_ = st.Set(func(snap *state.Snapshot) { snap.PID = pid })
	return runID, pid, logPath, nil
}

// pipelineRunning reports whether the registration worker is alive.
func (s *Server) pipelineRunning() bool {
	if p, err := daemon.ReadPID(s.opt.Paths.PID); err == nil && daemon.PIDAlive(p) {
		return true
	}
	store := state.NewStore(s.opt.Paths.State)
	snapshot, err := store.Load()
	if err != nil {
		return false
	}
	reconciled := s.reconcile(snapshot)
	if reconciled != snapshot {
		_ = store.Set(func(current *state.Snapshot) { *current = reconciled })
	}
	return reconciled.Status == state.StatusRunning
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	stopErr := daemon.Stop(s.opt.Paths)
	if stopErr != nil && !strings.Contains(stopErr.Error(), "未在运行") {
		writeJSON(w, 400, map[string]any{"ok": false, "error": stopErr.Error()})
		return
	}
	if stopErr != nil {
		s.registerMu.Lock()
		cancel := s.registerCancel
		s.registerMu.Unlock()
		if cancel == nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "任务已不在运行"})
			return
		}
		cancel()
	}
	st := state.NewStore(s.opt.Paths.State)
	_ = st.Set(func(snap *state.Snapshot) {
		snap.Status = state.StatusCancelled
		snap.Phase = state.PhaseIdle
		snap.PhaseDetail = "用户手动终止"
		snap.Error = "用户手动终止"
		snap.PID = 0
	})
	go s.autoImportLatestRun("")
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"
	tailN := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tailN = n
		}
	}

	st := state.NewStore(s.opt.Paths.State)
	snap, _ := st.Load()
	path := snap.LogPath
	if path == "" {
		path = latestLog(s.opt.Paths.LogsDir)
	}
	if path == "" {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "no log file"})
		return
	}

	if !follow {
		text, err := readLogTail(path, tailN)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "path": path, "log": text})
		return
	}

	// SSE stream
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}

	var offset int64
	if fi, err := os.Stat(path); err == nil {
		// start near end
		offset = fi.Size() - 8192
		if offset < 0 {
			offset = 0
		}
	}

	ctx := r.Context()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	// initial comment
	_, _ = fmt.Fprintf(w, ": connected path=%s\n\n", path)
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				_ = f.Close()
				continue
			}
			buf, err := io.ReadAll(f)
			_ = f.Close()
			if len(buf) == 0 {
				// heartbeat
				_, _ = fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
				continue
			}
			offset += int64(len(buf))
			// SSE data lines
			for _, line := range strings.Split(string(buf), "\n") {
				_, _ = fmt.Fprintf(w, "data: %s\n", line)
			}
			_, _ = fmt.Fprintf(w, "\n")
			flusher.Flush()
			_ = err
		}
	}
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePage(r, 1, 10, 100)
	dirs, err := cpa.ListRunDirs(s.opt.Paths.Outputs, 0)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	total := len(dirs)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	pageDirs := dirs[start:end]

	importedByRun := map[string]int{}
	for _, entry := range s.localPool.List() {
		if entry.SourceRun != "" {
			importedByRun[entry.SourceRun]++
		}
	}
	current, _ := state.NewStore(s.opt.Paths.State).Load()
	type runInfo struct {
		ID                 string `json:"id"`
		Path               string `json:"path"`
		Plugin             string `json:"plugin"`
		Status             string `json:"status"`
		Phase              string `json:"phase,omitempty"`
		PhaseDetail        string `json:"phase_detail,omitempty"`
		Error              string `json:"error,omitempty"`
		Target             int    `json:"target,omitempty"`
		Done               int    `json:"done,omitempty"`
		FailCount          int    `json:"fail_count,omitempty"`
		CPACount           int    `json:"cpa_count"`
		SSOFiles           int    `json:"sso_files"`
		ImportedCount      int    `json:"imported_count"`
		StartedAt          string `json:"started_at,omitempty"`
		UpdatedAt          string `json:"updated_at,omitempty"`
		ModTime            string `json:"mod_time"`
		DurationMS         int64  `json:"duration_ms,omitempty"`
		AverageAccountMS   int64  `json:"average_account_ms,omitempty"`
		AccountMetricCount int    `json:"account_metric_count,omitempty"`
	}
	out := make([]runInfo, 0, len(pageDirs))
	for _, dir := range pageDirs {
		id := filepath.Base(dir)
		files, _ := cpa.CollectCPAJSON(dir)
		ssoN := 0
		if entries, readErr := os.ReadDir(filepath.Join(dir, "SSO")); readErr == nil {
			ssoN = len(entries)
		}
		modTime := ""
		if info, statErr := os.Stat(dir); statErr == nil {
			modTime = info.ModTime().UTC().Format(time.RFC3339)
		}
		snapshot, _ := state.LoadRun(dir)
		if current.RunID == id && (snapshot.RunID == "" || current.UpdatedAt > snapshot.UpdatedAt) {
			snapshot = current
		}
		pluginID := snapshot.Plugin
		if pluginID == "" && (len(files) > 0 || ssoN > 0) {
			pluginID = "xai-accounts"
		}
		status := string(snapshot.Status)
		if status == "" || status == string(state.StatusStopped) {
			status = "unknown"
		}
		durationMS, averageAccountMS, accountMetricCount := runMetricsSummary(dir)
		importedCount := snapshot.ImportedCount
		if importedCount == 0 {
			importedCount = importedByRun[id]
		}
		out = append(out, runInfo{
			ID: id, Path: dir, Plugin: pluginID, Status: status, Phase: string(snapshot.Phase),
			PhaseDetail: snapshot.PhaseDetail, Error: snapshot.Error, Target: snapshot.Target,
			Done: snapshot.Done, FailCount: snapshot.FailCount, CPACount: len(files), SSOFiles: ssoN,
			ImportedCount: importedCount, StartedAt: snapshot.StartedAt, UpdatedAt: snapshot.UpdatedAt,
			ModTime: modTime, DurationMS: durationMS, AverageAccountMS: averageAccountMS,
			AccountMetricCount: accountMetricCount,
		})
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "runs": out, "total": total, "page": page,
		"page_size": pageSize, "total_pages": pageCount(total, pageSize),
	})
}

func (s *Server) handleRunFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir, err := s.resolveRun(id)
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var files []map[string]any
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		info, _ := d.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		files = append(files, map[string]any{
			"path": rel,
			"size": size,
		})
		return nil
	})
	if files == nil {
		files = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "run_id": id, "files": files})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir, err := s.resolveRun(id)
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	kind := r.URL.Query().Get("kind") // all | cpa | sso
	if kind == "" {
		kind = "cpa"
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="grok-%s-%s.zip"`, id, kind))
	zw := zip.NewWriter(w)
	defer zw.Close()

	addTree := func(sub string) error {
		root := filepath.Join(dir, sub)
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			rel = filepath.ToSlash(rel)
			fw, err := zw.Create(rel)
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(fw, f)
			_ = f.Close()
			return copyErr
		})
	}

	switch kind {
	case "cpa":
		_ = addTree("CPA")
	case "sso":
		_ = addTree("SSO")
	default:
		_ = addTree("CPA")
		_ = addTree("SSO")
	}
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rel := r.URL.Query().Get("path")
	if rel == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "path required"})
		return
	}
	dir, err := s.resolveRun(id)
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// prevent path traversal
	clean := filepath.Clean("/" + rel)
	clean = strings.TrimPrefix(clean, "/")
	full := filepath.Join(dir, clean)
	if !strings.HasPrefix(full, dir+string(os.PathSeparator)) && full != dir {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid path"})
		return
	}
	http.ServeFile(w, r, full)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// redact secrets
	view := map[string]any{
		"email_mode":                     string(cfg.EmailMode),
		"email_domain":                   cfg.EmailDomain,
		"email_api":                      cfg.EmailAPI,
		"email_default_domains":          cfg.EmailDefaultDomains,
		"duckmail_base":                  cfg.DuckMailBase,
		"duckmail_key_set":               strings.TrimSpace(cfg.DuckMailKey) != "",
		"cloudflare_base":                cfg.CloudflareBase,
		"cloudflare_key_set":             strings.TrimSpace(cfg.CloudflareKey) != "",
		"cloudflare_auth_mode":           cfg.CloudflareAuthMode,
		"cloudflare_custom_auth_set":     strings.TrimSpace(cfg.CloudflareCustomAuth) != "",
		"cloudflare_randomize_subdomain": cfg.CloudflareRandomizeSubdomain,
		"cloudmail_url":                  cfg.CloudMailURL,
		"cloudmail_admin_email":          cfg.CloudMailAdminEmail,
		"cloudmail_password_set":         strings.TrimSpace(cfg.CloudMailPassword) != "",
		"mailnest_key_set":               strings.TrimSpace(cfg.MailNestKey) != "",
		"mailnest_project_code":          cfg.MailNestProjectCode,
		"moemail_base":                   cfg.MoeMailBase,
		"moemail_key_set":                strings.TrimSpace(cfg.MoeMailKey) != "",
		"moemail_domain":                 cfg.MoeMailDomain,
		"moemail_expiry_ms":              cfg.MoeMailExpiryMS,
		"yyds_key_set":                   strings.TrimSpace(cfg.YYDSKey) != "",
		"yyds_jwt_set":                   strings.TrimSpace(cfg.YYDSJWT) != "",
		"yyds_domain":                    cfg.YYDSDomain,
		"clearance_enabled":              cfg.ClearanceEnabled,
		"register_proxy":                 cfg.RegisterProxy,
		"flaresolverr_url":               cfg.FlareSolverrURL,
		"clearance_proxy":                cfg.ClearanceProxy,
		"clearance_urls":                 cfg.ClearanceURLs,
		"turnstile_provider":             cfg.TurnstileProvider,
		"lite_solver_url":                cfg.LiteSolverURL,
		"turnstile_chrome_path":          cfg.TurnstileChromePath,
		"turnstile_python":               cfg.TurnstilePython,
		"turnstile_script":               cfg.TurnstileScript,
		"turnstile_inject_clearance":     cfg.TurnstileInjectClearance,
		"protocol_http":                  cfg.ProtocolHTTP,
		"http_pool_size":                 cfg.HTTPPoolSize,
		"oauth_min_interval_sec":         cfg.OAuthMinIntervalSec,
		"oauth_retry_sec":                cfg.OAuthRetrySec,
		"tempmail_lol_retries":           cfg.TempmailLOLRetries,
		"tempmail_lol_min_interval_ms":   cfg.TempmailLOLIntervalMS,
		"http_proxy":                     cfg.HTTPProxy,
		"https_proxy":                    cfg.HTTPSProxy,
		"no_proxy":                       cfg.NoProxy,
		"resin_proxy":                    cfg.ResinProxy,
		"resin_token_set":                strings.TrimSpace(cfg.ResinToken) != "",
		"resin_platform":                 cfg.ResinPlatform,
		"mail_router_url":                cfg.MailRouterURL,
		"mail_router_api_key_set":        strings.TrimSpace(cfg.MailRouterAPIKey) != "",
		"mail_router_domain":             cfg.MailRouterDomain,
		"bridge_reg_factory_root":        cfg.BridgeRegFactoryRoot,
		"bridge_grok_panel_root":         cfg.BridgeGrokPanelRoot,
		"bridge_outlook_pool_dir":        cfg.BridgeOutlookPoolDir,
		"bridge_python":                  cfg.BridgePythonExe,
		"probe_enabled":                  cfg.ProbeEnabled,
		"physical_cap":                   cfg.PhysicalCap,
		"cpa_upload_enabled":             cfg.CPAUploadEnabled,
		"cpa_management_base":            cfg.CPAManagementBase,
		"cpa_management_key_set":         strings.TrimSpace(cfg.CPAManagementKey) != "",
		"cpa_management_key_masked":      transfer.MaskKey(cfg.CPAManagementKey),
		"cpa_upload_name_template":       cfg.CPAUploadNameTemplate,
		"cpa_upload_timeout_sec":         cfg.CPAUploadTimeoutSec,
		"cpa_upload_retries":             cfg.CPAUploadRetries,
		"cpa_upload_verify":              cfg.CPAUploadVerify,
		"cpa_upload_mode":                cfg.CPAUploadMode,
		"upload_concurrency":             cfg.UploadConcurrency,
		"upload_batch_size":              cfg.UploadBatchSize,
		"export_batch_size":              cfg.ExportBatchSize,
		"export_concurrency":             cfg.ExportConcurrency,
		"patrol_enabled":                 cfg.PatrolEnabled,
		"patrol_interval_min":            cfg.PatrolIntervalMin,
		"patrol_deep_probe":              cfg.PatrolDeepProbe,
		"patrol_concurrency":             cfg.PatrolConcurrency,
		"quota_per_account":              cfg.QuotaPerAccount,
		"refill_enabled":                 cfg.RefillEnabled,
		"refill_min_healthy":             cfg.RefillMinHealthy,
		"refill_batch":                   cfg.RefillBatch,
		"refill_cooldown_min":            cfg.RefillCooldownMin,
		"refill_daily_cap":               cfg.RefillDailyCap,
		"cleanup_quota_enabled":          cfg.CleanupQuotaEnabled,
		"cleanup_on_patrol":              cfg.CleanupOnPatrol,
		"cleanup_backup":                 cfg.CleanupBackup,
		"cleanup_dry_run":                cfg.CleanupDryRun,
		"cluster_role":                   cfg.ClusterRole,
		"cluster_node_name":              cfg.ClusterNodeName,
		"cluster_public_token_set":       strings.TrimSpace(cfg.ClusterPublicToken) != "",
		"cluster_master_url":             cfg.ClusterMasterURL,
		"cluster_master_urls":            maskMasterURLsString(cfg),
		"cluster_master_endpoints":       maskMasterEndpoints(cfg),
		"cluster_status_password_set":    strings.TrimSpace(cfg.ClusterStatusPassword) != "",
		"cluster_heartbeat_sec":          cfg.ClusterHeartbeatSec,
		"cluster_pool_target":            cfg.ClusterPoolTarget,
		"cluster_assign_min":             cfg.ClusterAssignMin,
		"cluster_assign_max":             cfg.ClusterAssignMax,
		"cluster_auto_register":          cfg.ClusterAutoRegister,
		"cluster_auto_upload":            cfg.ClusterAutoUpload,
		"cluster_share_pool_list":        cfg.ClusterSharePoolList,
		"cluster_share_pool_pull":        cfg.ClusterSharePoolPull,
		"cluster_share_infrastructure":   cfg.ClusterShareInfrastructure,
		"local_pool_auto_import":         cfg.LocalPoolAutoImport,
		"local_pool_auto_sync":           cfg.LocalPoolAutoSync,
	}
	writeJSON(w, 200, map[string]any{"ok": true, "config": view})
}

type configUpdate struct {
	EmailMode                    *string  `json:"email_mode"`
	EmailDomain                  *string  `json:"email_domain"`
	EmailAPI                     *string  `json:"email_api"`
	EmailDefaultDomains          *string  `json:"email_default_domains"`
	DuckMailBase                 *string  `json:"duckmail_base"`
	DuckMailKey                  *string  `json:"duckmail_key"`
	CloudflareBase               *string  `json:"cloudflare_base"`
	CloudflareKey                *string  `json:"cloudflare_key"`
	CloudflareAuthMode           *string  `json:"cloudflare_auth_mode"`
	CloudflareCustomAuth         *string  `json:"cloudflare_custom_auth"`
	CloudflareRandomizeSubdomain *bool    `json:"cloudflare_randomize_subdomain"`
	CloudMailURL                 *string  `json:"cloudmail_url"`
	CloudMailAdminEmail          *string  `json:"cloudmail_admin_email"`
	CloudMailPassword            *string  `json:"cloudmail_password"`
	MailNestKey                  *string  `json:"mailnest_key"`
	MailNestProjectCode          *string  `json:"mailnest_project_code"`
	MoeMailBase                  *string  `json:"moemail_base"`
	MoeMailKey                   *string  `json:"moemail_key"`
	MoeMailDomain                *string  `json:"moemail_domain"`
	MoeMailExpiryMS              *int64   `json:"moemail_expiry_ms"`
	YYDSKey                      *string  `json:"yyds_key"`
	YYDSJWT                      *string  `json:"yyds_jwt"`
	YYDSDomain                   *string  `json:"yyds_domain"`
	ClearanceEnabled             *bool    `json:"clearance_enabled"`
	RegisterProxy                *string  `json:"register_proxy"`
	FlareSolverrURL              *string  `json:"flaresolverr_url"`
	ClearanceProxy               *string  `json:"clearance_proxy"`
	ClearanceURLs                *string  `json:"clearance_urls"`
	TurnstileProvider            *string  `json:"turnstile_provider"`
	LiteSolverURL                *string  `json:"lite_solver_url"`
	TurnstileChromePath          *string  `json:"turnstile_chrome_path"`
	TurnstilePython              *string  `json:"turnstile_python"`
	TurnstileScript              *string  `json:"turnstile_script"`
	TurnstileInjectClearance     *bool    `json:"turnstile_inject_clearance"`
	ProtocolHTTP                 *bool    `json:"protocol_http"`
	HTTPPoolSize                 *int     `json:"http_pool_size"`
	OAuthMinInterval             *float64 `json:"oauth_min_interval_sec"`
	OAuthRetry                   *float64 `json:"oauth_retry_sec"`
	TempmailLOLRetries           *int     `json:"tempmail_lol_retries"`
	TempmailLOLIntervalMS        *int     `json:"tempmail_lol_min_interval_ms"`
	ProbeEnabled                 *bool    `json:"probe_enabled"`
	PhysicalCap                  *int     `json:"physical_cap"`
	CPAUploadEnabled             *bool    `json:"cpa_upload_enabled"`
	CPAManagementBase            *string  `json:"cpa_management_base"`
	CPAManagementKey             *string  `json:"cpa_management_key"`
	CPAUploadTimeoutSec          *int     `json:"cpa_upload_timeout_sec"`
	CPAUploadRetries             *int     `json:"cpa_upload_retries"`
	CPAUploadNameTemplate        *string  `json:"cpa_upload_name_template"`
	CPAUploadVerify              *bool    `json:"cpa_upload_verify"`
	CPAUploadMode                *string  `json:"cpa_upload_mode"`
	HTTPProxy                    *string  `json:"http_proxy"`
	HTTPSProxy                   *string  `json:"https_proxy"`
	NoProxy                      *string  `json:"no_proxy"`
	ResinProxy                   *string  `json:"resin_proxy"`
	ResinToken                   *string  `json:"resin_token"`
	ResinPlatform                *string  `json:"resin_platform"`
	MailRouterURL                *string  `json:"mail_router_url"`
	MailRouterAPIKey             *string  `json:"mail_router_api_key"`
	MailRouterDomain             *string  `json:"mail_router_domain"`
	BridgeRegFactoryRoot         *string  `json:"bridge_reg_factory_root"`
	BridgeGrokPanelRoot          *string  `json:"bridge_grok_panel_root"`
	BridgeOutlookPoolDir         *string  `json:"bridge_outlook_pool_dir"`
	BridgePythonExe              *string  `json:"bridge_python"`

	UploadConcurrency *int `json:"upload_concurrency"`
	UploadBatchSize   *int `json:"upload_batch_size"`
	ExportBatchSize   *int `json:"export_batch_size"`
	ExportConcurrency *int `json:"export_concurrency"`

	PatrolEnabled     *bool `json:"patrol_enabled"`
	PatrolIntervalMin *int  `json:"patrol_interval_min"`
	PatrolDeepProbe   *bool `json:"patrol_deep_probe"`
	PatrolConcurrency *int  `json:"patrol_concurrency"`
	QuotaPerAccount   *int  `json:"quota_per_account"`

	RefillEnabled     *bool `json:"refill_enabled"`
	RefillMinHealthy  *int  `json:"refill_min_healthy"`
	RefillBatch       *int  `json:"refill_batch"`
	RefillCooldownMin *int  `json:"refill_cooldown_min"`
	RefillDailyCap    *int  `json:"refill_daily_cap"`

	CleanupQuotaEnabled *bool `json:"cleanup_quota_enabled"`
	CleanupOnPatrol     *bool `json:"cleanup_on_patrol"`
	CleanupBackup       *bool `json:"cleanup_backup"`
	CleanupDryRun       *bool `json:"cleanup_dry_run"`

	ClusterRole                *string `json:"cluster_role"`
	ClusterNodeName            *string `json:"cluster_node_name"`
	ClusterPublicToken         *string `json:"cluster_public_token"`
	ClusterMasterURL           *string `json:"cluster_master_url"`
	ClusterMasterURLs          *string `json:"cluster_master_urls"`
	ClusterStatusPassword      *string `json:"cluster_status_password"`
	ClusterHeartbeatSec        *int    `json:"cluster_heartbeat_sec"`
	ClusterPoolTarget          *int    `json:"cluster_pool_target"`
	ClusterAssignMin           *int    `json:"cluster_assign_min"`
	ClusterAssignMax           *int    `json:"cluster_assign_max"`
	ClusterAutoRegister        *bool   `json:"cluster_auto_register"`
	ClusterAutoUpload          *bool   `json:"cluster_auto_upload"`
	ClusterSharePoolList       *bool   `json:"cluster_share_pool_list"`
	ClusterSharePoolPull       *bool   `json:"cluster_share_pool_pull"`
	ClusterShareInfrastructure *bool   `json:"cluster_share_infrastructure"`

	LocalPoolAutoImport *bool `json:"local_pool_auto_import"`
	LocalPoolAutoSync   *bool `json:"local_pool_auto_sync"`
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var u configUpdate
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&u); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if u.ResinProxy != nil {
		if err := validateProxyURL("resin_proxy", *u.ResinProxy); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"duckmail_key", u.DuckMailKey},
		{"cloudflare_key", u.CloudflareKey},
		{"cloudflare_custom_auth", u.CloudflareCustomAuth},
		{"cloudmail_password", u.CloudMailPassword},
		{"mailnest_key", u.MailNestKey},
		{"moemail_key", u.MoeMailKey},
		{"yyds_key", u.YYDSKey},
		{"yyds_jwt", u.YYDSJWT},
		{"resin_token", u.ResinToken},
		{"mail_router_api_key", u.MailRouterAPIKey},
		{"cpa_management_key", u.CPAManagementKey},
		{"cluster_public_token", u.ClusterPublicToken},
		{"cluster_status_password", u.ClusterStatusPassword},
		{"email_mode", u.EmailMode},
		{"email_domain", u.EmailDomain},
		{"email_api", u.EmailAPI},
		{"email_default_domains", u.EmailDefaultDomains},
		{"duckmail_base", u.DuckMailBase},
		{"cloudflare_base", u.CloudflareBase},
		{"cloudflare_auth_mode", u.CloudflareAuthMode},
		{"cloudmail_url", u.CloudMailURL},
		{"cloudmail_admin_email", u.CloudMailAdminEmail},
		{"mailnest_project_code", u.MailNestProjectCode},
		{"moemail_base", u.MoeMailBase},
		{"moemail_domain", u.MoeMailDomain},
		{"yyds_domain", u.YYDSDomain},
		{"register_proxy", u.RegisterProxy},
		{"flaresolverr_url", u.FlareSolverrURL},
		{"clearance_proxy", u.ClearanceProxy},
		{"clearance_urls", u.ClearanceURLs},
		{"turnstile_provider", u.TurnstileProvider},
		{"lite_solver_url", u.LiteSolverURL},
		{"turnstile_chrome_path", u.TurnstileChromePath},
		{"turnstile_python", u.TurnstilePython},
		{"turnstile_script", u.TurnstileScript},
		{"http_proxy", u.HTTPProxy},
		{"https_proxy", u.HTTPSProxy},
		{"no_proxy", u.NoProxy},
		{"resin_platform", u.ResinPlatform},
		{"mail_router_url", u.MailRouterURL},
		{"mail_router_domain", u.MailRouterDomain},
		{"bridge_reg_factory_root", u.BridgeRegFactoryRoot},
		{"bridge_grok_panel_root", u.BridgeGrokPanelRoot},
		{"bridge_outlook_pool_dir", u.BridgeOutlookPoolDir},
		{"bridge_python", u.BridgePythonExe},
		{"cpa_management_base", u.CPAManagementBase},
		{"cpa_upload_name_template", u.CPAUploadNameTemplate},
		{"cpa_upload_mode", u.CPAUploadMode},
		{"cluster_role", u.ClusterRole},
		{"cluster_node_name", u.ClusterNodeName},
	} {
		if field.value == nil {
			continue
		}
		if err := validateSecretInput(field.name, *field.value); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	if u.EmailMode != nil {
		cfg.EmailMode = config.EmailMode(strings.ToLower(*u.EmailMode))
	}
	if u.EmailDomain != nil {
		cfg.EmailDomain = *u.EmailDomain
	}
	if u.EmailAPI != nil {
		cfg.EmailAPI = *u.EmailAPI
	}
	if u.EmailDefaultDomains != nil {
		cfg.EmailDefaultDomains = strings.TrimSpace(*u.EmailDefaultDomains)
	}
	if u.DuckMailBase != nil {
		cfg.DuckMailBase = strings.TrimRight(strings.TrimSpace(*u.DuckMailBase), "/")
	}
	if u.DuckMailKey != nil && strings.TrimSpace(*u.DuckMailKey) != "" {
		cfg.DuckMailKey = strings.TrimSpace(*u.DuckMailKey)
	}
	if u.CloudflareBase != nil {
		cfg.CloudflareBase = strings.TrimRight(strings.TrimSpace(*u.CloudflareBase), "/")
	}
	if u.CloudflareKey != nil && strings.TrimSpace(*u.CloudflareKey) != "" {
		cfg.CloudflareKey = strings.TrimSpace(*u.CloudflareKey)
	}
	if u.CloudflareAuthMode != nil {
		cfg.CloudflareAuthMode = strings.ToLower(strings.TrimSpace(*u.CloudflareAuthMode))
	}
	if u.CloudflareCustomAuth != nil && strings.TrimSpace(*u.CloudflareCustomAuth) != "" {
		cfg.CloudflareCustomAuth = strings.TrimSpace(*u.CloudflareCustomAuth)
	}
	if u.CloudflareRandomizeSubdomain != nil {
		cfg.CloudflareRandomizeSubdomain = *u.CloudflareRandomizeSubdomain
	}
	if u.CloudMailURL != nil {
		cfg.CloudMailURL = strings.TrimRight(strings.TrimSpace(*u.CloudMailURL), "/")
	}
	if u.CloudMailAdminEmail != nil {
		cfg.CloudMailAdminEmail = strings.TrimSpace(*u.CloudMailAdminEmail)
	}
	if u.CloudMailPassword != nil && strings.TrimSpace(*u.CloudMailPassword) != "" {
		cfg.CloudMailPassword = strings.TrimSpace(*u.CloudMailPassword)
	}
	if u.MailNestKey != nil && strings.TrimSpace(*u.MailNestKey) != "" {
		cfg.MailNestKey = strings.TrimSpace(*u.MailNestKey)
	}
	if u.MailNestProjectCode != nil {
		cfg.MailNestProjectCode = strings.TrimSpace(*u.MailNestProjectCode)
	}
	if u.MoeMailBase != nil {
		cfg.MoeMailBase = strings.TrimRight(strings.TrimSpace(*u.MoeMailBase), "/")
	}
	if u.MoeMailKey != nil && strings.TrimSpace(*u.MoeMailKey) != "" {
		cfg.MoeMailKey = strings.TrimSpace(*u.MoeMailKey)
	}
	if u.MoeMailDomain != nil {
		cfg.MoeMailDomain = strings.TrimSpace(*u.MoeMailDomain)
	}
	if u.MoeMailExpiryMS != nil {
		cfg.MoeMailExpiryMS = *u.MoeMailExpiryMS
	}
	if u.YYDSKey != nil && strings.TrimSpace(*u.YYDSKey) != "" {
		cfg.YYDSKey = strings.TrimSpace(*u.YYDSKey)
	}
	if u.YYDSJWT != nil && strings.TrimSpace(*u.YYDSJWT) != "" {
		cfg.YYDSJWT = strings.TrimSpace(*u.YYDSJWT)
	}
	if u.YYDSDomain != nil {
		cfg.YYDSDomain = strings.TrimSpace(*u.YYDSDomain)
	}
	if u.ClearanceEnabled != nil {
		cfg.ClearanceEnabled = *u.ClearanceEnabled
	}
	if u.RegisterProxy != nil {
		cfg.RegisterProxy = *u.RegisterProxy
		// keep process proxies in sync when user sets register proxy
		if cfg.HTTPProxy == "" || cfg.HTTPProxy == "http://127.0.0.1:40080" {
			cfg.HTTPProxy = *u.RegisterProxy
		}
		if cfg.HTTPSProxy == "" || cfg.HTTPSProxy == "http://127.0.0.1:40080" {
			cfg.HTTPSProxy = *u.RegisterProxy
		}
	}
	if u.FlareSolverrURL != nil {
		cfg.FlareSolverrURL = *u.FlareSolverrURL
	}
	if u.ClearanceProxy != nil {
		cfg.ClearanceProxy = *u.ClearanceProxy
	}
	if u.ClearanceURLs != nil {
		cfg.ClearanceURLs = strings.TrimSpace(*u.ClearanceURLs)
	}
	if u.TurnstileProvider != nil {
		cfg.TurnstileProvider = *u.TurnstileProvider
	}
	if u.LiteSolverURL != nil {
		cfg.LiteSolverURL = strings.TrimRight(strings.TrimSpace(*u.LiteSolverURL), "/")
	}
	if u.TurnstileChromePath != nil {
		cfg.TurnstileChromePath = strings.TrimSpace(*u.TurnstileChromePath)
	}
	if u.TurnstilePython != nil {
		cfg.TurnstilePython = strings.TrimSpace(*u.TurnstilePython)
	}
	if u.TurnstileScript != nil {
		cfg.TurnstileScript = strings.TrimSpace(*u.TurnstileScript)
	}
	if u.TurnstileInjectClearance != nil {
		cfg.TurnstileInjectClearance = *u.TurnstileInjectClearance
	}
	if u.ProtocolHTTP != nil {
		cfg.ProtocolHTTP = *u.ProtocolHTTP
	}
	if u.HTTPPoolSize != nil {
		cfg.HTTPPoolSize = *u.HTTPPoolSize
	}
	if u.TempmailLOLRetries != nil {
		cfg.TempmailLOLRetries = *u.TempmailLOLRetries
	}
	if u.TempmailLOLIntervalMS != nil {
		cfg.TempmailLOLIntervalMS = *u.TempmailLOLIntervalMS
	}
	if u.OAuthMinInterval != nil {
		cfg.OAuthMinIntervalSec = *u.OAuthMinInterval
	}
	if u.OAuthRetry != nil {
		cfg.OAuthRetrySec = *u.OAuthRetry
	}
	if u.ProbeEnabled != nil {
		cfg.ProbeEnabled = *u.ProbeEnabled
	}
	if u.PhysicalCap != nil {
		cfg.PhysicalCap = *u.PhysicalCap
	}
	if u.CPAUploadEnabled != nil {
		cfg.CPAUploadEnabled = *u.CPAUploadEnabled
	}
	if u.CPAManagementBase != nil {
		cfg.CPAManagementBase = *u.CPAManagementBase
	}
	if u.CPAManagementKey != nil && *u.CPAManagementKey != "" {
		cfg.CPAManagementKey = *u.CPAManagementKey
	}
	if u.HTTPProxy != nil {
		cfg.HTTPProxy = *u.HTTPProxy
	}
	if u.HTTPSProxy != nil {
		cfg.HTTPSProxy = *u.HTTPSProxy
	}
	if u.NoProxy != nil {
		cfg.NoProxy = strings.TrimSpace(*u.NoProxy)
	}
	if u.ResinProxy != nil {
		cfg.ResinProxy = strings.TrimSpace(*u.ResinProxy)
	}
	if u.ResinToken != nil && strings.TrimSpace(*u.ResinToken) != "" {
		cfg.ResinToken = strings.TrimSpace(*u.ResinToken)
	}
	if u.ResinPlatform != nil {
		cfg.ResinPlatform = strings.TrimSpace(*u.ResinPlatform)
	}
	if u.MailRouterURL != nil {
		cfg.MailRouterURL = strings.TrimRight(strings.TrimSpace(*u.MailRouterURL), "/")
	}
	if u.MailRouterAPIKey != nil && strings.TrimSpace(*u.MailRouterAPIKey) != "" {
		cfg.MailRouterAPIKey = strings.TrimSpace(*u.MailRouterAPIKey)
	}
	if u.MailRouterDomain != nil {
		cfg.MailRouterDomain = strings.TrimSpace(*u.MailRouterDomain)
	}
	if u.BridgeRegFactoryRoot != nil {
		cfg.BridgeRegFactoryRoot = strings.TrimSpace(*u.BridgeRegFactoryRoot)
	}
	if u.BridgeGrokPanelRoot != nil {
		cfg.BridgeGrokPanelRoot = strings.TrimSpace(*u.BridgeGrokPanelRoot)
	}
	if u.BridgeOutlookPoolDir != nil {
		cfg.BridgeOutlookPoolDir = strings.TrimSpace(*u.BridgeOutlookPoolDir)
	}
	if u.BridgePythonExe != nil {
		cfg.BridgePythonExe = strings.TrimSpace(*u.BridgePythonExe)
	}
	if u.CPAUploadTimeoutSec != nil {
		cfg.CPAUploadTimeoutSec = *u.CPAUploadTimeoutSec
	}
	if u.CPAUploadRetries != nil {
		cfg.CPAUploadRetries = *u.CPAUploadRetries
	}
	if u.CPAUploadNameTemplate != nil {
		cfg.CPAUploadNameTemplate = strings.TrimSpace(*u.CPAUploadNameTemplate)
	}
	if u.CPAUploadVerify != nil {
		cfg.CPAUploadVerify = *u.CPAUploadVerify
	}
	if u.CPAUploadMode != nil {
		cfg.CPAUploadMode = strings.TrimSpace(*u.CPAUploadMode)
	}
	if u.UploadConcurrency != nil {
		cfg.UploadConcurrency = *u.UploadConcurrency
	}
	if u.UploadBatchSize != nil {
		cfg.UploadBatchSize = *u.UploadBatchSize
	}
	if u.ExportBatchSize != nil {
		cfg.ExportBatchSize = *u.ExportBatchSize
	}
	if u.ExportConcurrency != nil {
		cfg.ExportConcurrency = *u.ExportConcurrency
	}
	if u.PatrolEnabled != nil {
		cfg.PatrolEnabled = *u.PatrolEnabled
	}
	if u.PatrolIntervalMin != nil {
		cfg.PatrolIntervalMin = *u.PatrolIntervalMin
	}
	if u.PatrolDeepProbe != nil {
		cfg.PatrolDeepProbe = *u.PatrolDeepProbe
	}
	if u.PatrolConcurrency != nil {
		cfg.PatrolConcurrency = *u.PatrolConcurrency
	}
	if u.QuotaPerAccount != nil {
		cfg.QuotaPerAccount = *u.QuotaPerAccount
	}
	if u.RefillEnabled != nil {
		cfg.RefillEnabled = *u.RefillEnabled
	}
	if u.RefillMinHealthy != nil {
		cfg.RefillMinHealthy = *u.RefillMinHealthy
	}
	if u.RefillBatch != nil {
		cfg.RefillBatch = *u.RefillBatch
	}
	if u.RefillCooldownMin != nil {
		cfg.RefillCooldownMin = *u.RefillCooldownMin
	}
	if u.RefillDailyCap != nil {
		cfg.RefillDailyCap = *u.RefillDailyCap
	}
	if u.CleanupQuotaEnabled != nil {
		cfg.CleanupQuotaEnabled = *u.CleanupQuotaEnabled
	}
	if u.CleanupOnPatrol != nil {
		cfg.CleanupOnPatrol = *u.CleanupOnPatrol
	}
	if u.CleanupBackup != nil {
		cfg.CleanupBackup = *u.CleanupBackup
	}
	if u.CleanupDryRun != nil {
		cfg.CleanupDryRun = *u.CleanupDryRun
	}
	if u.ClusterRole != nil {
		cfg.ClusterRole = strings.ToLower(strings.TrimSpace(*u.ClusterRole))
	}
	if u.ClusterNodeName != nil {
		cfg.ClusterNodeName = strings.TrimSpace(*u.ClusterNodeName)
	}
	if u.ClusterPublicToken != nil {
		cfg.ClusterPublicToken = strings.TrimSpace(*u.ClusterPublicToken)
	}
	if u.ClusterMasterURL != nil {
		cfg.ClusterMasterURL = strings.TrimRight(strings.TrimSpace(*u.ClusterMasterURL), "/")
	}
	if u.ClusterMasterURLs != nil {
		// Accept JSON endpoints or legacy URL lists; blank token keeps previous per-URL secret.
		next := config.Config{ClusterMasterURLs: *u.ClusterMasterURLs, ClusterMasterURL: ""}.ClusterMasterEndpoints()
		prevTok := map[string]string{}
		for _, e := range cfg.ClusterMasterEndpoints() {
			if t := strings.TrimSpace(e.Token); t != "" {
				prevTok[e.URL] = t
			}
		}
		for i := range next {
			if strings.TrimSpace(next[i].Token) == "" {
				if t, ok := prevTok[next[i].URL]; ok {
					next[i].Token = t
				}
			}
		}
		cfg.ClusterMasterURLs = config.FormatMasterEndpoints(next)
		if len(next) > 0 {
			cfg.ClusterMasterURL = next[0].URL
		} else {
			cfg.ClusterMasterURL = ""
		}
	}
	if u.ClusterHeartbeatSec != nil {
		cfg.ClusterHeartbeatSec = *u.ClusterHeartbeatSec
	}
	if u.ClusterPoolTarget != nil {
		cfg.ClusterPoolTarget = *u.ClusterPoolTarget
	}
	if u.ClusterAssignMin != nil {
		cfg.ClusterAssignMin = *u.ClusterAssignMin
	}
	if u.ClusterAssignMax != nil {
		cfg.ClusterAssignMax = *u.ClusterAssignMax
	}
	if u.ClusterAutoRegister != nil {
		cfg.ClusterAutoRegister = *u.ClusterAutoRegister
	}
	if u.ClusterAutoUpload != nil {
		cfg.ClusterAutoUpload = *u.ClusterAutoUpload
	}
	if u.ClusterSharePoolList != nil {
		cfg.ClusterSharePoolList = *u.ClusterSharePoolList
	}
	if u.ClusterSharePoolPull != nil {
		cfg.ClusterSharePoolPull = *u.ClusterSharePoolPull
	}
	if u.ClusterShareInfrastructure != nil {
		cfg.ClusterShareInfrastructure = *u.ClusterShareInfrastructure
	}
	if u.ClusterStatusPassword != nil {
		cfg.ClusterStatusPassword = *u.ClusterStatusPassword
	}
	if u.LocalPoolAutoImport != nil {
		cfg.LocalPoolAutoImport = *u.LocalPoolAutoImport
	}
	if u.LocalPoolAutoSync != nil {
		cfg.LocalPoolAutoSync = *u.LocalPoolAutoSync
	}
	if err := saveConfigWithSecrets(s.opt.Paths.Config, cfg); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func saveConfigWithSecrets(path string, cfg config.Config) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	if err := config.Save(tmpPath, cfg); err != nil {
		return err
	}
	for _, secret := range []struct {
		key string
		val string
	}{
		{"CPA_MANAGEMENT_KEY", cfg.CPAManagementKey},
		{"CLUSTER_PUBLIC_TOKEN", cfg.ClusterPublicToken},
		{"CLUSTER_STATUS_PASSWORD", cfg.ClusterStatusPassword},
		{"RESIN_TOKEN", cfg.ResinToken},
		{"MAIL_ROUTER_API_KEY", cfg.MailRouterAPIKey},
		{"DUCKMAIL_API_KEY", cfg.DuckMailKey},
		{"CLOUDFLARE_API_KEY", cfg.CloudflareKey},
		{"CLOUDFLARE_CUSTOM_AUTH", cfg.CloudflareCustomAuth},
		{"CLOUDMAIL_PASSWORD", cfg.CloudMailPassword},
		{"MAILNEST_API_KEY", cfg.MailNestKey},
		{"MOEMAIL_API_KEY", cfg.MoeMailKey},
		{"YYDS_API_KEY", cfg.YYDSKey},
		{"YYDS_JWT", cfg.YYDSJWT},
	} {
		if strings.TrimSpace(secret.val) == "" {
			continue
		}
		if err := appendEnvKey(tmpPath, secret.key, secret.val); err != nil {
			return err
		}
	}
	return os.Rename(tmpPath, path)
}

func appendEnvKey(path, key, val string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	prefix := key + "="
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			lines[i] = prefix + val
			found = true
			break
		}
	}
	if !found {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, prefix+val)
		} else {
			lines = append(lines, prefix+val)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
}

func (s *Server) resolveRun(id string) (string, error) {
	id = filepath.Base(strings.TrimSpace(id))
	if id == "" || id == "." || id == ".." {
		return "", fmt.Errorf("invalid run id")
	}
	dir := filepath.Join(s.opt.Paths.Outputs, id)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("run not found")
	}
	return dir, nil
}

func latestLog(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestT time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "run-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestT) {
			bestT = info.ModTime()
			best = filepath.Join(dir, e.Name())
		}
	}
	return best
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// Shutdown helper for tests.
func IdleContext() context.Context { return context.Background() }

func contentTypeFor(path string) string {
	switch {
	case strings.HasSuffix(path, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(path, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(path, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(path, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(path, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(path, ".png"):
		return "image/png"
	case strings.HasSuffix(path, ".ico"):
		return "image/x-icon"
	case strings.HasSuffix(path, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(path, ".txt"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func parsePage(r *http.Request, defPage, defSize, maxSize int) (page, pageSize int) {
	page, pageSize = defPage, defSize
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > maxSize {
		pageSize = maxSize
	}
	if pageSize < 1 {
		pageSize = defSize
	}
	return page, pageSize
}

func pageCount(total, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	if total <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

func maskMasterURLsString(cfg config.Config) string {
	eps := cfg.ClusterMasterEndpoints()
	if len(eps) == 0 {
		return ""
	}
	urls := make([]string, 0, len(eps))
	for _, e := range eps {
		urls = append(urls, e.URL)
	}
	return strings.Join(urls, ",")
}

func maskMasterEndpoints(cfg config.Config) []map[string]any {
	eps := cfg.ClusterMasterEndpoints()
	out := make([]map[string]any, 0, len(eps))
	for _, e := range eps {
		out = append(out, map[string]any{
			"url":       e.URL,
			"token_set": strings.TrimSpace(e.Token) != "",
		})
	}
	return out
}
