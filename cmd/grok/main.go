package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/api"
	"github.com/grok-free-register/grok-reg/internal/artifact"
	"github.com/grok-free-register/grok-reg/internal/bridge"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/daemon"
	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/jobs"
	"github.com/grok-free-register/grok-reg/internal/logx"
	"github.com/grok-free-register/grok-reg/internal/pipeline"
	"github.com/grok-free-register/grok-reg/internal/plugin"
	"github.com/grok-free-register/grok-reg/internal/state"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
	"github.com/grok-free-register/grok-reg/web"
)

var version = "0.1.0"

func main() {
	args := os.Args[1:]
	if daemon.IsWorker() {
		if err := runWorker(args); err != nil {
			fmt.Fprintf(os.Stderr, "worker error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) == 0 {
		printHelp()
		os.Exit(0)
	}
	cmd := args[0]
	switch cmd {
	case "start":
		if err := cmdStart(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := cmdStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "stop":
		if err := cmdStop(); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "logs":
		if err := cmdLogs(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "upload":
		if err := cmdUpload(); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "panel", "serve", "web":
		if err := cmdPanel(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "plugin", "plugins":
		if err := cmdPlugin(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "run":
		if err := cmdRun(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "pool":
		if err := cmdPool(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "artifacts", "artifact":
		if err := cmdArtifacts(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		printHelp()
	case "version", "-v", "--version":
		fmt.Printf("touch-squirrel %s (%s)\n", version, appName())
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

func appName() string {
	base := filepath.Base(os.Args[0])
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		return "squirrel"
	}
	return base
}

func printHelp() {
	name := appName()
	fmt.Printf(`%s — touch-squirrel 囤囤鼠运行时（插件化注册 / 号池）

用法:
  %s plugin list              列出已发现插件（in-tree + 已安装）
  %s plugin show <id>         查看插件清单
  %s plugin install <dir>     安装本地插件目录（任意本地包）
  %s plugin enable <id>       启用插件
  %s plugin disable <id>      禁用插件
  %s run <id> [-t N]          运行 registrar 插件（xai-accounts 走现网流水线）
  %s start [-t N]             兼容：等同 run xai-accounts
  %s pool keys list|add       tavily-pool 密钥管理
  %s pool serve [--addr]      启动 tavily-pool HTTP/MCP 代理
  %s artifacts list           列出统一囤货产物
  %s status                   查看运行状态与进度
  %s stop                     立即停止注册机
  %s logs [-f]                查看最近一次运行日志；-f 实时跟踪
  %s upload                   选择最近 run 的 CPA JSON 上传到 Management API
  %s panel                    启动 Web 控制面板 (默认 :8787)
  %s help                     显示帮助

环境变量:
  SQUIRREL_HOME       数据目录（默认 ~/.touch-squirrel；若仅有 ~/.grok 则沿用）
  GROK_HOME           兼容旧变量（次优先）
  SQUIRREL_PLUGINS    in-tree plugins 目录覆盖
  PANEL_ADDR          面板监听地址（默认 :8787）
  PANEL_TOKEN         面板鉴权 token（建议生产环境设置）

数据目录: ~/.touch-squirrel/ （可用 SQUIRREL_HOME / GROK_HOME 覆盖）
插件目录: $HOME/plugins/<id>/<version>/
输出:     $HOME/outputs/<yyyymmdd-HHMMSS>/{SSO,CPA}/
契约:     docs/contracts/plugin-idl.md
`, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name)
}

func paths() (home.Paths, error) {
	p, err := home.Resolve()
	if err != nil {
		return p, err
	}
	if err := p.EnsureBase(); err != nil {
		return p, err
	}
	return p, nil
}

func pluginManager(p home.Paths) *plugin.Manager {
	return plugin.NewManager(p.PluginsDir, p.EnabledFile, plugin.ResolveInTreeRoot())
}

func cmdPanel(args []string) error {
	addr := os.Getenv("PANEL_ADDR")
	if addr == "" {
		addr = ":8787"
	}
	token := os.Getenv("PANEL_TOKEN")
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-addr" || a == "--addr":
			if i+1 >= len(args) {
				return fmt.Errorf("--addr 需要参数")
			}
			addr = args[i+1]
			i++
		case a == "-token" || a == "--token":
			if i+1 >= len(args) {
				return fmt.Errorf("--token 需要参数")
			}
			token = args[i+1]
			i++
		default:
			return fmt.Errorf("未知参数: %s", a)
		}
	}

	p, err := paths()
	if err != nil {
		return err
	}

	displayAddr := addr
	if strings.HasPrefix(displayAddr, ":") {
		displayAddr = "0.0.0.0" + displayAddr
	}
	fmt.Printf("[*] touch-squirrel Panel  http://%s\n", displayAddr)
	fmt.Printf("    HOME=       %s\n", p.Root)
	if token != "" {
		fmt.Printf("    鉴权:       PANEL_TOKEN 已启用\n")
	} else {
		fmt.Printf("    鉴权:       关闭（生产请设置 PANEL_TOKEN）\n")
	}
	fmt.Printf("    停止:       Ctrl-C\n")

	srv := api.New(api.Options{
		Paths: p,
		Addr:  addr,
		Token: token,
		WebFS: web.FS,
	})
	return srv.ListenAndServe()
}

func cmdStart(args []string) error {
	target := 10
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t" || a == "--target":
			if i+1 >= len(args) {
				return fmt.Errorf("-t 需要数字参数")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("无效目标: %s", args[i+1])
			}
			target, err = config.ClampTarget(n)
			if err != nil {
				return err
			}
			i++
		case strings.HasPrefix(a, "-t"):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "-t"))
			if err != nil {
				return fmt.Errorf("无效 -t: %s", a)
			}
			target, err = config.ClampTarget(n)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("未知参数: %s", a)
		}
	}

	p, err := paths()
	if err != nil {
		return err
	}

	if pid, err := daemon.ReadPID(p.PID); err == nil && daemon.PIDAlive(pid) {
		return fmt.Errorf("注册机已经在运行 (PID %d)，先 grok status / grok stop", pid)
	}

	if _, err := os.Stat(p.Config); os.IsNotExist(err) {
		if _, err := config.InteractiveSetup(p.Config); err != nil {
			return err
		}
	}

	runID := home.NewRunID()
	_ = os.MkdirAll(p.LogsDir, 0o700)
	logPath := filepath.Join(p.LogsDir, fmt.Sprintf("run-%s.log", runID))

	st := state.NewStore(p.State)
	_ = st.Set(func(s *state.Snapshot) {
		s.Status = state.StatusRunning
		s.RunID = runID
		s.Target = target
		s.Done = 0
		s.Phase = state.PhaseIdle
		s.PhaseDetail = "启动中"
		s.LogPath = logPath
		s.OutputDir = filepath.Join(p.Outputs, runID)
		s.Error = ""
		s.PID = 0
	})

	pid, err := daemon.StartBackground(target, runID)
	if err != nil {
		return err
	}
	if err := daemon.WritePID(p.PID, pid); err != nil {
		return err
	}
	_ = st.Set(func(s *state.Snapshot) { s.PID = pid })

	fmt.Printf("[✓] 注册机已后台启动\n")
	fmt.Printf("    PID:    %d\n", pid)
	fmt.Printf("    目标:   %d\n", target)
	fmt.Printf("    Run:    %s\n", runID)
	fmt.Printf("    日志:   %s\n", logPath)
	fmt.Printf("    输出:   %s\n", filepath.Join(p.Outputs, runID))
	fmt.Printf("    查看:   grok status  |  grok logs -f\n")
	return nil
}

func runWorker(args []string) error {
	target := 10
	runID := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--worker":
			continue
		case "--target":
			if i+1 < len(args) {
				n, _ := strconv.Atoi(args[i+1])
				if n > 0 {
					target = n
				}
				i++
			}
		case "--run-id":
			if i+1 < len(args) {
				runID = args[i+1]
				i++
			}
		}
	}
	target, err := config.ClampTarget(target)
	if err != nil {
		return err
	}

	p, err := paths()
	if err != nil {
		return err
	}
	unlock, err := daemon.TryLock(p.Lock)
	if err != nil {
		return err
	}
	defer unlock()

	if err := daemon.WritePID(p.PID, os.Getpid()); err != nil {
		return err
	}
	defer daemon.ClearPID(p.PID)

	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	cfg.Target = target

	run, err := p.PrepareRun(runID)
	if err != nil {
		return err
	}
	log, err := logx.New(run.LogPath)
	if err != nil {
		return err
	}
	defer log.Close()

	st := state.NewStore(p.State)
	_ = st.Set(func(s *state.Snapshot) {
		s.Status = state.StatusRunning
		s.RunID = run.RunID
		s.Target = target
		s.PID = os.Getpid()
		s.LogPath = run.LogPath
		s.OutputDir = run.Root
	})

	ctx := context.Background()
	err = pipeline.Run(ctx, pipeline.Options{
		Cfg:    cfg,
		Paths:  p,
		Run:    run,
		Target: target,
		Log:    log,
		Store:  st,
	})
	if err != nil {
		_ = st.Set(func(s *state.Snapshot) {
			s.Status = state.StatusError
			s.Error = err.Error()
			s.PhaseDetail = "错误退出"
			s.PID = 0
		})
		log.Errf("%v", err)
		return err
	}
	return nil
}

func cmdStatus() error {
	p, err := paths()
	if err != nil {
		return err
	}
	st := state.NewStore(p.State)
	snap, err := st.Load()
	if err != nil && !os.IsNotExist(err) {
		fmt.Println("状态: 未运行")
		return nil
	}
	if os.IsNotExist(err) {
		fmt.Println("状态: 未运行")
		return nil
	}
	if snap.Status == state.StatusRunning {
		if snap.PID == 0 {
			if pid, e := daemon.ReadPID(p.PID); e == nil {
				snap.PID = pid
			}
		}
		if snap.PID != 0 && !daemon.PIDAlive(snap.PID) {
			snap.Status = state.StatusStopped
			snap.PhaseDetail = "进程已结束"
			snap.PID = 0
		}
	}
	fmt.Print(daemon.FormatStatus(snap))
	return nil
}

func cmdStop() error {
	p, err := paths()
	if err != nil {
		return err
	}
	if err := daemon.Stop(p); err != nil {
		return err
	}
	st := state.NewStore(p.State)
	_ = st.Set(func(s *state.Snapshot) {
		s.Status = state.StatusStopped
		s.Phase = state.PhaseIdle
		s.PhaseDetail = "已手动停止"
		s.PID = 0
	})
	fmt.Println("[✓] 注册机已停止")
	return nil
}

func cmdLogs(args []string) error {
	follow := false
	for _, a := range args {
		if a == "-f" || a == "--follow" {
			follow = true
		}
	}
	p, err := paths()
	if err != nil {
		return err
	}
	st := state.NewStore(p.State)
	snap, _ := st.Load()
	path := snap.LogPath
	if path == "" {
		path = latestLog(p.LogsDir)
	}
	if path == "" {
		return fmt.Errorf("没有日志文件")
	}
	if !follow {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	}
	fmt.Fprintf(os.Stderr, "跟踪 %s (Ctrl-C 退出)\n", path)
	var offset int64
	if fi, err := os.Stat(path); err == nil {
		offset = fi.Size() - 4096
		if offset < 0 {
			offset = 0
		}
	}
	for {
		f, err := os.Open(path)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if _, err := f.Seek(offset, 0); err != nil {
			_ = f.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		buf := make([]byte, 8192)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				_, _ = os.Stdout.Write(buf[:n])
				offset += int64(n)
			}
			if err != nil {
				break
			}
		}
		_ = f.Close()
		time.Sleep(400 * time.Millisecond)
	}
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

func cmdUpload() error {
	p, err := paths()
	if err != nil {
		return err
	}
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	if v := os.Getenv("CPA_UPLOAD_ENABLED"); v != "" {
		cfg.CPAUploadEnabled = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := os.Getenv("CPA_MANAGEMENT_BASE"); v != "" {
		cfg.CPAManagementBase = v
	}
	if v := os.Getenv("CPA_MANAGEMENT_KEY"); v != "" {
		cfg.CPAManagementKey = v
	}
	if strings.TrimSpace(cfg.CPAManagementKey) == "" {
		return fmt.Errorf("未配置 CPA_MANAGEMENT_KEY（在 ~/.grok/config.env 或环境变量中设置）")
	}
	if strings.TrimSpace(cfg.CPAManagementBase) == "" {
		cfg.CPAManagementBase = "http://localhost:8317/v0/management"
	}

	runs, err := cpa.ListRunDirs(p.Outputs, 10)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return fmt.Errorf("outputs 下没有注册结果目录")
	}

	fmt.Println("最近注册 run（最多 10 个）:")
	type item struct {
		dir   string
		name  string
		files []string
	}
	var items []item
	for i, dir := range runs {
		files, _ := cpa.CollectCPAJSON(dir)
		name := filepath.Base(dir)
		items = append(items, item{dir: dir, name: name, files: files})
		fmt.Printf("  [%d] %s  CPA文件=%d\n", i+1, name, len(files))
	}
	fmt.Print("选择要上传的序号（如 1 或 1,2,3；回车取消）: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		fmt.Println("已取消")
		return nil
	}
	var selected []int
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > len(items) {
			return fmt.Errorf("无效序号: %s", part)
		}
		selected = append(selected, n-1)
	}
	if len(selected) == 0 {
		fmt.Println("未选择")
		return nil
	}

	up := cpa.NewUploader(cpa.UploadConfig{
		Enabled:      true,
		BaseURL:      cfg.CPAManagementBase,
		Key:          cfg.CPAManagementKey,
		TimeoutSec:   cfg.CPAUploadTimeoutSec,
		Retries:      cfg.CPAUploadRetries,
		NameTemplate: cfg.CPAUploadNameTemplate,
		Verify:       cfg.CPAUploadVerify,
		Mode:         cfg.CPAUploadMode,
	}, func(f string, a ...any) {
		fmt.Printf(f+"\n", a...)
	})

	var okN, failN, skipN int
	for _, idx := range selected {
		it := items[idx]
		if len(it.files) == 0 {
			fmt.Printf("[!] %s 无 CPA json，跳过\n", it.name)
			skipN++
			continue
		}
		fmt.Printf("[*] 上传 %s (%d 个文件)...\n", it.name, len(it.files))
		for _, f := range it.files {
			res := up.UploadFile(f)
			if res.OK {
				okN++
			} else {
				failN++
			}
		}
	}
	fmt.Printf("[✓] 完成 ok=%d fail=%d skip_runs=%d\n", okN, failN, skipN)
	return nil
}

func cmdPlugin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: %s plugin list|show|install|enable|disable", appName())
	}
	p, err := paths()
	if err != nil {
		return err
	}
	mgr := pluginManager(p)
	switch args[0] {
	case "list", "ls":
		list, err := mgr.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("（无插件）可设置 SQUIRREL_PLUGINS 或 squirrel plugin install <dir>")
			return nil
		}
		fmt.Printf("%-18s %-10s %-12s %-8s %-8s %s\n", "ID", "VERSION", "RUNTIME", "ENABLED", "SOURCE", "KIND")
		for _, it := range list {
			src := "installed"
			if it.InTree {
				src = "in-tree"
			} else if it.Manifest.Source != "" && it.Manifest.Source != "in-tree" {
				src = it.Manifest.Source
			}
			en := "no"
			if it.Enabled {
				en = "yes"
			}
			kinds := make([]string, 0, len(it.Manifest.Kind))
			for _, k := range it.Manifest.Kind {
				kinds = append(kinds, string(k))
			}
			fmt.Printf("%-18s %-10s %-12s %-8s %-8s %s\n",
				it.Manifest.ID, it.Manifest.Version, it.Manifest.Runtime, en, src, strings.Join(kinds, ","))
		}
		if root := plugin.ResolveInTreeRoot(); root != "" {
			fmt.Printf("\nin-tree: %s\n", root)
		}
		fmt.Printf("home:    %s\n", p.PluginsDir)
		return nil
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("用法: %s plugin show <id>", appName())
		}
		it, err := mgr.Get(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("id:          %s\n", it.Manifest.ID)
		fmt.Printf("name:        %s\n", it.Manifest.Name)
		fmt.Printf("version:     %s\n", it.Manifest.Version)
		fmt.Printf("runtime:     %s\n", it.Manifest.Runtime)
		fmt.Printf("enabled:     %v\n", it.Enabled)
		fmt.Printf("path:        %s\n", it.Root)
		fmt.Printf("description: %s\n", it.Manifest.Description)
		if len(it.Manifest.Capabilities) > 0 {
			fmt.Printf("capabilities:%s\n", " "+strings.Join(it.Manifest.Capabilities, ", "))
		}
		if len(it.Manifest.ArtifactKinds) > 0 {
			fmt.Printf("artifacts:   %s\n", strings.Join(it.Manifest.ArtifactKinds, ", "))
		}
		return nil
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("用法: %s plugin install <dir>", appName())
		}
		it, err := mgr.InstallPath(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("[✓] 已安装 %s@%s\n", it.Manifest.ID, it.Manifest.Version)
		fmt.Printf("    path: %s\n", it.Root)
		fmt.Printf("    enabled: %v\n", it.Enabled)
		return nil
	case "enable":
		if len(args) < 2 {
			return fmt.Errorf("用法: %s plugin enable <id>", appName())
		}
		if err := mgr.Enable(args[1]); err != nil {
			return err
		}
		fmt.Printf("[✓] enabled %s\n", args[1])
		return nil
	case "disable":
		if len(args) < 2 {
			return fmt.Errorf("用法: %s plugin disable <id>", appName())
		}
		if err := mgr.Disable(args[1]); err != nil {
			return err
		}
		fmt.Printf("[✓] disabled %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("未知 plugin 子命令: %s", args[0])
	}
}

