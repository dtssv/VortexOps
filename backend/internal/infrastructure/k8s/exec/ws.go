package exec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// wsClientMessage 浏览器 → 服务端控制帧。
type wsClientMessage struct {
	Type   string `json:"type"`              // stdin | resize | ping
	Data   string `json:"data,omitempty"`    // stdin 文本
	Cols   uint16 `json:"cols,omitempty"`    // resize 列
	Rows   uint16 `json:"rows,omitempty"`    // resize 行
}

// wsServerMessage 服务端 → 浏览器 帧。
type wsServerMessage struct {
	Type string `json:"type"` // stdout | stderr | exit | error
	Data string `json:"data,omitempty"`
	Code int    `json:"code,omitempty"` // exit code
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
}

// clearConnDeadlines 清除 net.Conn 上的读写 deadline，避免 http.Server 的 WriteTimeout/ReadTimeout
// 在 hijacked WebSocket 连接上生效导致连接被提前关闭。
func clearConnDeadlines(conn net.Conn) {
	if conn == nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})
}

// ptyStream 是连接 WebSocket 与 remotecommand 的双向桥，并实现 TerminalSizeQueue。
type ptyStream struct {
	ws        *websocket.Conn
	sizeQueue chan remotecommand.TerminalSize
	writeMu   sync.Mutex

	// stdin 命令审计：累积用户输入，遇到回车（\r）触发回调记录一行命令。
	// tab 补全/方向键等控制序列会被剥离，仅记录可打印字符构成的命令行。
	stdinWatcher StdinWatcher
	stdinBuf     []byte
	// stdinRecorder 旁路录制原始 stdin 到 asciinema cast 的 "i" 事件，
	// 用于完整回放与离线精确命令提取。nil 表示不录制 stdin。
	stdinRecorder SessionRecorder
}

// StdinWatcher 观察 stdin 输入，用于命令行审计。
// OnCommand 在用户按下回车时被调用，cmd 为本行可打印字符构成的命令（已剥离 ANSI/控制序列）。
type StdinWatcher interface {
	OnCommand(cmd string)
}

func newPTYStream(ws *websocket.Conn) *ptyStream {
	return &ptyStream{
		ws:        ws,
		sizeQueue: make(chan remotecommand.TerminalSize, 8),
	}
}

// Read 把 WebSocket stdin 帧写入 exec stdin。
func (p *ptyStream) Read(buf []byte) (int, error) {
	for {
		_, data, err := p.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		var msg wsClientMessage
		if json.Unmarshal(data, &msg) == nil {
			switch msg.Type {
			case "ping":
				continue
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					select {
					case p.sizeQueue <- remotecommand.TerminalSize{Width: msg.Cols, Height: msg.Rows}:
					default:
					}
				}
				continue
			case "stdin":
				p.observeStdin(msg.Data)
				n := copy(buf, []byte(msg.Data))
				return n, nil
			}
		}
		// 非 JSON 帧按裸 stdin 处理。
		if len(data) > 0 {
			p.observeStdin(string(data))
			n := copy(buf, data)
			return n, nil
		}
	}
}

// observeStdin 累积 stdin 字符用于命令审计。
// 处理规则：
//   - \r 或 \n：视为命令提交，把累积 buffer 作为一行命令回调，然后清空。
//   - \t（tab 补全）：保留一个空格占位（无法获知补全结果，但保留命令骨架）。
//   - \x7f（backspace）/ \b：删除最后一个字符。
//   - ANSI/控制序列（0x1b 开头到字母结束）：忽略。
//   - 其他可打印字符：追加。
func (p *ptyStream) observeStdin(s string) {
	// 原始 stdin 字节旁路录制到 cast 的 "i" 事件（完整回放 + 离线命令提取用）。
	if p.stdinRecorder != nil {
		_ = p.stdinRecorder.AppendEvent("i", s)
	}
	if p.stdinWatcher == nil {
		return
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\r' || c == '\n':
			cmd := string(p.stdinBuf)
			p.stdinBuf = p.stdinBuf[:0]
			cmd = strings.TrimSpace(cmd)
			if cmd != "" {
				p.stdinWatcher.OnCommand(cmd)
			}
		case c == '\t':
			p.stdinBuf = append(p.stdinBuf, ' ')
		case c == 0x7f || c == '\b':
			if len(p.stdinBuf) > 0 {
				p.stdinBuf = p.stdinBuf[:len(p.stdinBuf)-1]
			}
		case c == 0x03: // Ctrl+C：丢弃当前行。
			p.stdinBuf = p.stdinBuf[:0]
		case c == 0x15: // Ctrl+U：清行。
			p.stdinBuf = p.stdinBuf[:0]
		case c == 0x1b: // ANSI 转义序列：跳过到字母。
			for ; i < len(s); i++ {
				if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
					break
				}
			}
		case c >= 0x20 && c < 0x7f: // 可打印 ASCII。
			p.stdinBuf = append(p.stdinBuf, c)
		default:
			// 其他控制字符忽略。
		}
	}
}

