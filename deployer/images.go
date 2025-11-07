package deployer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"golang.org/x/net/context"

	"stackman/compose"
)

func (d *StackDeployer) pullImages(ctx context.Context, services map[string]*compose.Service) error {
	for name, svc := range services {
		if svc.Image == "" {
			log.Printf("Service %s has no image specified, skipping pull", name)
			continue
		}

		log.Printf("Pulling image for service %s: %s", name, svc.Image)

		// Pull image with credentials from Docker config (~/.docker/config.json)
		pullOpts := image.PullOptions{
			RegistryAuth: getRegistryAuth(svc.Image),
		}

		out, err := d.cli.ImagePull(ctx, svc.Image, pullOpts)
		if err != nil {
			return fmt.Errorf("failed to pull image %s: %w", svc.Image, err)
		}
		defer out.Close()

		// Parse and log pull progress
		if err := d.logPullProgress(out); err != nil {
			return fmt.Errorf("failed to read pull progress for %s: %w", svc.Image, err)
		}

		log.Printf("Successfully pulled image: %s", svc.Image)
	}

	return nil
}

func getRegistryAuth(imageName string) string {
	// Extract registry from image name
	registryURL := extractRegistry(imageName)
	if registryURL == "" {
		return ""
	}

	// Read Docker config
	// Check for DOCKER_CONFIG_PATH env variable first, then fall back to default
	configPath := filepath.Join(os.Getenv("HOME"), ".docker", "config.json")
	if dockerConfigPath := os.Getenv("DOCKER_CONFIG_PATH"); dockerConfigPath != "" {
		configPath = filepath.Join(dockerConfigPath, "config.json")
		log.Printf("Using Docker config from DOCKER_CONFIG_PATH: %s", configPath)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Warning: could not read Docker config from %s: %v", configPath, err)
		return ""
	}

	var config struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("Warning: could not parse Docker config: %v", err)
		return ""
	}

	// Try to find auth for this registry
	if auth, ok := config.Auths[registryURL]; ok && auth.Auth != "" {
		// Auth is already base64 encoded username:password
		// We need to decode it, then encode as registry.AuthConfig
		authBytes, err := base64.StdEncoding.DecodeString(auth.Auth)
		if err != nil {
			log.Printf("Warning: could not decode auth: %v", err)
			return ""
		}

		parts := strings.SplitN(string(authBytes), ":", 2)
		if len(parts) != 2 {
			log.Printf("Warning: invalid auth format")
			return ""
		}

		authConfig := registry.AuthConfig{
			Username:      parts[0],
			Password:      parts[1],
			ServerAddress: registryURL,
		}

		encodedJSON, err := json.Marshal(authConfig)
		if err != nil {
			log.Printf("Warning: could not encode auth config: %v", err)
			return ""
		}

		return base64.URLEncoding.EncodeToString(encodedJSON)
	}

	return ""
}

func extractRegistry(imageName string) string {
	// Examples:
	// gitlab.opscore.org:5001/core/devops/domain-router:v0.1.0-alfa1 -> gitlab.opscore.org:5001
	// docker.io/library/nginx:latest -> docker.io
	// nginx:latest -> "" (Docker Hub, default)

	parts := strings.Split(imageName, "/")
	if len(parts) < 2 {
		return "" // No registry specified, use default
	}

	firstPart := parts[0]
	// Check if first part looks like a registry (contains . or :)
	if strings.Contains(firstPart, ".") || strings.Contains(firstPart, ":") {
		return firstPart
	}

	return ""
}

func (d *StackDeployer) logPullProgress(reader io.ReadCloser) error {
	decoder := json.NewDecoder(reader)

	var lastStatus string
	for {
		var progress struct {
			Status         string `json:"status"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
			Progress string `json:"progress"`
			ID       string `json:"id"`
		}

		if err := decoder.Decode(&progress); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// Log only status changes to reduce noise
		currentStatus := fmt.Sprintf("%s: %s", progress.ID, progress.Status)
		if progress.Status != "" && currentStatus != lastStatus {
			if progress.Progress != "" {
				log.Printf("  %s %s %s", progress.ID, progress.Status, progress.Progress)
			} else {
				log.Printf("  %s %s", progress.ID, progress.Status)
			}
			lastStatus = currentStatus
		}
	}

	return nil
}
