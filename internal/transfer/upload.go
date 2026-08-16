package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/jobs"
)

// UploadOptions tunes one upload job. Key is never serialized into summaries.
type UploadOptions struct {
	Concurrency int    `json:"concurrency"`
	BatchSize   int    `json:"batchSize"`
	TimeoutMs   int    `json:"timeoutMs"`
	RetryLimit  int    `json:"retryLimit"`
	SkipCached  bool   `json:"skipCached"`
	BaseURL     string `json:"baseUrl"`
	Source      string `json:"source,omitempty"`
	RunID       string `json:"runId,omitempty"`

	Key           string `json:"-"`
	SkipCachedSet bool   `json:"-"`
}

// UploadFile is one in-memory credential supplied by pool and auto-upload callers.
type UploadFile struct {
	Name    string
	Content []byte
}

type uploadStart struct {
	job        *jobs.Job
	onlyFailed bool
}

type UploadSuccessHook func(name string) error
type UploadFailureHook func(name, message string)

// uploadRequest carries prepare inputs from either multipart or JSON body.
type uploadRequest struct {
	FolderPath string
	RawJSON    string
	Files      []struct {
		Filename string
		Data     []byte
	}
	Overrides        UploadOptions
	HasSkipCachedSet bool
}

// PrepareUpload parses the prepare request (multipart files / folderPath /
// rawJson), builds candidates, pre-checks the upload cache, and registers a
// queued job.
func (s *Service) PrepareUpload(r *http.Request) (*jobs.Job, error) {
	req, err := parseUploadRequest(r)
	if err != nil {
		return nil, err
	}
	var cands []candidate
	var srcErrs []string

	for _, f := range req.Files {
		lower := strings.ToLower(f.Filename)
		switch {
		case strings.HasSuffix(lower, ".zip"):
			cs, err := fromZip(f.Data)
			if err != nil {
				srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", f.Filename, err))
				continue
			}
			cands = append(cands, cs...)
		case strings.HasSuffix(lower, ".json"):
			c, err := parseCredential(f.Data, f.Filename)
			if err != nil {
				srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", f.Filename, err))
				continue
			}
			cands = append(cands, c)
		default:
			srcErrs = append(srcErrs, fmt.Sprintf("%s: 仅支持 .json / .zip", f.Filename))
		}
	}
	if strings.TrimSpace(req.FolderPath) != "" {
		cs, err := fromFolder(req.FolderPath)
		if err != nil {
			srcErrs = append(srcErrs, err.Error())
		} else {
			cands = append(cands, cs...)
		}
	}
	if strings.TrimSpace(req.RawJSON) != "" {
		cs, err := fromRawJSON(req.RawJSON)
		if err != nil {
			srcErrs = append(srcErrs, err.Error())
		} else {
			cands = append(cands, cs...)
		}
	}

	return s.prepareUploadCandidates(cands, srcErrs, req.Overrides, req.HasSkipCachedSet)
}

// PrepareUploadFiles creates a queued job from already-resolved credentials.
func (s *Service) PrepareUploadFiles(files []UploadFile, opts UploadOptions) (*jobs.Job, error) {
	cands := make([]candidate, 0, len(files))
	errs := make([]string, 0)
	for _, file := range files {
		cand, err := parseCredential(file.Content, file.Name)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", file.Name, err))
			continue
		}
		cands = append(cands, cand)
	}
	return s.prepareUploadCandidates(cands, errs, opts, opts.SkipCachedSet)
}

// PrepareUploadFolder creates a queued job from one server-local credential directory.
func (s *Service) PrepareUploadFolder(folder string, opts UploadOptions) (*jobs.Job, error) {
	cands, err := fromFolder(folder)
	if err != nil {
		return nil, err
	}
	return s.prepareUploadCandidates(cands, nil, opts, opts.SkipCachedSet)
}

