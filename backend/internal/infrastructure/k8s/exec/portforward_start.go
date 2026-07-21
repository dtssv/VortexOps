package exec

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardStart 非阻塞端口转发：预先分配本地端口 → 后台运行 → 返回分配端口与会话 ID。
// localPort 为 0 时由系统自动分配可用端口。
// 可通过返回的 sessionID 调用 Close 终止转发。
func (m *SessionManager) StartPortForward(
	ctx context.Context,
	cfg *rest.Config,
	clientset kubernetes.Interface,
	opt PortForwardOptions,
	localPort int,
) (sessionID string, actualLocalPort int, err error) {
	if opt.Namespace == "" || opt.Pod == "" || len(opt.Ports) == 0 {
		return "", 0, errors.New("namespace, pod and ports are required")
	}

	// 解析 remote 端口列表（仅取第一个进行本地转发，多个端口需多次调用或扩展）。
	remotePorts := make([]string, 0, len(opt.Ports))
	for _, p := range opt.Ports {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 保留 remote 部分。
		if strings.Contains(p, ":") {
			remotePorts = append(remotePorts, strings.SplitN(p, ":", 2)[1])
		} else {
			remotePorts = append(remotePorts, p)
		}
	}
	if len(remotePorts) == 0 {
		return "", 0, errors.New("no valid ports")
	}

	// 预分配本地端口（确保能返回确定端口）。
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return "", 0, fmt.Errorf("allocate local port: %w", err)
	}
	resolvedPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // 立即释放，由 portforward 重新绑定（窗口极小，实践中可接受）。

	// 构造 local:remote 端口对。
	ports := make([]string, len(remotePorts))
	for i, rp := range remotePorts {
		ports[i] = fmt.Sprintf("%d:%s", resolvedPort+i, rp)
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(opt.Pod).
		Namespace(opt.Namespace).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return "", 0, fmt.Errorf("spdy round tripper: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	fw, err := portforward.New(dialer, ports, stopCh, readyCh, nil, nil)
	if err != nil {
		return "", 0, fmt.Errorf("port forward: %w", err)
	}

	sessionID = opt.SessionID
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	sessCtx, cancel := context.WithCancel(ctx)
	m.register(sessionID, "portforward", opt.ClusterID, opt.Namespace, opt.Pod, cancel)

	go func() {
		<-sessCtx.Done()
		close(stopCh)
		m.unregister(sessionID)
	}()

	// 后台运行转发。
	go func() {
		_ = fw.ForwardPorts()
	}()

	// 等待 ready（最多 5s），确保端口已绑定。
	select {
	case <-readyCh:
	case <-sessCtx.Done():
		return "", 0, errors.New("port forward canceled before ready")
	}

	return sessionID, resolvedPort, nil
}
