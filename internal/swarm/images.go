package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/docker/docker/api/types/image"

	"github.com/SomeBlackMagic/stackman/internal/compose"
)

const maxParallelImagePulls = 4

// PullImages downloads images for all services in parallel.
// Services without image (build-only) are skipped.
func PullImages(ctx context.Context, client DockerClient, services map[string]*compose.Service) error {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	var wg sync.WaitGroup
	state := &pullImagesState{}
	sem := make(chan struct{}, maxParallelImagePulls)

	for _, name := range names {
		svc := services[name]
		if svc == nil {
			return fmt.Errorf("pull image for service %q: nil service config", name)
		}
		if svc.Image == "" {
			continue
		}

		wg.Add(1)
		go pullImageWorker(ctx, &wg, sem, state, client, name, svc.Image)
	}

	wg.Wait()
	return state.first()
}

func pullImage(ctx context.Context, client DockerClient, ref string) error {
	rc, err := client.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()

	dec := json.NewDecoder(rc)
	for {
		var message struct {
			Error string `json:"error"`
		}

		if err := dec.Decode(&message); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read pull progress: %w", err)
		}

		if message.Error != "" {
			return fmt.Errorf("pull error: %s", message.Error)
		}
	}
}

type pullImagesState struct {
	mu       sync.Mutex
	firstErr error
}

func (s *pullImagesState) setFirst(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstErr == nil {
		s.firstErr = err
	}
}

func (s *pullImagesState) first() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstErr
}

func pullImageWorker(
	ctx context.Context,
	wg *sync.WaitGroup,
	sem chan struct{},
	state *pullImagesState,
	client DockerClient,
	serviceName string,
	ref string,
) {
	defer wg.Done()

	if err := acquirePullSlot(ctx, sem); err != nil {
		state.setFirst(fmt.Errorf("pull image for service %q (%s): %w", serviceName, ref, err))
		return
	}
	defer releasePullSlot(sem)

	if err := pullImage(ctx, client, ref); err != nil {
		state.setFirst(fmt.Errorf("pull image for service %q (%s): %w", serviceName, ref, err))
	}
}

func acquirePullSlot(ctx context.Context, sem chan struct{}) error {
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releasePullSlot(sem chan struct{}) {
	<-sem
}
