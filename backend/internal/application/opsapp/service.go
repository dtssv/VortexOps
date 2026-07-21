// Package opsapp 是运维操作（Pod exec / port-forward）应用服务，含审计记录。
package opsapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/application/auditapp"
	"github.com/vortexops/vortexops/internal/domain/audit"
	"github.com/vortexops/vortexops/internal/domain/behavioraudit"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	"github.com/vortexops/vortexops/internal/domain/opssession"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	k8sexec "github.com/vortexops/vortexops/internal/infrastructure/k8s/exec"
	"github.com/vortexops/vortexops/internal/infrastructure/s3"
	"github.com/vortexops/vortexops/internal/platform/security"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 运维应用服务。
type Service struct {
	clusterRepo cluster.Repository
	cipher      *security.FieldCipher
	pool        *k8s.ClientPool
	sessions    *k8sexec.SessionManager
	audit       *auditapp.Service
	// 可选依赖：会话持久化、录像存储、行为审计。nil 时降级（不落库/不录像）。
	sessionRepo  opssession.Repository
	logStore     *s3.LogStore
	behaviorRepo behavioraudit.Repository
}

// New 创建运维服务。
func New(
	clusterRepo cluster.Repository,
	cipher *security.FieldCipher,
	pool *k8s.ClientPool,
	sessions *k8sexec.SessionManager,
	auditSvc *auditapp.Service,
) *Service {
	return &Service{
		clusterRepo: clusterRepo,
		cipher:      cipher,
		pool:        pool,
		sessions:    sessions,
		audit:       auditSvc,
	}
}

// WithSessionRepo 注入运维会话持久化（启用录像元数据落库）。
func (s *Service) WithSessionRepo(r opssession.Repository) *Service {
	s.sessionRepo = r
	return s
}

// WithLogStore 注入 S3 日志存储（启用 asciinema 录像上传）。
func (s *Service) WithLogStore(l *s3.LogStore) *Service {
	s.logStore = l
	return s
}

// WithBehaviorRepo 注入行为审计仓储（启用 WebSSH 命令捕获）。
func (s *Service) WithBehaviorRepo(r behavioraudit.Repository) *Service {
	s.behaviorRepo = r
	return s
}

// ExecInput Pod exec 输入。
type ExecInput struct {
	UserID    int64
	UserName  string
	ClusterID int64
	Namespace string
	Pod       string
	Container string
	Command   []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	TTY       bool
}

// Exec 在 Pod 中执行命令并记录审计。
func (s *Service) Exec(ctx context.Context, in ExecInput) error {
	if in.ClusterID == 0 || in.Namespace == "" || in.Pod == "" {
		return apperr.Validation("cluster_id, namespace and pod are required", nil)
	}
	entry, err := s.clientForCluster(ctx, in.ClusterID)
	if err != nil {
		return err
	}
	sessionID := uuid.New().String()
	err = s.sessions.Exec(ctx, entry.RestConfig, entry.Clientset, k8sexec.ExecOptions{
		SessionID: sessionID,
		ClusterID: in.ClusterID,
		Namespace: in.Namespace,
		Pod:       in.Pod,
		Container: in.Container,
		Command:   in.Command,
		Stdin:     in.Stdin,
		Stdout:    in.Stdout,
		Stderr:    in.Stderr,
		TTY:       in.TTY,
	})
	s.recordAudit(ctx, in.UserID, in.UserName, in.ClusterID, in.Namespace, in.Pod, "exec", err)
	return err
}

// StartPortForward 非阻塞启动端口转发，返回分配的本地端口与会话 ID。
// 转发在后台 goroutine 运行，可通过 CloseSession(sessionID) 终止。
func (s *Service) StartPortForward(ctx context.Context, in PortForwardStartInput) (*PortForwardResult, error) {
	if in.ClusterID == 0 || in.Namespace == "" || in.Pod == "" || in.Port == 0 {
		return nil, apperr.Validation("cluster_id, namespace, pod and port are required", nil)
	}
	entry, err := s.clientForCluster(ctx, in.ClusterID)
	if err != nil {
		return nil, err
	}
	sessionID, localPort, err := s.sessions.StartPortForward(ctx, entry.RestConfig, entry.Clientset, k8sexec.PortForwardOptions{
		ClusterID: in.ClusterID, Namespace: in.Namespace, Pod: in.Pod,
		Ports: []string{strconv.Itoa(in.Port)},
	}, in.LocalPort)
	if err != nil {
		return nil, apperr.Internal("start port forward", err)
	}
	s.recordAudit(ctx, in.UserID, in.UserName, in.ClusterID, in.Namespace, in.Pod, "port_forward", err)
	return &PortForwardResult{
		SessionID: sessionID, LocalPort: localPort, RemotePort: in.Port,
		LocalAddr: fmt.Sprintf("127.0.0.1:%d", localPort),
	}, nil
}