func (s *Service) prepareUploadCandidates(cands []candidate, srcErrs []string, opts UploadOptions, hasSkipCachedSet bool) (*jobs.Job, error) {
	cands = dedupeCandidates(cands)
	if len(cands) == 0 {
		if len(srcErrs) > 0 {
			return nil, fmt.Errorf("没有可用凭证: %s", strings.Join(srcErrs[:min(3, len(srcErrs))], "; "))
		}
		return nil, fmt.Errorf("没有可用凭证：请上传 .json/.zip、填写目录或粘贴 JSON")
	}

	_, _, defs := s.cfgFn()
	opts.Concurrency = clampInt(opts.Concurrency, defs.UploadConcurrency, 1, 100)
	opts.BatchSize = clampInt(opts.BatchSize, defs.UploadBatchSize, 1, 500)
	opts.TimeoutMs = clampInt(opts.TimeoutMs, defs.TimeoutMs, 3000, 300000)
	opts.RetryLimit = clampInt(opts.RetryLimit, defs.RetryLimit, 0, 10)
	if !hasSkipCachedSet {
		opts.SkipCached = true
	}
	if strings.TrimSpace(opts.Source) == "" {
		opts.Source = "manual"
	}
	conn := s.ResolveConnection(opts.BaseURL, opts.Key)
	opts.BaseURL = conn.BaseURL
	opts.Key = conn.Key

	items := toItems(cands)
	job := jobs.NewJob("upload", items)
	if opts.SkipCached {
		job.WithItems(func(items []*jobs.Item) {
			for _, item := range items {
				key := CacheKey(conn.BaseURL, item.Name, item.Content)
				if s.Cache.Has(key) {
					item.Status = jobs.ItemSkipped
					item.FromCache = true
					item.Content = nil
					item.Error = "本地缓存：此前已上传成功"
				}
			}
		})
	}
	s.uploadOpts.Store(job.ID, opts)
	s.UploadJobs.Add(job)
	if len(srcErrs) > 0 {
		job.AddLog("部分来源解析失败: %s", strings.Join(srcErrs[:min(3, len(srcErrs))], "; "))
	}
	job.AddLog("任务已创建：共 %d 项，来源 %s", len(items), opts.Source)
	return job, nil
}

// UploadJobOptions returns the stored options for a job.
func (s *Service) UploadJobOptions(jobID string) (UploadOptions, bool) {
	v, ok := s.uploadOpts.Load(jobID)
	if !ok {
		return UploadOptions{}, false
	}
	return v.(UploadOptions), true
}

// SetUploadSuccessHook attaches source bookkeeping to one upload job.
func (s *Service) SetUploadSuccessHook(jobID string, hook UploadSuccessHook) {
	if hook == nil {
		s.uploadHooks.Delete(jobID)
		return
	}
	s.uploadHooks.Store(jobID, hook)
}

// SetUploadFailureHook attaches source-specific failure bookkeeping.
func (s *Service) SetUploadFailureHook(jobID string, hook UploadFailureHook) {
	if hook == nil {
		s.uploadFailureHooks.Delete(jobID)
		return
	}
	s.uploadFailureHooks.Store(jobID, hook)
}

// StartUploadJob appends one job to the process-wide serial upload queue.
func (s *Service) StartUploadJob(job *jobs.Job, onlyFailed bool) bool {
	s.uploadQueueMu.Lock()
	if _, exists := s.uploadQueued[job.ID]; exists {
		s.uploadQueueMu.Unlock()
		return false
	}
	status := job.GetStatus()
	if status == jobs.StatusRunning || status == jobs.StatusCancelled || (status == jobs.StatusCompleted && !onlyFailed) {
		s.uploadQueueMu.Unlock()
		return false
	}
	if onlyFailed {
		hasFailed := false
		for _, item := range job.ItemValues() {
			if item.Status == jobs.ItemFailed {
				hasFailed = true
				break
			}
		}
		if !hasFailed {
			s.uploadQueueMu.Unlock()
			return false
		}
	}
	job.SetStatus(jobs.StatusQueued)
	s.uploadQueued[job.ID] = struct{}{}
	s.uploadQueue = append(s.uploadQueue, uploadStart{job: job, onlyFailed: onlyFailed})
	s.uploadQueueCond.Signal()
	s.uploadQueueMu.Unlock()
	job.AddLog("已进入上传队列")
	job.Broadcast(s.UploadSummary(job))
	return true
}

