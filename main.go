package main

import (
	"bufio"
	"context"
	"fmt"
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
	if len(os.Args) < 3 {
		log.Fatalf("usage: %s <stack-name> <compose-file>", os.Args[0])
	}
	log.Printf("Start Docker Stack Wait version=%s revision=%s", version, revision)

	stack := os.Args[1]
	file := os.Args[2]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatalf("docker client init: %v", err)
	}

	prev := saveCurrentImages(stack)

	fmt.Printf("Deploying stack %q...\n", stack)
	cmd := exec.Command("docker", "stack", "deploy", "-c", file, stack)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatalf("deploy start: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go streamStackEvents(ctx, cli, stack, &wg)

	cmd.Wait()
	fmt.Println("Stack deployment command finished. Monitoring containers...")

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

func rollback(prev map[string]string) {
	for svc, img := range prev {
		fmt.Printf("Rolling back %s to %s\n", svc, img)
		cmd := exec.Command("docker", "service", "update", "--image", img, svc)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
}

func hasLabel(labels map[string]string, key, val string) bool {
	v, ok := labels[key]
	return ok && v == val
}