// cmdRun dispatches a registrar plugin. xai-accounts uses the existing pipeline.
func cmdRun(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: %s run <plugin-id> [-t N]", appName())
	}
	id := args[0]
	rest := args[1:]
	p, err := paths()
	if err != nil {
		return err
	}
	mgr := pluginManager(p)
	it, err := mgr.Get(id)
	if err != nil {
		return err
	}
	if !it.Enabled {
		return fmt.Errorf("插件 %s 未启用，先 %s plugin enable %s", id, appName(), id)
	}
	switch id {
	case "xai-accounts":
		fmt.Printf("[*] run plugin=%s runtime=%s (legacy pipeline)\n", id, it.Manifest.Runtime)
		return cmdStart(rest)
	case "tavily-registrar":
		return fmt.Errorf("tavily-registrar 仍是 shell：等待真实开号流程接入")
	case "tavily-pool":
		return cmdPool(append([]string{"serve"}, rest...))
	default:
		if !it.Manifest.HasKind(plugin.KindRegistrar) {
			return fmt.Errorf("插件 %s 不是 registrar，无法 run", id)
		}
		// Bridge plugins: spawn Python subprocess with job tracking.
		if it.Manifest.Runtime == plugin.RuntimeBridge && it.Manifest.Entry.Bridge != "" {
			return runBridge(id, it, rest, p)
		}
		return fmt.Errorf("插件 %s 尚无 runner 实现（runtime=%s path=%s）", id, it.Manifest.Runtime, it.Root)
	}
}

