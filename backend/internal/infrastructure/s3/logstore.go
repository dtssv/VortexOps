// Package s3 提供 MinIO/S3 兼容的对象存储客户端，用于构建日志归档。
// 使用 minio-go v7 SDK（S3 兼容）。
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/vortexops/vortexops/internal/domain/build"
)

// Config S3/MinIO 配置。
type Config struct {
	Endpoint        string
	AccessKey       string
	SecretKey       string
	Bucket          string
	Region          string
	UseSSL          bool
}

// LogStore 实现 build.LogStore，对象键为 {bucket}/builds/{buildID}/{step}.log。
type LogStore struct {
	client *minio.Client
	bucket string
}

// NewLogStore 创建 S3 日志存储。若 bucket 不存在则自动创建。
func NewLogStore(ctx context.Context, cfg Config) (*LogStore, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("s3 endpoint and bucket are required")
	}
	cli, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("init minio client: %w", err)
	}
	// 确保 bucket 存在。
	exists, err := cli.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}
	return &LogStore{client: cli, bucket: cfg.Bucket}, nil
}

// Upload 上传日志字节。
func (s *LogStore) Upload(ctx context.Context, key string, data []byte) error {
	if key == "" {
		return errors.New("key is required")
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "text/plain; charset=utf-8"})
	if err != nil {
		return fmt.Errorf("upload log: %w", err)
	}
	return nil
}

// Download 下载完整日志。
func (s *LogStore) Download(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get log object: %w", err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}
	return data, nil
}

// StreamDownload 返回对象的读取流（调用方负责 Close），用于流式代理大文件（如会话录像 cast）。
func (s *LogStore) StreamDownload(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get log object: %w", err)
	}
	return obj, nil
}

// DownloadRange 下载日志字节范围 [start, end)。
func (s *LogStore) DownloadRange(ctx context.Context, key string, start, end int64) ([]byte, error) {
	if start < 0 {
		start = 0
	}
	opts := minio.GetObjectOptions{}
	if end > 0 {
		if err := opts.SetRange(start, end-1); err != nil {
			return nil, fmt.Errorf("set range: %w", err)
		}
	} else if start > 0 {
		if err := opts.SetRange(start, -1); err != nil {
			return nil, fmt.Errorf("set range: %w", err)
		}
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		return nil, fmt.Errorf("get log range: %w", err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read log range: %w", err)
	}
	return data, nil
}

// Compile-time assertion: LogStore 实现 build.LogStore。
var _ build.LogStore = (*LogStore)(nil)

// PresignedGet 生成预签名下载 URL（供前端直接下载归档日志）。
func (s *LogStore) PresignedGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign: %w", err)
	}
	return u.String(), nil
}
