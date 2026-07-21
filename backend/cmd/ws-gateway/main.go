// Command ws-gateway 是 VortexOps WebSocket 网关：实时事件推送 + Redis Pub/Sub 跨实例路由。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	goredis "github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/vortexops/vortexops/internal/config"
	"github.com/vortexops/vortexops/internal/platform/logger"
	"github.com/vortexops/vortexops/internal/platform/redis"
	"github.com/vortexops/vortexops/internal/version"
)

const (
	wsBroadcastChannel = "ws:broadcast"
	wsRouteKeyPrefix   = "ws:route:"
)

func main() {
	root := &cobra.Command{Use: "ws-gateway", Short: "VortexOps WebSocket gateway"}
	root.AddCommand(serveCmd(), versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use: "version", Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) { fmt.Println(version.String()) },
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use: "serve", Short: "Start WebSocket gateway",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load("VORTEXOPS", "")
			if err != nil {
				return err
			}
			log := logger.New(cfg.Log.Level, cfg.Log.Format)
			instanceID := cfg.App.InstanceID
			if instanceID == "" {
				instanceID, _ = os.Hostname()
			}
			log.Info("starting ws-gateway", "instance", instanceID, "version", version.Version)

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			rc, err := redis.New(ctx, cfg.Redis)
			if err != nil {
				return fmt.Errorf("init redis: %w", err)
			}
			defer rc.Close()

			hub := newHub(rc.Universal, instanceID, log)
			go hub.runRedisSubscriber(ctx)

			addr := os.Getenv("VORTEXOPS_WS_ADDR")
			if addr == "" {
				addr = ":8081"
			}
			r := chi.NewRouter()
			r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
			r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			})
			r.Get("/ws", hub.handleWS)

			srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("ws server error", "err", err)
				}
			}()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
			cancel()
			shutdownCtx, sdCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
			defer sdCancel()
			return srv.Shutdown(shutdownCtx)
		},
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

type wsMessage struct {
	Type    string `json:"type"`
	Topic   string `json:"topic"`
	Payload any    `json:"payload"`
}

type broadcastEnvelope struct {
	TargetTopic string    `json:"target_topic"`
	Message     wsMessage `json:"message"`
	Origin      string    `json:"origin"`
}

type client struct {
	id     string
	topics map[string]bool
	conn   *websocket.Conn
	send   chan []byte
}

type hub struct {
	redis      goredis.UniversalClient
	instanceID string
	log        *logger.Logger
	mu         sync.RWMutex
	clients    map[string]*client
}

func newHub(r goredis.UniversalClient, instanceID string, log *logger.Logger) *hub {
	return &hub{redis: r, instanceID: instanceID, log: log, clients: make(map[string]*client)}
}

func (h *hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	clientID := r.Header.Get("X-Request-ID")
	if clientID == "" {
		clientID = fmt.Sprintf("%s-%d", h.instanceID, time.Now().UnixNano())
	}
	c := &client{id: clientID, topics: map[string]bool{}, conn: conn, send: make(chan []byte, 64)}
	h.register(c)
	defer h.unregister(c)
	go h.writePump(c)
	h.readPump(c)
}

func (h *hub) register(c *client) {
	h.mu.Lock()
	h.clients[c.id] = c
	h.mu.Unlock()
	ctx := context.Background()
	_ = h.redis.Set(ctx, wsRouteKeyPrefix+c.id, h.instanceID, 24*time.Hour).Err()
}

func (h *hub) unregister(c *client) {
	h.mu.Lock()
	delete(h.clients, c.id)
	h.mu.Unlock()
	ctx := context.Background()
	_ = h.redis.Del(ctx, wsRouteKeyPrefix+c.id).Err()
	close(c.send)
	_ = c.conn.Close()
}

func (h *hub) readPump(c *client) {
	defer h.unregister(c)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "subscribe":
			if msg.Topic != "" {
				c.topics[msg.Topic] = true
			}
		case "unsubscribe":
			delete(c.topics, msg.Topic)
		case "publish":
			h.publishCrossInstance(context.Background(), msg.Topic, msg)
		}
	}
}

func (h *hub) writePump(c *client) {
	for data := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}

func (h *hub) publishCrossInstance(ctx context.Context, topic string, msg wsMessage) {
	env := broadcastEnvelope{
		TargetTopic: topic,
		Message:     msg,
		Origin:      h.instanceID,
	}
	data, _ := json.Marshal(env)
	if err := h.redis.Publish(ctx, wsBroadcastChannel, data).Err(); err != nil {
		h.log.Error("redis publish failed", "err", err)
	}
	h.deliverLocal(topic, msg)
}

func (h *hub) runRedisSubscriber(ctx context.Context) {
	sub := h.redis.Subscribe(ctx, wsBroadcastChannel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			var env broadcastEnvelope
			if err := json.Unmarshal([]byte(m.Payload), &env); err != nil {
				continue
			}
			if env.Origin == h.instanceID {
				continue
			}
			h.deliverLocal(env.TargetTopic, env.Message)
		}
	}
}

func (h *hub) deliverLocal(topic string, msg wsMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if topic == "" || c.topics[topic] || c.topics["*"] {
			select {
			case c.send <- data:
			default:
			}
		}
	}
}