// runBridge spawns a bridge plugin subprocess (Python, etc.) with job tracking.
func runBridge(id string, it plugin.Installed, args []string, p home.Paths) error {
	target := 10
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t" || a == "--target":
			if i+1 >= len(args) {
				return fmt.Errorf("-t 需要数字参数")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("无效目标: %s", args[i+1])
			}
			target, err = config.ClampTarget(n)
			if err != nil {
				return err
			}
			i++
		case strings.HasPrefix(a, "-t"):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "-t"))
			if err != nil {
				return fmt.Errorf("无效 -t: %s", a)
			}
			target, err = config.ClampTarget(n)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("未知参数: %s", a)
		}
	}

	runID := home.NewRunID()
	outputDir := filepath.Join(p.Outputs, runID)
	logPath := filepath.Join(p.LogsDir, fmt.Sprintf("run-%s.log", runID))
	_ = os.MkdirAll(p.LogsDir, 0o700)

	st := state.NewStore(p.State)
	_ = st.Set(func(s *state.Snapshot) {
		s.Status = state.StatusRunning
		s.RunID = runID
		s.Target = target
		s.Done = 0
		s.Phase = state.PhaseIdle
		s.PhaseDetail = fmt.Sprintf("bridge: %s", id)
		s.LogPath = logPath
		s.OutputDir = outputDir
		s.Error = ""
		s.PID = 0
	})

	bridgePath := filepath.Join(it.Root, it.Manifest.Entry.Bridge)
	fmt.Printf("[*] bridge plugin=%s script=%s target=%d\n", id, bridgePath, target)

	// CLI mode: no job manager needed (direct stdout).
	mgr := jobs.NewManager(id, 24*time.Hour)
	result, err := bridge.Run(context.Background(), mgr, bridge.Config{
		PluginID:    id,
		BridgePath:  bridgePath,
		PythonExe:   os.Getenv("GROK_PYTHON"),
		Target:      target,
		PluginCfg:   map[string]any{"auto": true},
		OutputDir:   outputDir,
		ArtifactDir: filepath.Join(p.Root, "artifacts"),
	})
	if err != nil {
		_ = st.Set(func(s *state.Snapshot) { s.Status = state.StatusError; s.Error = err.Error() })
		return err
	}

	_ = st.Set(func(s *state.Snapshot) {
		s.Status = state.StatusStopped
		s.Done = result.OK + result.Fail
		s.Phase = state.PhaseIdle
	})
	fmt.Printf("[✓] %s 完成 ok=%d fail=%d\n", id, result.OK, result.Fail)
	fmt.Printf("    日志: %s\n", logPath)
	fmt.Printf("    输出: %s\n", outputDir)
	return nil
}

