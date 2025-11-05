package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

var (
	version  string = "dev"
	revision string = "000000000000000000000000000000"
)

const (
	waitInterval = 3 * time.Second
	waitTimeout  = 5 * time.Minute
)

func main() {
	showLogs := flag.Bool("logs", false, "Stream logs during healthcheck")
	logsMode := flag.String("logs-mode", "service", "Logs mode: container or service")
	registryAuthPath := flag.String("registry-auth-path", "", "Path to Docker config directory for registry authentication")
	flag.Parse()

	if flag.NArg() < 2 {
		log.Fatalf("usage: %s [--logs] [--logs-mode container|service] [--registry-auth-path DIR] <stack-name> <compose-file>", os.Args[0])
	}

	log.Printf("Start Docker Stack Wait version=%s revision=%s", version, revision)

	stack := flag.Arg(0)
	file := flag.Arg(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatalf("docker client init: %v", err)
	}

	prev := saveCurrentImages(stack)

	fmt.Printf("Deploying stack %q...\n", stack)

	args := []string{"stack", "deploy", "--detach=false", "-c", file}
	if *registryAuthPath != "" {
		args = append(args, "--with-registry-auth")
	}
	args = append(args, stack)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if *registryAuthPath != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_CONFIG=%s", *registryAuthPath))
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("deploy start: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go streamStackEvents(ctx, cli, stack, &wg)

	cmd.Wait()
	fmt.Println("Stack deployment finished. Monitoring containers...")

	if *showLogs {
		switch *logsMode {
		case "service":
			go streamServiceLogs(ctx, stack)
		default:
			go streamContainerLogs(ctx, cli, stack)
		}
	}

	success := waitForHealthy(ctx, cli, stack, waitTimeout)
	cancel()
	wg.Wait()

	if !success {
		fmt.Println("Some containers failed healthcheck. Rolling back...")
		rollback(prev)
		os.Exit(1)
	}
	fmt.Println("All containers are healthy.")
}

// ---- Events ----
func streamStackEvents(ctx context.Context, cli *client.Client, stack string, wg *sync.WaitGroup) {
	defer wg.Done()
	f := filters.NewArgs()
	f.Add("type", "service")
	f.Add("type", "container")
	f.Add("label", fmt.Sprintf("com.docker.stack.namespace=%s", stack))

	msgs, errs := cli.Events(ctx, types.EventsOptions{Filters: f})
	for {
		select {
		case e := <-msgs:
			switch e.Type {
			case events.ServiceEventType:
				fmt.Printf("[%s] service %s: %s\n",
					time.Unix(e.Time, 0).Format(time.RFC3339),
					e.Actor.Attributes["name"], e.Action)
			case events.ContainerEventType:
				if name := e.Actor.Attributes["name"]; name != "" {
					fmt.Printf("[%s] container %s: %s\n",
						time.Unix(e.Time, 0).Format(time.RFC3339), name, e.Action)
				}
			}
		case err := <-errs:
			if err != nil && ctx.Err() == nil {
				fmt.Printf("event stream error: %v\n", err)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

// ---- Logs ----
func streamServiceLogs(ctx context.Context, stack string) {
	cmd := exec.Command("docker", "service", "logs", "-f", "--raw", stack)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("service logs start: %v", err)
		return
	}
	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
	}()
	_ = cmd.Wait()
}

func streamContainerLogs(ctx context.Context, cli *client.Client, stack string) {
	known := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
			if err != nil {
				log.Printf("log streaming: list error: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}
			for _, c := range containers {
				if !hasLabel(c.Labels, "com.docker.stack.namespace", stack) {
					continue
				}
				if known[c.ID] {
					continue
				}
				known[c.ID] = true
				go followLogs(ctx, cli, c.ID, strings.TrimPrefix(c.Names[0], "/"))
			}
			time.Sleep(3 * time.Second)
		}
	}
}

func followLogs(ctx context.Context, cli *client.Client, id, name string) {
	reader, err := cli.ContainerLogs(ctx, id, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Tail:       "10",
	})
	if err != nil {
		log.Printf("logs(%s): %v", name, err)
		return
	}
	defer reader.Close()

	prefix := fmt.Sprintf("[logs:%s]", name)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fmt.Printf("%s %s\n", prefix, scanner.Text())
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Printf("log stream error (%s): %v", name, err)
	}
}

// ---- Health ----
func waitForHealthy(ctx context.Context, cli *client.Client, stack string, limit time.Duration) bool {
	start := time.Now()
	for {
		allHealthy := true
		containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
		if err != nil {
			log.Printf("list error: %v", err)
			return false
		}
		count := 0
		for _, c := range containers {
			if !hasLabel(c.Labels, "com.docker.stack.namespace", stack) {
				continue
			}
			count++
			info, err := cli.ContainerInspect(ctx, c.ID)
			if err != nil {
				allHealthy = false
				continue
			}
			state := info.State
			if state.Health == nil || state.Health.Status != "healthy" {
				allHealthy = false
			}
		}
		if allHealthy && count > 0 {
			return true
		}
		if time.Since(start) > limit {
			return false
		}
		time.Sleep(waitInterval)
	}
}

// ---- Rollback ----
func rollback(prev map[string]string) {
	for svc, img := range prev {
		fmt.Printf("Rolling back %s to %s\n", svc, img)

		digestOut, err := exec.Command("docker", "inspect", "--format", "{{index .RepoDigests 0}}", img).Output()
		if err == nil {
			if digest := strings.TrimSpace(string(digestOut)); digest != "" {
				fmt.Printf("Resolved digest: %s\n", digest)
				img = digest
			} else {
				fmt.Printf("Digest not found locally, using tag: %s\n", img)
			}
		} else {
			fmt.Printf("Image not found locally, using tag: %s\n", img)
		}

		cmd := exec.Command("docker", "service", "update", "--image", img, svc)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("rollback failed for %s: %v", svc, err)
		}
	}
}

// ---- Helpers ----
func saveCurrentImages(stack string) map[string]string {
	out, err := exec.Command("docker", "stack", "services", stack, "--format", "{{.Name}} {{.Image}}").Output()
	if err != nil {
		return map[string]string{}
	}

	prev := make(map[string]string)
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) == 2 {
			prev[parts[0]] = parts[1]
		}
	}
	return prev
}

func hasLabel(labels map[string]string, key, val string) bool {
	v, ok := labels[key]
	return ok && v == val
}
