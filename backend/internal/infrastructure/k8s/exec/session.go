// Package exec 提供 Pod exec 与 port-forward 会话管理（SPDY via client-go remotecommand）。
package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
)

// SessionManager 管理 exec 与 port-forward 会话。
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// Session 活跃会话。
type Session struct {
	ID        string
	Type      string
	ClusterID int64
	Namespace string
	Pod       string
	StartedAt time.Time
	cancel    context.CancelFunc
}

// NewSessionManager 创建会话管理器。
func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

// ExecOptions Pod exec 参数。
type ExecOptions struct {
	SessionID string
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

// Exec 在 Pod 中执行命令（SPDY 流）。
func (m *SessionManager) Exec(ctx context.Context, cfg *rest.Config, clientset kubernetes.Interface, opt ExecOptions) error {
	if opt.Namespace == "" || opt.Pod == "" {
		return errors.New("namespace and pod are required")
	}
	if len(opt.Command) == 0 {
		opt.Command = []string{"/bin/sh"}
	}
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(opt.Pod).
		Namespace(opt.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: opt.Container,
			Command:   opt.Command,
			Stdin:     opt.Stdin != nil,
			Stdout:    opt.Stdout != nil,
			Stderr:    opt.Stderr != nil,
			TTY:       opt.TTY,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, req.URL())
	if err != nil {
		return fmt.Errorf("create spdy executor: %w", err)
	}
	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if opt.SessionID != "" {
		m.register(opt.SessionID, "exec", opt.ClusterID, opt.Namespace, opt.Pod, cancel)
		defer m.unregister(opt.SessionID)
	}
	return exec.StreamWithContext(sessCtx, remotecommand.StreamOptions{
		Stdin:  opt.Stdin,
		Stdout: opt.Stdout,
		Stderr: opt.Stderr,
		Tty:    opt.TTY,
	})
}

// PortForwardOptions 端口转发参数。
type PortForwardOptions struct {
	SessionID string
	ClusterID int64
	Namespace string
	Pod       string
	Ports     []string
	ReadyChan chan struct{}
	StopChan  <-chan struct{}
	Out       io.Writer
	ErrOut    io.Writer
}

// PortForward 建立 Pod 端口转发（SPDY）。
func (m *SessionManager) PortForward(ctx context.Context, cfg *rest.Config, clientset kubernetes.Interface, opt PortForwardOptions) error {
	if opt.Namespace == "" || opt.Pod == "" || len(opt.Ports) == 0 {
		return errors.New("namespace, pod and ports are required")
	}
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(opt.Pod).
		Namespace(opt.Namespace).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return fmt.Errorf("spdy round tripper: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	stopCh := make(chan struct{})
	if opt.StopChan != nil {
		go func() {
			select {
			case <-opt.StopChan:
				close(stopCh)
			case <-ctx.Done():
				close(stopCh)
			}
		}()
	} else {
		go func() {
			<-ctx.Done()
			close(stopCh)
		}()
	}
	ready := opt.ReadyChan
	if ready == nil {
		ready = make(chan struct{})
	}
	out := opt.Out
	if out == nil {
		out = io.Discard
	}
	errOut := opt.ErrOut
	if errOut == nil {
		errOut = io.Discard
	}
	fw, err := portforward.New(dialer, opt.Ports, stopCh, ready, out, errOut)
	if err != nil {
		return fmt.Errorf("port forward: %w", err)
	}
	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if opt.SessionID != "" {
		m.register(opt.SessionID, "portforward", opt.ClusterID, opt.Namespace, opt.Pod, cancel)
		defer m.unregister(opt.SessionID)
	}
	go func() { <-sessCtx.Done(); close(stopCh) }()
	return fw.ForwardPorts()
}

func (m *SessionManager) register(id, typ string, clusterID int64, ns, pod string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = &Session{
		ID: id, Type: typ, ClusterID: clusterID, Namespace: ns, Pod: pod,
		StartedAt: time.Now(), cancel: cancel,
	}
}

func (m *SessionManager) unregister(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// List 返回活跃会话快照。
func (m *SessionManager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, &Session{
			ID: s.ID, Type: s.Type, ClusterID: s.ClusterID,
			Namespace: s.Namespace, Pod: s.Pod, StartedAt: s.StartedAt,
		})
	}
	return out
}

// Close 关闭指定会话。
func (m *SessionManager) Close(id string) bool {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok && s.cancel != nil {
		s.cancel()
	}
	return ok
}