func cmdPool(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: %s pool keys list|add <key> | serve [--addr host:port]", appName())
	}
	p, err := paths()
	if err != nil {
		return err
	}
	pool := tavilypool.New(tavilypool.DefaultStatePath(p.Root))
	switch args[0] {
	case "keys":
		if len(args) < 2 {
			return fmt.Errorf("用法: %s pool keys list|add <api_key> [note]", appName())
		}
		switch args[1] {
		case "list", "ls":
			keys, err := pool.List(true)
			if err != nil {
				return err
			}
			if len(keys) == 0 {
				fmt.Println("（空池）用: squirrel pool keys add <tavily-api-key>")
				return nil
			}
			fmt.Printf("%-8s %-10s %8s %8s %-20s %s\n", "ID", "STATUS", "OK", "FAIL", "LAST_USED", "KEY")
			for _, k := range keys {
				lu := "-"
				if k.LastUsedUnix > 0 {
					lu = time.Unix(k.LastUsedUnix, 0).Format(time.RFC3339)
				}
				fmt.Printf("%-8s %-10s %8d %8d %-20s %s\n", k.ID, k.Status, k.Success, k.Failure, lu, k.APIKey)
			}
			return nil
		case "add":
			if len(args) < 3 {
				return fmt.Errorf("用法: %s pool keys add <api_key> [note]", appName())
			}
			note := ""
			if len(args) >= 4 {
				note = strings.Join(args[3:], " ")
			}
			k, err := pool.Add(args[2], note)
			if err != nil {
				return err
			}
			// also mirror into artifact store as key.tavily (secret still only in pool file)
			arts := artifact.NewStore(p.ArtifactsDir)
			_, _ = arts.PutJSON("tavily-pool", "key.tavily", artifact.StatusFresh, map[string]string{
				"key_id": k.ID,
			}, map[string]any{
				"id":     k.ID,
				"status": k.Status,
				"note":   k.Note,
				"masked": k.APIKey,
			}, "")
			fmt.Printf("[✓] added key id=%s %s\n", k.ID, k.APIKey)
			return nil
		case "disable":
			if len(args) < 3 {
				return fmt.Errorf("用法: %s pool keys disable <id>", appName())
			}
			return pool.SetStatus(args[2], tavilypool.StatusDisabled)
		case "enable":
			if len(args) < 3 {
				return fmt.Errorf("用法: %s pool keys enable <id>", appName())
			}
			return pool.SetStatus(args[2], tavilypool.StatusActive)
		default:
			return fmt.Errorf("未知 pool keys 子命令: %s", args[1])
		}
	case "serve", "start":
		addr := "127.0.0.1:8791"
		upstream := "https://api.tavily.com"
		for i := 1; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--addr" || a == "-addr":
				if i+1 >= len(args) {
					return fmt.Errorf("--addr 需要参数")
				}
				addr = args[i+1]
				i++
			case a == "--upstream":
				if i+1 >= len(args) {
					return fmt.Errorf("--upstream 需要参数")
				}
				upstream = args[i+1]
				i++
			default:
				if !strings.HasPrefix(a, "-") {
					addr = a
				} else {
					return fmt.Errorf("未知参数: %s", a)
				}
			}
		}
		addr, err = tavilypool.ListenAddr(addr)
		if err != nil {
			return err
		}
		srv := &tavilypool.Server{
			Pool:     pool,
			Upstream: upstream,
			Addr:     addr,
			Logf: func(f string, a ...any) {
				fmt.Printf(f+"\n", a...)
			},
		}
		fmt.Printf("[*] tavily-pool serve %s  (data %s)\n", addr, tavilypool.DefaultStatePath(p.Root))
		fmt.Printf("    health:  http://%s/health\n", addr)
		fmt.Printf("    search:  POST http://%s/api/tavily/search\n", addr)
		fmt.Printf("    mcp:     POST http://%s/mcp\n", addr)
		fmt.Printf("    keys:    GET  http://%s/api/keys\n", addr)
		ctx := context.Background()
		return srv.ListenAndServe(ctx)
	default:
		return fmt.Errorf("未知 pool 子命令: %s", args[0])
	}
}