// Write 把 exec stdout 回写到 WebSocket。
func (p *ptyStream) Write(data []byte) (int, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	msg := wsServerMessage{Type: "stdout", Data: string(data)}
	payload, _ := json.Marshal(msg)
	if err := p.ws.WriteMessage(websocket.TextMessage, payload); err != nil {
		return 0, err
	}
	return len(data), nil
}

// Next 实现 remotecommand.TerminalSizeQueue，阻塞返回最新终端尺寸。
func (p *ptyStream) Next() *remotecommand.TerminalSize {
	size, ok := <-p.sizeQueue
	if !ok {
		return nil
	}
	return &size
}

// writeError / writeExit 向浏览器推送错误与退出码帧。
func (p *ptyStream) writeError(msg string) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	payload, _ := json.Marshal(wsServerMessage{Type: "error", Data: msg})
	_ = p.ws.WriteMessage(websocket.TextMessage, payload)
}

func (p *ptyStream) writeExit(code int) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	payload, _ := json.Marshal(wsServerMessage{Type: "exit", Code: code})
	_ = p.ws.WriteMessage(websocket.TextMessage, payload)
}

// ExecInteractiveInput 交互式 TTY exec 输入。
type ExecInteractiveInput struct {
	ClusterID int64
	Namespace string
	Pod       string
	Container string
	Command   []string
	UserID    int64
	UserName  string
	// Recorder 可选 asciinema 录像写入器；nil 表示不录像。
	Recorder SessionRecorder
	// StdinWatcher 可选 stdin 命令观察器；nil 表示不审计命令行。
	StdinWatcher StdinWatcher
}

// SessionRecorder 录制 asciinema cast 流（实现方由 opsapp 注入）。
type SessionRecorder interface {
	// AppendHeader 写入 cast header（首行 JSON）。
	AppendHeader(width, height uint16) error
	// AppendEvent 写入一条 [time, "o"/"i", data] 事件。
	AppendEvent(evType string, data string) error
	// Close 落盘并返回对象 key。
	Close() (string, error)
}

// ExecInteractive 处理 WebSocket 交互式 Pod exec，支持 resize 与录像。
// 连接生命周期：浏览器升级 WS → 建立 SPDY exec → 双向转发 → 任一端断开即清理。
func (m *SessionManager) ExecInteractive(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	cfg *rest.Config,
	clientset kubernetes.Interface,
	in ExecInteractiveInput,
) error {
	if in.Namespace == "" || in.Pod == "" {
		return errors.New("namespace and pod are required")
	}
	if len(in.Command) == 0 {
		in.Command = []string{"/bin/sh"}
	}
	// K8s exec 协议（PodExecOptions）无法直接注入环境变量；但 shell 的行编辑/tab 补全
	// 依赖 TERM（bash 的 readline、busybox ash 的 line editing 均会在 TERM 为空时禁用）。
	// 通过 sh -c 包一层：在 exec 真正启动用户命令前导出 TERM=xterm-256color，使目标 shell
	// 以交互式 TTY + 正确 TERM 启动，tab 补全/方向键/历史回溯可用。详见 wrapInteractiveCommand。
	in.Command = wrapInteractiveCommand(in.Command)
	// http.Server.WriteTimeout（30s）会在 hijacked conn 上设置写截止时间，
	// 导致 WS 连接在 30s 后被强制关闭（"WebSocket is closed before the connection is established"）。
	// 握手升级后立即清零底层 TCP 的读写 deadline，恢复为长连接。
	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	clearConnDeadlines(ws.UnderlyingConn())

	pty := newPTYStream(ws)
	pty.stdinWatcher = in.StdinWatcher
	if in.Recorder != nil {
		pty.stdinRecorder = in.Recorder
	}
	// 初始尺寸由首个 resize 帧驱动；若浏览器未发送，使用 80x24 兜底。
	initialSize := remotecommand.TerminalSize{Width: 80, Height: 24}
	select {
	case pty.sizeQueue <- initialSize:
	case <-time.After(500 * time.Millisecond):
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(in.Pod).
		Namespace(in.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: in.Container,
			Command:   in.Command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, req.URL())
	if err != nil {
		pty.writeError("create spdy executor: " + err.Error())
		return err
	}

	// 录像：包装 pty 以旁路捕获输出帧。
	stdout := io.Writer(pty)
	if in.Recorder != nil {
		_ = in.Recorder.AppendHeader(initialSize.Width, initialSize.Height)
		stdout = &recordingWriter{inner: pty, recorder: in.Recorder}
	}

	// WS 会话不继承上层 request context 的 120s 超时（中间件 ctx timeout 会取消 exec 流）。
	// 改用独立的、仅在连接断开时取消的 context，避免长连接被中途打断。
	sessCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 若上层 request context 提前结束（如 client 断开），联动取消会话 context。
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-sessCtx.Done():
		}
	}()
	sessionID := generateSessionID()
	m.register(sessionID, "exec", in.ClusterID, in.Namespace, in.Pod, cancel)
	defer m.unregister(sessionID)

	streamErr := executor.StreamWithContext(sessCtx, remotecommand.StreamOptions{
		Stdin:             pty,
		Stdout:            stdout,
		Stderr:            pty,
		Tty:               true,
		TerminalSizeQueue: pty,
	})

	exitCode := 0
	if streamErr != nil {
		// remotecommand 在命令非零退出时返回包含 ExitStatus 的错误；尝试类型断言多种实现。
		if code, ok := exitStatusFrom(streamErr); ok {
			exitCode = code
		} else {
			pty.writeError(streamErr.Error())
		}
	}
	pty.writeExit(exitCode)

	if in.Recorder != nil {
		_, _ = in.Recorder.Close()
	}
	return streamErr
}