func (s *Service) runUploadQueue() {
	for {
		s.uploadQueueMu.Lock()
		for len(s.uploadQueue) == 0 {
			s.uploadQueueCond.Wait()
		}
		next := s.uploadQueue[0]
		s.uploadQueue = s.uploadQueue[1:]
		delete(s.uploadQueued, next.job.ID)
		s.uploadQueueMu.Unlock()

		if next.job.GetStatus() == jobs.StatusCancelled {
			continue
		}
		next.job.SetStatus(jobs.StatusRunning)
		next.job.AddLog("队列调度：开始执行")
		next.job.Broadcast(s.UploadSummary(next.job))
		s.runUploadJob(next.job, mustUploadOptions(s, next.job.ID), next.onlyFailed)
	}
}

func mustUploadOptions(s *Service, jobID string) UploadOptions {
	opts, _ := s.UploadJobOptions(jobID)
	return opts
}

// UploadQueuePosition returns a one-based pending position; running jobs return zero.
func (s *Service) UploadQueuePosition(jobID string) int {
	s.uploadQueueMu.Lock()
	defer s.uploadQueueMu.Unlock()
	for index, pending := range s.uploadQueue {
		if pending.job.ID == jobID {
			return index + 1
		}
	}
	return 0
}

func (s *Service) runUploadJob(job *jobs.Job, opts UploadOptions, onlyFailed bool) {
	// Reset failed items to pending; when not onlyFailed, also re-queue
	// everything that never finished.
	job.WithItems(func(items []*jobs.Item) {
		for _, it := range items {
			switch {
			case onlyFailed && it.Status == jobs.ItemFailed:
				it.Status = jobs.ItemPending
				it.Error = ""
			case !onlyFailed && (it.Status == jobs.ItemPending || it.Status == jobs.ItemFailed):
				it.Status = jobs.ItemPending
				it.Error = ""
			}
		}
	})

	conn := Connection{
		BaseURL:    opts.BaseURL,
		Key:        opts.Key,
		TimeoutMs:  opts.TimeoutMs,
		RetryLimit: opts.RetryLimit,
	}
	client := s.NewClient(conn)

	waveSize := max(1, opts.BatchSize)
	parallel := opts.Concurrency
	if waveSize > parallel {
		parallel = waveSize
	}
	if parallel > 100 {
		parallel = 100
	}

	items := job.Items()
	total := len(items)
	job.AddLog("开始上传：%d 项，每批 %d，并发 %d", total, waveSize, parallel)
	job.Broadcast(s.UploadSummary(job))

	for start := 0; start < total; start += waveSize {
		if job.Context().Err() != nil {
			break
		}
		end := min(start+waveSize, total)
		wave := items[start:end]

		jobs.RunPool(job.Context(), len(wave), parallel, func(i int) {
			it := wave[i]
			idx := start + i
			// Only process pending items (respects cache-skipped & done items).
			skip := true
			job.MutateItem(idx, func(cur *jobs.Item) {
				if cur.Status == jobs.ItemPending {
					cur.Status = jobs.ItemRunning
					now := time.Now()
					cur.StartedAt = &now
					skip = false
				}
			})
			if skip {
				return
			}
			job.Broadcast(s.UploadSummary(job))

			var content []byte
			job.MutateItem(idx, func(cur *jobs.Item) { content = cur.Content })

			var lastErr string
			attempts := 1 + opts.RetryLimit
			for a := 1; a <= attempts; a++ {
				if job.Context().Err() != nil {
					break
				}
				if a > 1 {
					timer := time.NewTimer(time.Duration(300*a) * time.Millisecond)
					select {
					case <-job.Context().Done():
						timer.Stop()
					case <-timer.C:
					}
					if job.Context().Err() != nil {
						break
					}
				}
				job.MutateItem(idx, func(cur *jobs.Item) { cur.Attempts = a })

				res := client.UploadOnce(it.Name, content)
				if res.OK {
					if value, exists := s.uploadHooks.Load(job.ID); exists {
						if hookErr := value.(UploadSuccessHook)(it.Name); hookErr != nil {
							lastErr = hookErr.Error()
							continue
						}
					}
					key := CacheKey(opts.BaseURL, it.Name, content)
					var email, typ string
					if it.Preview != nil {
						email, typ = it.Preview["email"], it.Preview["type"]
					}
					s.Cache.Remember(key, CacheEntry{
						Name: it.Name, Hash: key, BaseURL: opts.BaseURL,
						Email: email, Type: typ, Size: it.Size, UploadedAt: time.Now(),
					})
					job.MutateItem(idx, func(cur *jobs.Item) {
						cur.Status = jobs.ItemSuccess
						cur.Content = nil
						now := time.Now()
						cur.FinishedAt = &now
					})
					job.AddLog("✓ %s", it.Name)
					lastErr = ""
					break
				}
				lastErr = uploadErrorMessage(res)
			}
			if lastErr != "" {
				job.MutateItem(idx, func(cur *jobs.Item) {
					cur.Status = jobs.ItemFailed
					cur.Error = lastErr
					now := time.Now()
					cur.FinishedAt = &now
				})
				job.AddLog("✗ %s: %s", it.Name, lastErr)
				if value, exists := s.uploadFailureHooks.Load(job.ID); exists {
					value.(UploadFailureHook)(it.Name, lastErr)
				}
			}
			job.Broadcast(s.UploadSummary(job))
		})
	}

	if job.Context().Err() != nil {
		job.SetStatus(jobs.StatusCancelled)
		job.AddLog("任务已取消")
	} else {
		job.SetStatus(jobs.StatusCompleted)
		counts := uploadCounts(job.ItemValues())
		job.AddLog("任务完成：成功 %d，失败 %d，跳过 %d", counts.Success, counts.Failed, counts.Skipped)
	}
	job.Broadcast(s.UploadSummary(job))
}

