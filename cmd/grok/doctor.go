package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/plugin"
	"github.com/grok-free-register/grok-reg/web"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func cmdDoctor(args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("doctor 未知参数: %s", arg)
		}
	}

	report := runDoctor()
	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	fmt.Println("Squirrel Doctor")
	for _, check := range report.Checks {
		fmt.Printf("[%s] %-16s %s\n", check.Status, check.Name, check.Detail)
	}
	if report.OK {
		fmt.Println("\n诊断完成：Host 可以启动。")
		return nil
	}
	return fmt.Errorf("诊断发现阻塞问题")
}

func runDoctor() doctorReport {
	checks := make([]doctorCheck, 0, 7)
	add := func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}

	build := currentBuildInfo()
	add("build", "ok", fmt.Sprintf("%s · %s · %s", build.Version, shortCommit(build.Commit), build.Repository))

	resolvedPaths, pathsErr := paths()
	if pathsErr != nil {
		add("data directory", "error", pathsErr.Error())
	} else if err := checkWritable(resolvedPaths.Root); err != nil {
		add("data directory", "error", err.Error())
	} else {
		add("data directory", "ok", resolvedPaths.Root)
	}

	if _, err := fs.Stat(web.FS, "out/index.html"); err != nil {
		add("web assets", "error", err.Error())
	} else {
		add("web assets", "ok", "embedded manager is available")
	}

	officialURL, officialErr := plugin.CurrentOfficialRepositoryURL()
	if officialErr != nil {
		add("official source", "error", officialErr.Error())
	} else if err := checkRepository(officialURL); err != nil {
		add("official source", "warn", officialURL+" · "+err.Error())
	} else {
		add("official source", "ok", officialURL)
	}

	if pathsErr == nil {
		manager := pluginManager(resolvedPaths)
		installed, listErr := manager.List()
		if listErr != nil {
			add("plugins", "error", listErr.Error())
		} else {
			mode := "installed only"
			if root := plugin.ResolveInTreeRoot(); root != "" {
				mode = "source tree: " + root
			}
			add("plugins", "ok", fmt.Sprintf("%d discovered · %s", len(installed), mode))
		}
	}

	addr := strings.TrimSpace(os.Getenv("PANEL_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	if listener, listenErr := net.Listen("tcp", addr); listenErr == nil {
		_ = listener.Close()
		add("web address", "ok", addr+" is available")
	} else if panelIsHealthy(addr) {
		add("web address", "ok", addr+" already serves Squirrel")
	} else {
		add("web address", "warn", addr+" is unavailable: "+listenErr.Error())
	}

	report := doctorReport{OK: true, Checks: checks}
	for _, check := range checks {
		if check.Status == "error" {
			report.OK = false
			break
		}
	}
	return report
}

func checkWritable(root string) error {
	file, err := os.CreateTemp(root, ".doctor-")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func checkRepository(repositoryURL string) error {
	request, err := http.NewRequest(http.MethodHead, repositoryURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return fmt.Errorf("GitHub returned %s", response.Status)
	}
	return nil
}

func panelIsHealthy(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/api/health")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
