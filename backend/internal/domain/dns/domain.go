// Package dns 定义域名→Pod IP 映射领域模型。
package dns

import (
	"time"
)

// RecordType DNS 记录类型。
type RecordType string

const (
	RecordA   RecordType = "A"
	RecordSRV RecordType = "SRV"
)

// RecordStatus 记录状态。
type RecordStatus string

const (
	StatusActive   RecordStatus = "active"
	StatusInactive RecordStatus = "inactive"
)

// Zone 域名区（如 vortexops.local）。
type Zone struct {
	ID          int64
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Record DNS 记录（分组维度域名映射）。
type Record struct {
	ID        int64
	GroupID   int64
	ClusterID int64
	Zone      string
	Name      string // 相对名（如 app-1.default）
	FQDN      string // 完整域名
	Type      RecordType
	TTL       int
	Status    RecordStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Backend Pod IP 后端（健康感知）。
type Backend struct {
	ID        int64
	RecordID  int64
	PodIP     string
	PodName   string
	Healthy   bool
	Weight    int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultZone 默认内部域名区。
const DefaultZone = "vortexops.local"

// DefaultTTL 默认 TTL（秒）。
const DefaultTTL = 30

// BuildFQDN 构造分组默认 FQDN：{deployment}.{namespace}.svc.vortexops.local
func BuildFQDN(deploymentName, namespace string) string {
	if namespace == "" {
		namespace = "default"
	}
	return deploymentName + "." + namespace + ".svc." + DefaultZone
}

// BuildRelativeName 构造相对记录名。
func BuildRelativeName(deploymentName, namespace string) string {
	if namespace == "" {
		namespace = "default"
	}
	return deploymentName + "." + namespace
}