// uploadErrorMessage extracts a friendly error from an UploadResult.
func uploadErrorMessage(res cpa.UploadResult) string {
	if res.Err != nil {
		msg := res.Err.Error()
		low := strings.ToLower(msg)
		switch {
		case strings.Contains(low, "timeout") || strings.Contains(low, "deadline"):
			return "请求超时"
		case strings.Contains(low, "connection refused") || strings.Contains(low, "no such host"):
			return "无法连接 CPA（" + msg + "）"
		}
		return msg
	}
	// HTTP error: try to pull error/message out of the body.
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Status  string `json:"status"`
	}
	if json.Unmarshal([]byte(res.Body), &body) == nil {
		for _, s := range []string{body.Error, body.Message, body.Status} {
			if s != "" {
				return s
			}
		}
	}
	hint := ""
	if strings.Contains(strings.ToLower(res.Body), "ban") {
		hint = "（可能触发 CPA 限流，约 30 分钟）"
	}
	return fmt.Sprintf("HTTP %d%s", res.Status, hint)
}

// itemCounts is the per-status tally.
type itemCounts struct {
	Pending   int `json:"pending"`
	Uploading int `json:"uploading"`
	Success   int `json:"success"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

func uploadCounts(items []jobs.Item) itemCounts {
	var c itemCounts
	for _, it := range items {
		switch it.Status {
		case jobs.ItemPending:
			c.Pending++
		case jobs.ItemRunning:
			c.Uploading++
		case jobs.ItemSuccess:
			c.Success++
		case jobs.ItemFailed:
			c.Failed++
		case jobs.ItemSkipped:
			c.Skipped++
		}
	}
	return c
}

// UploadSummary is the SSE/snapshot payload for an upload job.
type UploadSummary struct {
	ID            string        `json:"id"`
	Kind          string        `json:"kind"`
	Status        jobs.Status   `json:"status"`
	CreatedAt     time.Time     `json:"createdAt"`
	StartedAt     *time.Time    `json:"startedAt,omitempty"`
	FinishedAt    *time.Time    `json:"finishedAt,omitempty"`
	Options       UploadOptions `json:"options"`
	QueuePosition int           `json:"queuePosition,omitempty"`
	Total         int           `json:"total"`
	Done          int           `json:"done"`
	Progress      int           `json:"progress"`
	Counts        itemCounts    `json:"counts"`
	Logs          []string      `json:"logs"`
	Items         []jobs.Item   `json:"items"`
}

// UploadSummary builds the public summary for a job.
func (s *Service) UploadSummary(job *jobs.Job) UploadSummary {
	items := job.ItemValues()
	counts := uploadCounts(items)
	done := counts.Success + counts.Failed + counts.Skipped
	progress := 0
	if len(items) > 0 {
		progress = done * 100 / len(items)
	}
	opts, _ := s.UploadJobOptions(job.ID)
	pub := opts
	pub.Key = ""
	logs := job.Logs()
	if len(logs) > 80 {
		logs = logs[len(logs)-80:]
	}
	return UploadSummary{
		ID: job.ID, Kind: "upload", Status: job.GetStatus(),
		CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
		Options: pub, QueuePosition: s.UploadQueuePosition(job.ID),
		Total: len(items), Done: done, Progress: progress,
		Counts: counts, Logs: logs, Items: items,
	}
}

// parseUploadRequest reads multipart or JSON prepare bodies.
func parseUploadRequest(r *http.Request) (uploadRequest, error) {
	var req uploadRequest
	ct := r.Header.Get("Content-Type")
	mt, _, _ := mime.ParseMediaType(ct)

	if mt == "application/json" {
		var body struct {
			FolderPath  string `json:"folderPath"`
			RawJSON     string `json:"rawJson"`
			BaseURL     string `json:"baseUrl"`
			Key         string `json:"managementKey"`
			Concurrency int    `json:"concurrency"`
			BatchSize   int    `json:"batchSize"`
			TimeoutMs   int    `json:"timeoutMs"`
			RetryLimit  int    `json:"retryLimit"`
			SkipCached  *bool  `json:"skipCached"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxPrepareTotalSz)).Decode(&body); err != nil {
			return req, fmt.Errorf("JSON 解析失败: %v", err)
		}
		req.FolderPath = body.FolderPath
		req.RawJSON = body.RawJSON
		req.Overrides = UploadOptions{
			Concurrency: body.Concurrency, BatchSize: body.BatchSize,
			TimeoutMs: body.TimeoutMs, RetryLimit: body.RetryLimit,
			BaseURL: body.BaseURL, Key: body.Key,
		}
		if body.SkipCached != nil {
			req.Overrides.SkipCached = *body.SkipCached
			req.HasSkipCachedSet = true
		}
		return req, nil
	}

	if mt != "multipart/form-data" {
		return req, fmt.Errorf("不支持的 Content-Type: %s", ct)
	}

	mr, err := r.MultipartReader()
	if err != nil {
		return req, fmt.Errorf("multipart 解析失败: %v", err)
	}
	fileCount := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return req, fmt.Errorf("multipart 读取失败: %v", err)
		}
		name := part.FormName()
		if name == "files" {
			fileCount++
			if fileCount > maxPrepareFiles {
				return req, fmt.Errorf("文件数超过上限 %d", maxPrepareFiles)
			}
			data, err := io.ReadAll(io.LimitReader(part, maxPrepareFileSz+1))
			part.Close()
			if err != nil {
				return req, fmt.Errorf("读取文件失败: %v", err)
			}
			if len(data) > maxPrepareFileSz {
				return req, fmt.Errorf("文件 %s 超过 80MB 上限", part.FileName())
			}
			req.Files = append(req.Files, struct {
				Filename string
				Data     []byte
			}{Filename: part.FileName(), Data: data})
			continue
		}
		// plain form field
		val, _ := io.ReadAll(io.LimitReader(part, 4<<20))
		part.Close()
		s := strings.TrimSpace(string(val))
		switch name {
		case "folderPath":
			req.FolderPath = s
		case "rawJson":
			req.RawJSON = string(val) // keep whitespace for JSON
		case "baseUrl":
			req.Overrides.BaseURL = s
		case "managementKey":
			req.Overrides.Key = s
		case "concurrency":
			req.Overrides.Concurrency, _ = strconv.Atoi(s)
		case "batchSize":
			req.Overrides.BatchSize, _ = strconv.Atoi(s)
		case "timeoutMs":
			req.Overrides.TimeoutMs, _ = strconv.Atoi(s)
		case "retryLimit":
			req.Overrides.RetryLimit, _ = strconv.Atoi(s)
		case "skipCached":
			req.HasSkipCachedSet = true
			req.Overrides.SkipCached = !(s == "false" || s == "0")
		}
	}
	return req, nil
}