func cmdArtifacts(args []string) error {
	p, err := paths()
	if err != nil {
		return err
	}
	pluginFilter, kindFilter := "", ""
	limit := 50
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "list" || a == "ls":
			continue
		case a == "--plugin" || a == "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("--plugin 需要参数")
			}
			pluginFilter = args[i+1]
			i++
		case a == "--kind" || a == "-k":
			if i+1 >= len(args) {
				return fmt.Errorf("--kind 需要参数")
			}
			kindFilter = args[i+1]
			i++
		case a == "--limit":
			if i+1 >= len(args) {
				return fmt.Errorf("--limit 需要数字")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return err
			}
			limit = n
			i++
		default:
			// bare plugin id shortcut
			if pluginFilter == "" && !strings.HasPrefix(a, "-") {
				pluginFilter = a
			}
		}
	}
	st := artifact.NewStore(p.ArtifactsDir)
	list, err := st.List(pluginFilter, kindFilter, limit)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("（无产物）")
		return nil
	}
	fmt.Printf("%-16s %-14s %-14s %-10s %-20s %s\n", "ID", "PLUGIN", "KIND", "STATUS", "CREATED", "LABELS")
	for _, a := range list {
		labs := make([]string, 0, len(a.Labels))
		for k, v := range a.Labels {
			labs = append(labs, k+"="+v)
		}
		fmt.Printf("%-16s %-14s %-14s %-10s %-20s %s\n",
			a.ID, a.Plugin, a.Kind, a.Status, a.CreatedAt, strings.Join(labs, ","))
	}
	fmt.Printf("\nstore: %s\n", p.ArtifactsDir)
	return nil
}