// PortForwardInput 端口转发输入。
type PortForwardInput struct {
	UserID    int64
	UserName  string
	ClusterID int64
	Namespace string
	Pod       string
	Ports     []string
	StopChan  <-chan struct{}
	Out       io.Writer
	ErrOut    io.Writer
}

// PortForwardStartInput 非阻塞端口转发输入。
type PortForwardStartInput struct {
	UserID    int64
	UserName  string
	ClusterID int64
	Namespace string
	Pod       string
	Port      int  // Pod 内目标端口
	LocalPort int  // 本地端口；0 自动分配
}

// PortForwardResult 端口转发启动结果。
type PortForwardResult struct {
	SessionID  string `json:"session_id"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
	LocalAddr  string `json:"local_addr"`
}

// PortForward 建立端口转发并记录审计。
func (s *Service) PortForward(ctx context.Context, in PortForwardInput) error {
	if in.ClusterID == 0 || in.Namespace == "" || in.Pod == "" || len(in.Ports) == 0 {
		return apperr.Validation("cluster_id, namespace, pod and ports are required", nil)
	}
	entry, err := s.clientForCluster(ctx, in.ClusterID)
	if err != nil {
		return err
	}
	sessionID := uuid.New().String()
	err = s.sessions.PortForward(ctx, entry.RestConfig, entry.Clientset, k8sexec.PortForwardOptions{
		SessionID: sessionID,
		ClusterID: in.ClusterID,
		Namespace: in.Namespace,
		Pod:       in.Pod,
		Ports:     in.Ports,
		StopChan:  in.StopChan,
		Out:       in.Out,
		ErrOut:    in.ErrOut,
	})
	s.recordAudit(ctx, in.UserID, in.UserName, in.ClusterID, in.Namespace, in.Pod, "port_forward", err)
	return err
}

// ListSessions 列出活跃运维会话。
func (s *Service) ListSessions() []*k8sexec.Session {
	return s.sessions.List()
}

// CloseSession 关闭运维会话。
func (s *Service) CloseSession(id string) bool {
	return s.sessions.Close(id)
}

func (s *Service) clientForCluster(ctx context.Context, clusterID int64) (*k8s.ClientEntry, error) {
	if entry, err := s.pool.Get(clusterID); err == nil {
		return entry, nil
	}
	c, err := s.clusterRepo.GetClusterByID(ctx, clusterID)
	if err != nil {
		if errors.Is(err, cluster.ErrClusterNotFound) {
			return nil, apperr.NotFound("cluster", fmt.Sprintf("%d", clusterID))
		}
		return nil, apperr.Internal("get cluster", err)
	}
	raw, err := s.cipher.Decrypt(c.KubeconfigEncrypted)
	if err != nil {
		return nil, apperr.Internal("decrypt kubeconfig", err)
	}
	entry, err := s.pool.GetOrCreate(clusterID, raw, c.InsecureSkipTLS)
	if err != nil {
		return nil, apperr.Internal("build k8s client", err)
	}
	return entry, nil
}

func (s *Service) recordAudit(ctx context.Context, userID int64, userName string, clusterID int64, ns, pod, op string, execErr error) {
	if s.audit == nil {
		return
	}
	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
	}
	s.audit.Record(ctx, auditapp.RecordInput{
		UserID: userID, UserName: userName, ResourceType: "pod", ResourceName: ns + "/" + pod,
		Action: audit.ActionRead, Operation: op,
		RequestBody: map[string]any{"cluster_id": clusterID, "namespace": ns, "pod": pod},
		ErrorMessage: errMsg,
	})
}

// --- WebSSH 交互式 exec ---

// WSExecInput WebSocket 交互式 Pod exec 输入。
type WSExecInput struct {
	UserID      int64
	UserName    string
	WorkspaceID int64
	ClusterID   int64
	Namespace   string
	Pod         string
	Container   string
	Command     []string
	ClientIP    string
}

// HandleWSExec 处理 WebSocket 升级并启动交互式 TTY exec，含录像与会话落库。
// Pod 登录审计在连接建立时记录一次；会话结束时回写 duration 与 recording_key。
func (s *Service) HandleWSExec(ctx context.Context, w http.ResponseWriter, r *http.Request, in WSExecInput) error {
	if in.ClusterID == 0 || in.Namespace == "" || in.Pod == "" {
		return apperr.Validation("cluster_id, namespace and pod are required", nil)
	}
	entry, err := s.clientForCluster(ctx, in.ClusterID)
	if err != nil {
		return err
	}

	// Pod 登录审计（每次连接一次）。
	if s.audit != nil {
		s.audit.Record(ctx, auditapp.RecordInput{
			UserID: in.UserID, UserName: in.UserName, ResourceType: "pod",
			ResourceName: in.Namespace + "/" + in.Pod,
			Action: audit.ActionPodLogin, Operation: "pod_login",
			RequestBody: map[string]any{
				"cluster_id": in.ClusterID, "namespace": in.Namespace,
				"pod": in.Pod, "container": in.Container, "client_ip": in.ClientIP,
			},
		})
	}

	// 创建会话记录 + 录像器。
	var sess *opssession.Session
	var recorder k8sexec.SessionRecorder
	recordingKey := ""
	if s.sessionRepo != nil {
		sess = &opssession.Session{
			WorkspaceID: in.WorkspaceID, ClusterID: in.ClusterID,
			Namespace: in.Namespace, Pod: in.Pod, Container: in.Container,
			Type: opssession.TypeExec, Status: opssession.StatusActive,
			UserID: in.UserID, UserName: in.UserName, ClientIP: in.ClientIP,
		}
		if err := s.sessionRepo.Create(ctx, sess); err != nil {
			sess = nil // 落库失败不阻断 exec。
		}
	}
	if s.logStore != nil && sess != nil {
		recordingKey = fmt.Sprintf("sessions/pod/%s/%d.cast", time.Now().Format("20060102"), sess.ID)
		recorder = s3.NewCastRecorder(ctx, s.logStore, recordingKey)
	}

	start := time.Now()
	var sessionID int64
	if sess != nil {
		sessionID = sess.ID
	}
	execErr := s.sessions.ExecInteractive(ctx, w, r, entry.RestConfig, entry.Clientset, k8sexec.ExecInteractiveInput{
		ClusterID: in.ClusterID, Namespace: in.Namespace, Pod: in.Pod,
		Container: in.Container, Command: in.Command,
		UserID: in.UserID, UserName: in.UserName, Recorder: recorder,
		StdinWatcher: &stdinCommandWatcher{
			audit: s.audit, behaviorRepo: s.behaviorRepo, ctx: context.Background(),
			userID: in.UserID, userName: in.UserName,
			workspaceID: in.WorkspaceID, clusterID: in.ClusterID,
			namespace: in.Namespace, pod: in.Pod, container: in.Container,
			sessionID: sessionID,
		},
	})

	// 会话结束回写。
	if sess != nil && s.sessionRepo != nil {
		end := time.Now()
		sess.Status = opssession.StatusClosed
		sess.EndedAt = &end
		sess.DurationMs = end.Sub(start).Milliseconds()
		sess.RecordingKey = recordingKey
		_ = s.sessionRepo.Update(ctx, sess)
	}
	return execErr
}

// ListOpsSessions 列出运维会话（落库的历史记录）。
func (s *Service) ListOpsSessions(ctx context.Context, q opssession.Query) ([]*opssession.Session, int64, error) {
	if s.sessionRepo == nil {
		return nil, 0, apperr.Internal("ops session persistence not configured", nil)
	}
	return s.sessionRepo.List(ctx, q)
}

// GetOpsSession 获取单个运维会话。
func (s *Service) GetOpsSession(ctx context.Context, id int64) (*opssession.Session, error) {
	if s.sessionRepo == nil {
		return nil, apperr.Internal("ops session persistence not configured", nil)
	}
	return s.sessionRepo.GetByID(ctx, id)
}

// PresignReplay 为会话录像生成预签名下载 URL。
func (s *Service) PresignReplay(ctx context.Context, key string) (string, error) {
	if s.logStore == nil {
		return "", apperr.Internal("recording storage not configured", nil)
	}
	return s.logStore.PresignedGet(ctx, key, 30*time.Minute)
}

// StreamRecording 流式返回会话录像内容（io.ReadCloser，调用方负责 Close）。
// 用于后端代理下载 cast 文件，避免向浏览器暴露 MinIO 内部地址。
func (s *Service) StreamRecording(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.logStore == nil {
		return nil, apperr.Internal("recording storage not configured", nil)
	}
	return s.logStore.StreamDownload(ctx, key)
}

// AppendBehavior 捕获一条 WebSSH 命令行为。
func (s *Service) AppendBehavior(ctx context.Context, l *behavioraudit.Log) error {
	if s.behaviorRepo == nil {
		return nil
	}
	l.RiskLevel = classifyCommand(l.Command)
	return s.behaviorRepo.Append(ctx, l)
}

// ListBehavior 列出行为审计。
func (s *Service) ListBehavior(ctx context.Context, q behavioraudit.Query) ([]*behavioraudit.Log, int64, error) {
	if s.behaviorRepo == nil {
		return nil, 0, nil
	}
	return s.behaviorRepo.List(ctx, q)
}

// classifyCommand 简单命令风险分级。
func classifyCommand(cmd string) behavioraudit.RiskLevel {
	c := strings.ToLower(cmd)
	dangerKeywords := []string{"rm -rf", "mkfs", "dd if=", "shutdown", "reboot", ":(){", "chmod -r 777 /"}
	for _, kw := range dangerKeywords {
		if strings.Contains(c, kw) {
			return behavioraudit.RiskDanger
		}
	}
	warnKeywords := []string{"rm ", "kill ", "killall", "iptables", "sysctl -w", "passwd", "userdel", "useradd"}
	for _, kw := range warnKeywords {
		if strings.Contains(c, kw) {
			return behavioraudit.RiskWarn
		}
	}
	return behavioraudit.RiskInfo
}

// --- 文件浏览器（tar-over-exec）---

// FileEntry 文件条目。
type FileEntry struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Mode     string `json:"mode"`
	ModTime  string `json:"mod_time"`
	IsDir    bool   `json:"is_dir"`
}

// ListFiles 列出 Pod 内指定路径的文件（exec ls -la 解析）。
// 使用 BusyBox 兼容的 ls 命令格式。
func (s *Service) ListFiles(ctx context.Context, in PodFileInput) ([]FileEntry, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	cmd := []string{"ls", "-la", in.Path}
	out, err := s.execCapture(ctx, in, cmd, nil)
	if err != nil {
		return nil, apperr.Internal("list files", err)
	}
	return parseLsOutputSimple(out), nil
}

// DownloadFile 以 tar 流下载文件/目录（写入 out）。
func (s *Service) DownloadFile(ctx context.Context, in PodFileInput, out io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}
	cmd := []string{"tar", "cf", "-", "-C", dirOf(in.Path), baseOf(in.Path)}
	return s.Exec(ctx, ExecInput{
		UserID: in.UserID, UserName: in.UserName, ClusterID: in.ClusterID,
		Namespace: in.Namespace, Pod: in.Pod, Container: in.Container,
		Command: cmd, Stdout: out, TTY: false,
	})
}

// ReadFileContent 读取 Pod 内指定文件的文本内容（限制前 N 行/字节）。
func (s *Service) ReadFileContent(ctx context.Context, in PodFileInput, maxLines int) (string, error) {
	if err := in.validate(); err != nil {
		return "", err
	}
	if in.Path == "" {
		return "", apperr.Validation("path is required", nil)
	}
	if maxLines <= 0 {
		maxLines = 500
	}
	// Use head to limit lines, handle binary files gracefully.
	cmd := []string{"sh", "-c", fmt.Sprintf("head -n %d %s 2>/dev/null", maxLines, shellQuote(in.Path))}
	out, err := s.execCapture(ctx, in, cmd, nil)
	if err != nil {
		return "", apperr.Internal("read file", err)
	}
	return out, nil
}

// SearchLogPaths 在 Pod 内搜索匹配 glob 模式的日志文件路径。
func (s *Service) SearchLogPaths(ctx context.Context, in PodFileInput, pattern string) ([]string, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	if pattern == "" {
		pattern = "/var/log/**/*.log"
	}
	cmd := []string{"sh", "-c", fmt.Sprintf("find / -name %s -type f 2>/dev/null | head -100", shellQuote(pattern))}
	out, err := s.execCapture(ctx, in, cmd, nil)
	if err != nil {
		return nil, apperr.Internal("search log paths", err)
	}
	var paths []string
	for _, p := range strings.Split(out, "\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// UploadFile 从 stdin 上传 tar 流到 Pod 内指定目录。
func (s *Service) UploadFile(ctx context.Context, in PodFileInput, tarStream io.Reader) error {
	if err := in.validate(); err != nil {
		return err
	}
	if in.Path == "" {
		return apperr.Validation("path is required", nil)
	}
	cmd := []string{"tar", "xf", "-", "-C", in.Path}
	return s.Exec(ctx, ExecInput{
		UserID: in.UserID, UserName: in.UserName, ClusterID: in.ClusterID,
		Namespace: in.Namespace, Pod: in.Pod, Container: in.Container,
		Command: cmd, Stdin: tarStream, TTY: false,
	})
}

// DeleteFile 删除 Pod 内文件/目录（含路径守卫，禁止危险路径）。
func (s *Service) DeleteFile(ctx context.Context, in PodFileInput) error {
	if err := in.validate(); err != nil {
		return err
	}
	if err := guardDeletePath(in.Path); err != nil {
		return err
	}
	cmd := []string{"rm", "-rf", in.Path}
	_, err := s.execCapture(ctx, in, cmd, nil)
	if err != nil {
		return apperr.Internal("delete file", err)
	}
	return nil
}

// CleanupFiles 预设清理命令（清 /tmp、清日志、清 cache）。preset: tmp|logs|cache。
// 返回清理前的 du -sh 预览与清理后的 du -sh 结果。
func (s *Service) CleanupFiles(ctx context.Context, in PodFileInput, preset string) (map[string]string, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	var target string
	switch preset {
	case "tmp":
		target = "/tmp/*"
	case "logs":
		target = "/var/log/*"
	case "cache":
		target = "/var/cache/*"
	default:
		return nil, apperr.Validation("preset must be one of: tmp, logs, cache", nil)
	}
	result := map[string]string{}
	// 预览大小。
	if out, err := s.execCapture(ctx, in, []string{"sh", "-c", "du -sh " + target + " 2>/dev/null"}, nil); err == nil {
		result["before"] = out
	}
	// 清理。
	if _, err := s.execCapture(ctx, in, []string{"sh", "-c", "rm -rf " + target}, nil); err != nil {
		return result, apperr.Internal("cleanup "+preset, err)
	}
	// 清理后大小。
	if out, err := s.execCapture(ctx, in, []string{"sh", "-c", "du -sh " + target + " 2>/dev/null"}, nil); err == nil {
		result["after"] = out
	}
	return result, nil
}

// PodFileInput 文件操作输入。
type PodFileInput struct {
	UserID    int64
	UserName  string
	ClusterID int64
	Namespace string
	Pod       string
	Container string
	Path      string
}

func (in *PodFileInput) validate() error {
	if in.ClusterID == 0 || in.Namespace == "" || in.Pod == "" || in.Container == "" {
		return apperr.Validation("cluster_id, namespace, pod and container are required", nil)
	}
	return nil
}

// guardDeletePath 守卫危险删除路径：禁止根、通配、..、空。
func guardDeletePath(path string) error {
	if path == "" || path == "/" || path == "." {
		return apperr.Validation("refuse to delete root or empty path", nil)
	}
	if strings.Contains(path, "*") || strings.Contains(path, "..") {
		return apperr.Validation("wildcards and parent refs not allowed in delete path", nil)
	}
	return nil
}

// execCapture 执行命令并捕获 stdout 为字符串。
func (s *Service) execCapture(ctx context.Context, in PodFileInput, cmd []string, stdin io.Reader) (string, error) {
	var stdout, stderr bytes.Buffer
	err := s.Exec(ctx, ExecInput{
		UserID: in.UserID, UserName: in.UserName, ClusterID: in.ClusterID,
		Namespace: in.Namespace, Pod: in.Pod, Container: in.Container,
		Command: cmd, Stdin: stdin, Stdout: &stdout, Stderr: &stderr, TTY: false,
	})
	if err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// parseLsOutputSimple 解析标准 ls -la 输出（无 --time-style）。
// BusyBox 格式：mode links owner group size month day time/year name
func parseLsOutputSimple(out string) []FileEntry {
	var entries []FileEntry
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		mode := fields[0]
		if mode == "total" {
			continue
		}
		// BusyBox ls -la: fields[0]=mode fields[1]=nlink fields[2]=owner fields[3]=group
		// fields[4]=size fields[5]=month fields[6]=day fields[7]=time/year fields[8+]=name
		nameIdx := 8
		// Check if field[7] is a time (HH:MM) or year (YYYY); name starts at 8.
		// Some BusyBox versions: month day HH:MM name → 9 fields minimum for name.
		if len(fields) > 8 {
			nameIdx = 8
		}
		name := strings.Join(fields[nameIdx:], " ")
		name = strings.TrimSpace(name)
		if name == "" || name == "." || name == ".." {
			continue
		}
		var size int64
		if n, err := strconv.ParseInt(fields[4], 10, 64); err == nil {
			size = n
		}
		modTime := strings.Join(fields[5:nameIdx], " ")
		entries = append(entries, FileEntry{
			Name:    name,
			Size:    size,
			Mode:    mode,
			ModTime: modTime,
			IsDir:   strings.HasPrefix(mode, "d"),
		})
	}
	return entries
}

// parseLsOutputPipe 解析管道分隔的 ls+stat 输出。
// 格式：mode|nlink|owner|group|size|mtime_epoch|name
func parseLsOutputPipe(out string) []FileEntry {
	var entries []FileEntry
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 7)
		if len(parts) < 7 {
			continue
		}
		mode := parts[0]
		name := strings.TrimSpace(parts[6])
		if name == "" || name == "." || name == ".." {
			continue
		}
		var size int64
		if n, err := strconv.ParseInt(parts[4], 10, 64); err == nil {
			size = n
		}
		entries = append(entries, FileEntry{
			Name:    name,
			Size:    size,
			Mode:    mode,
			ModTime: parts[5],
			IsDir:   strings.HasPrefix(mode, "d"),
		})
	}
	return entries
}

// parseLsOutput 解析 `ls -la --time-style=+%Y %s` 输出为 FileEntry 列表（已弃用，保留兼容）。
func parseLsOutput(out string) []FileEntry {
	return parseLsOutputSimple(out)
}

// shellQuote 单引号转义路径参数。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func dirOf(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func baseOf(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

// --- 网络命令快捷 ---

// netCmdAllowlist 允许的网络诊断命令。
var netCmdAllowlist = map[string]bool{
	"ping": true, "curl": true, "nslookup": true, "dig": true,
	"netstat": true, "tracepath": true, "ip": true, "ifconfig": true, "traceroute": true,
}

// NetCmd 在 Pod 内执行 allowlist 网络诊断命令，返回 stdout+stderr。
func (s *Service) NetCmd(ctx context.Context, in PodFileInput, cmd string, args []string) (string, error) {
	if err := in.validate(); err != nil {
		return "", err
	}
	if !netCmdAllowlist[cmd] {
		return "", apperr.Validation("command not in allowlist: "+cmd, nil)
	}
	// Run via sh -c with timeout and redirect stderr to stdout.
	fullCmd := cmd + " " + strings.Join(args, " ")
	script := fmt.Sprintf("timeout 15 %s 2>&1 || true", fullCmd)
	out, err := s.execCapture(ctx, in, []string{"sh", "-c", script}, nil)
	if err != nil {
		return out, apperr.Internal("netcmd", err)
	}
	return out, nil
}

// NetCmdStream 流式执行网络诊断命令，stdout/stderr 实时写入 out。
// 相比 NetCmd（缓冲全部输出后一次性返回），避免长时间命令的等待。
// out 应为支持 Flush 的 http.ResponseWriter 或带 flusher 的 writer。
//
// 实现要点：
//   - 强制行缓冲：若容器有 stdbuf（coreutils）则用 stdbuf -oL -eL；否则直接执行。
//     非 TTY 下 ping/curl 等命令可能全缓冲，stdbuf 可解决；缺失时降级为默认缓冲
//     （busybox 版 ping 通常本身就行缓冲，影响有限）。
//   - timeout 60s 保护，避免长命令无限占用连接。
//   - 末尾输出 __EXIT__=<code> 标记行，前端据此剥离并展示退出状态；
//     同时该退出码会写入审计日志。
func (s *Service) NetCmdStream(ctx context.Context, in PodFileInput, cmd string, args []string, out io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}
	if !netCmdAllowlist[cmd] {
		return apperr.Validation("command not in allowlist: "+cmd, nil)
	}
	fullCmd := cmd + " " + strings.Join(args, " ")
	// 强制行缓冲，避免非 TTY 下 ping/curl 等命令全缓冲导致前端收不到流式输出。
	// 优先级：stdbuf -oL（coreutils，退出码准确）> awk 管道（busybox 自带，通用性最好，
	//   但管道退出码取自 awk 而非 cmd，命令失败时退出码可能不准确——netcmd 场景可接受）
	// > 直接执行（无行缓冲，输出攒到进程退出）。
	// timeout 60s 保护；2>&1 合并流；末尾打印退出码标记行供前端解析。
	// 注意：script 经 fmt.Sprintf 渲染，shell printf 的 %d 必须转义为 %%d。
	script := fmt.Sprintf(
		`code=0; `+
			`if command -v stdbuf >/dev/null 2>&1; then `+
			`stdbuf -oL -eL timeout 60 %s 2>&1 || code=$?; `+
			`elif command -v awk >/dev/null 2>&1; then `+
			`timeout 60 %s 2>&1 | awk '{print; fflush()}' || code=$?; `+
			`else timeout 60 %s 2>&1 || code=$?; fi; `+
			`if [ $code -eq 124 ]; then code=0; fi; `+ // timeout 自身退出码 124 不算错误（命令被超时）
			`printf '\n__EXIT__=%%d\n' $code`,
		fullCmd, fullCmd, fullCmd,
	)
	// stdinCtx：命令结束后立即取消，释放 ctxBlockingReader。
	// 用独立 ctx 而非外层 ctx，避免 reader 阻塞到 client 断开（goroutine 泄漏）。
	stdinCtx, stdinCancel := context.WithCancel(ctx)
	defer stdinCancel()
	execErr := s.Exec(ctx, ExecInput{
		UserID: in.UserID, UserName: in.UserName, ClusterID: in.ClusterID,
		Namespace: in.Namespace, Pod: in.Pod, Container: in.Container,
		Command: []string{"sh", "-c", script},
		// TTY=true：K8s exec 非 TTY 模式下 stdout 走 SPDY message，remotecommand 用 io.Copy
		// 攒 32KB buffer，小输出会攒到命令结束才 flush——前端感知不到流式。
		// TTY 模式下走 PTY，输出按行实时传输。提供阻塞不 EOF 的 stdin，避免 TTY 模式因
		// stdin EOF 提前结束 exec 流；命令结束后 stdout 关闭，StreamWithContext 正常返回，
		// 随后 stdinCtx 取消释放 reader。
		Stdin:  &ctxBlockingReader{ctx: stdinCtx},
		Stdout: out, Stderr: out, TTY: true,
	})
	// 命令审计：记录具体命令、参数、执行结果（exec 流本身的错误，如连接失败）。
	s.recordNetCmdAudit(ctx, in, cmd, args, execErr)
	return execErr
}

// recordNetCmdAudit 记录网络命令执行的审计日志。
func (s *Service) recordNetCmdAudit(ctx context.Context, in PodFileInput, cmd string, args []string, execErr error) {
	if s.audit == nil {
		return
	}
	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
	}
	s.audit.Record(ctx, auditapp.RecordInput{
		UserID:       in.UserID,
		UserName:     in.UserName,
		ResourceType: "pod",
		ResourceName: in.Namespace + "/" + in.Pod,
		Action:       audit.ActionExecute,
		Operation:    "netcmd",
		RequestBody: map[string]any{
			"cluster_id": in.ClusterID,
			"namespace":  in.Namespace,
			"pod":        in.Pod,
			"container":  in.Container,
			"cmd":        cmd,
			"args":       args,
		},
		ErrorMessage: errMsg,
	})
}

// stdinCommandWatcher 实现 k8sexec.StdinWatcher，把 WebSSH 会话中用户输入的
// 每条命令行实时记录到审计日志。回车触发 OnCommand，逐字符累积 + 控制序列剥离
// 在 k8sexec.ptyStream.observeStdin 中完成，这里只负责落审计。
//
// 注意：tab 补全/历史回溯等场景下，累积字符可能不等于最终执行命令；
// 完整精确的命令留痕依赖 asciinema cast 录像（含 i 事件），本审计提供实时可检索的近似命令行。
type stdinCommandWatcher struct {
	audit        *auditapp.Service
	behaviorRepo behavioraudit.Repository // 写入 vo_behavior_audit_logs（行为审计页查询源）
	ctx          context.Context          // 用独立 ctx，避免请求 ctx 在 WS 长连接结束后失效
	userID       int64
	userName     string
	workspaceID  int64
	clusterID    int64
	namespace    string
	pod          string
	container    string
	sessionID    int64 // 关联 vo_ops_sessions.id，便于按会话检索命令
}

func (w *stdinCommandWatcher) OnCommand(cmd string) {
	if cmd == "" {
		return
	}
	// 危险命令简单标记（仅标注，不阻断；阻断会破坏交互体验，由 RBAC/录像兜底）。
	risk := ""
	if isRiskyCommand(cmd) {
		risk = "risky"
	}
	// 1) 通用审计日志（vo_audit_logs）：可检索、与 HTTP 审计统一。
	if w.audit != nil {
		w.audit.Record(w.ctx, auditapp.RecordInput{
			UserID:       w.userID,
			UserName:     w.userName,
			ResourceType: "pod",
			ResourceName: w.namespace + "/" + w.pod,
			Action:       audit.ActionExecute,
			Operation:    "webssh_command",
			RequestBody: map[string]any{
				"cluster_id": w.clusterID,
				"namespace":  w.namespace,
				"pod":        w.pod,
				"container":  w.container,
				"command":    cmd,
				"risk":       risk,
			},
		})
	}
	// 2) 行为审计表（vo_behavior_audit_logs）：行为审计页查询源，含 session_id/风险分级。
	if w.behaviorRepo != nil {
		l := &behavioraudit.Log{
			UUID:        uuid.New(),
			WorkspaceID: w.workspaceID,
			SessionID:   w.sessionID,
			ClusterID:   w.clusterID,
			Namespace:   w.namespace,
			Pod:         w.pod,
			UserID:      w.userID,
			UserName:    w.userName,
			Command:     cmd,
		}
		l.RiskLevel = classifyCommand(cmd)
		// 落库失败不影响终端交互，仅记录错误日志。
		if err := w.behaviorRepo.Append(w.ctx, l); err != nil {
			log := slog.Default()
			log.Warn("append behavior audit failed", "err", err, "cmd", cmd)
		}
	}
}

// isRiskyCommand 粗略识别高风险命令，用于审计标注（不阻断）。
var riskyPatterns = []string{
	"rm -rf", "mkfs", "dd if=", "shutdown", "reboot", "halt",
	":(){", "fork bomb", "chmod -R 777", "curl | sh", "wget | sh",
}

func isRiskyCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range riskyPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// ctxBlockingReader 在 ctx 未取消前阻塞 Read（既不返回数据也不返回 EOF）。
// 用于 TTY 模式的 netcmd exec：避免空 stdin 立即 EOF 导致 exec 流提前结束。
// 命令结束后 stdout 关闭，remotecommand.StreamWithContext 返回，ctx 被取消，reader 释放。
type ctxBlockingReader struct {
	ctx context.Context
}

func (r *ctxBlockingReader) Read(p []byte) (int, error) {
	<-r.ctx.Done()
	return 0, io.EOF
}
