package swarm_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/image"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestPullImages_CallsImagePull(t *testing.T) {
	t.Parallel()

	pulled := make(map[string]bool)
	var mu sync.Mutex

	mock := &swarmint.MockDockerClient{
		ImagePullFn: func(_ context.Context, ref string, _ image.PullOptions) (io.ReadCloser, error) {
			mu.Lock()
			pulled[ref] = true
			mu.Unlock()

			return io.NopCloser(strings.NewReader("{}\n")), nil
		},
	}

	services := map[string]*compose.Service{
		"web":    {Image: "nginx:latest"},
		"worker": {Image: "myapp:1.0"},
	}

	if err := swarmint.PullImages(context.Background(), mock, services); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !pulled["nginx:latest"] {
		t.Error("nginx:latest should have been pulled")
	}
	if !pulled["myapp:1.0"] {
		t.Error("myapp:1.0 should have been pulled")
	}
	if len(pulled) != 2 {
		t.Fatalf("expected exactly 2 pulled images, got %d", len(pulled))
	}
}

func TestPullImages_SkipsEmptyImage(t *testing.T) {
	t.Parallel()

	pullCalled := 0

	mock := &swarmint.MockDockerClient{
		ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
			pullCalled++
			return io.NopCloser(strings.NewReader("{}\n")), nil
		},
	}

	services := map[string]*compose.Service{
		"built": {Image: ""},
	}

	if err := swarmint.PullImages(context.Background(), mock, services); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pullCalled != 0 {
		t.Errorf("expected no ImagePull calls for services without image, got %d", pullCalled)
	}
}

func TestPullImages_ImagePullError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("registry unavailable")

	mock := &swarmint.MockDockerClient{
		ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
			return nil, wantErr
		},
	}

	services := map[string]*compose.Service{
		"web": {Image: "nginx:latest"},
	}

	err := swarmint.PullImages(context.Background(), mock, services)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped pull error, got %v", err)
	}
}

func TestPullImages_ProgressDecodeError(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("{")), nil
		},
	}

	services := map[string]*compose.Service{
		"web": {Image: "nginx:latest"},
	}

	err := swarmint.PullImages(context.Background(), mock, services)
	if err == nil {
		t.Fatal("expected decode error from pull progress stream")
	}
	if !strings.Contains(err.Error(), "read pull progress") {
		t.Fatalf("expected pull progress context in error, got %v", err)
	}
}

func TestPullImages_ProgressContainsErrorMessage(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("{\"error\":\"manifest unknown\"}\n")), nil
		},
	}

	services := map[string]*compose.Service{
		"web": {Image: "nginx:latest"},
	}

	err := swarmint.PullImages(context.Background(), mock, services)
	if err == nil {
		t.Fatal("expected pull error from progress stream")
	}
	if !strings.Contains(err.Error(), "manifest unknown") {
		t.Fatalf("expected registry error message, got %v", err)
	}
}