// recordingWriter 旁路复制 stdout 写入到 recorder，同时透传给浏览器。
type recordingWriter struct {
	inner    *ptyStream
	recorder SessionRecorder
}

func (rw *recordingWriter) Write(data []byte) (int, error) {
	if rw.recorder != nil {
		_ = rw.recorder.AppendEvent("o", string(data))
	}
	return rw.inner.Write(data)
}

func generateSessionID() string {
	return time.Now().Format("20060102-150405.000") + "-" + randSuffix()
}

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// exitStatusFrom 通过接口断言从 exec 错误中提取退出码。
// remotecommand 返回的错误通常实现 ExitStatus() int（如 exec.CodeExitError）。
func exitStatusFrom(err error) (int, bool) {
	type exitStatuser interface{ ExitStatus() int }
	var es exitStatuser
	if errors.As(err, &es) {
		return es.ExitStatus(), true
	}
	return 0, false
}

// wrapInteractiveCommand 包装用户命令，使其在交互式 TTY 下启用行编辑/tab 补全。
//
// 背景：K8s exec 协议（PodExecOptions）不直接支持注入环境变量；而 shell 的行编辑
// （bash 的 readline、busybox ash 的 line editing）依赖 TERM 环境变量。当 TERM 为空时，
// 这些 shell 会回退到无行编辑模式，Tab 被当作字面制表符、方向键输出 ANSI 转义序列。
// 用 sh -c 包一层：导出 TERM=xterm-256color 后 exec 进用户命令，目标 shell 以正确的
// TERM + 交互式 TTY 启动，tab 补全/方向键/历史回溯可用。
//
// 为不破坏上层传入的复合命令（如 ["sh","-c","script"]），仅在首元素是裸 shell
// （/bin/sh、/bin/bash、sh、bash）且参数中没有 -c/-i 时包装。
func wrapInteractiveCommand(cmd []string) []string {
	if len(cmd) == 0 {
		return []string{"/bin/sh", "-c", "export TERM=xterm-256color; exec /bin/sh"}
	}
	first := cmd[0]
	isBareShell := first == "/bin/sh" || first == "/bin/bash" || first == "sh" || first == "bash"
	if !isBareShell {
		return cmd
	}
	for _, a := range cmd[1:] {
		if a == "-c" || a == "-i" {
			return cmd
		}
	}
	script := "export TERM=xterm-256color; exec " + shellQuoteArgs(cmd)
	return []string{"/bin/sh", "-c", script}
}

// shellQuoteArgs 对参数列表做最小 shell 转义后拼成单行命令。
func shellQuoteArgs(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('\'')
		b.WriteString(strings.ReplaceAll(a, "'", `'\''`))
		b.WriteByte('\'')
	}
	return b.String()
}
