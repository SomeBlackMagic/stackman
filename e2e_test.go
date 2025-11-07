package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"

	"stackman/compose"
	"stackman/deployer"
	"stackman/monitor"
)

// MockDockerClient реализует интерфейс Docker API для тестирования
type MockDockerClient struct {
	services   map[string]swarm.Service
	tasks      map[string][]swarm.Task
	containers map[string]types.ContainerJSON
	networks   map[string]network.Inspect
	volumes    map[string]volume.Volume
	logs       map[string]string
}

func NewMockDockerClient() *MockDockerClient {
	return &MockDockerClient{
		services:   make(map[string]swarm.Service),
		tasks:      make(map[string][]swarm.Task),
		containers: make(map[string]types.ContainerJSON),
		networks:   make(map[string]network.Inspect),
		volumes:    make(map[string]volume.Volume),
		logs:       make(map[string]string),
	}
}

// Реализация интерфейса client.CommonAPIClient

func (m *MockDockerClient) ClientVersion() string      { return "mock" }
func (m *MockDockerClient) DaemonHost() string         { return "mock" }
func (m *MockDockerClient) HTTPClient() *client.Client { return nil }
func (m *MockDockerClient) ServerVersion(ctx context.Context) (types.Version, error) {
	return types.Version{}, nil
}
func (m *MockDockerClient) NegotiateAPIVersion(ctx context.Context) {}
func (m *MockDockerClient) NegotiateAPIVersionPing(ping types.Ping) {}
func (m *MockDockerClient) DialHijack(ctx context.Context, url, proto string, meta map[string][]string) (io.ReadWriteCloser, error) {
	return nil, nil
}
func (m *MockDockerClient) Dialer() func(context.Context) (net.Conn, error) { return nil }

// ServiceList возвращает список сервисов
func (m *MockDockerClient) ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error) {
	var result []swarm.Service

	// Фильтруем по stack namespace
	stackFilter := ""
	if options.Filters.Len() > 0 {
		if labels := options.Filters.Get("label"); len(labels) > 0 {
			for _, label := range labels {
				if strings.HasPrefix(label, "com.docker.stack.namespace=") {
					stackFilter = strings.TrimPrefix(label, "com.docker.stack.namespace=")
					break
				}
			}
		}
		// Фильтр по имени
		if names := options.Filters.Get("name"); len(names) > 0 {
			for _, svc := range m.services {
				if svc.Spec.Name == names[0] {
					result = append(result, svc)
				}
			}
			return result, nil
		}
	}

	for _, svc := range m.services {
		if stackFilter != "" {
			if ns, ok := svc.Spec.Labels["com.docker.stack.namespace"]; ok && ns == stackFilter {
				result = append(result, svc)
			}
		} else {
			result = append(result, svc)
		}
	}

	return result, nil
}

// ServiceCreate создает новый сервис
func (m *MockDockerClient) ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
	svcID := fmt.Sprintf("svc_%s_%d", service.Name, time.Now().UnixNano())

	replicas := uint64(1)
	if service.Mode.Replicated != nil && service.Mode.Replicated.Replicas != nil {
		replicas = *service.Mode.Replicated.Replicas
	}

	m.services[svcID] = swarm.Service{
		ID:   svcID,
		Spec: service,
	}

	// Создаем таски для сервиса
	m.tasks[svcID] = make([]swarm.Task, 0)
	for i := uint64(0); i < replicas; i++ {
		taskID := fmt.Sprintf("task_%s_%d", svcID, i)
		containerID := fmt.Sprintf("container_%s_%d", svcID, i)

		task := swarm.Task{
			ID: taskID,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateRunning,
				ContainerStatus: &swarm.ContainerStatus{
					ContainerID: containerID,
				},
			},
			DesiredState: swarm.TaskStateRunning,
		}

		m.tasks[svcID] = append(m.tasks[svcID], task)

		// Создаем контейнер
		healthStatus := types.Healthy
		m.containers[containerID] = types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:   containerID,
				Name: fmt.Sprintf("/%s.%d.%s", service.Name, i+1, taskID[:12]),
				State: &types.ContainerState{
					Status: "running",
					Health: &types.Health{
						Status: healthStatus,
					},
				},
			},
		}
	}

	return swarm.ServiceCreateResponse{ID: svcID}, nil
}

// ServiceUpdate обновляет существующий сервис
func (m *MockDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	if _, exists := m.services[serviceID]; !exists {
		return swarm.ServiceUpdateResponse{}, fmt.Errorf("service not found: %s", serviceID)
	}

	m.services[serviceID] = swarm.Service{
		ID:   serviceID,
		Spec: service,
	}

	return swarm.ServiceUpdateResponse{}, nil
}

// ServiceRemove удаляет сервис
func (m *MockDockerClient) ServiceRemove(ctx context.Context, serviceID string) error {
	delete(m.services, serviceID)
	delete(m.tasks, serviceID)
	return nil
}

// ServiceInspectWithRaw возвращает детали сервиса
func (m *MockDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	if svc, ok := m.services[serviceID]; ok {
		return svc, nil, nil
	}
	return swarm.Service{}, nil, fmt.Errorf("service not found")
}

// TaskList возвращает список задач
func (m *MockDockerClient) TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error) {
	var result []swarm.Task

	// Фильтруем по service
	serviceFilter := ""
	if options.Filters.Len() > 0 {
		if services := options.Filters.Get("service"); len(services) > 0 {
			serviceFilter = services[0]
		}
	}

	if serviceFilter != "" {
		if tasks, ok := m.tasks[serviceFilter]; ok {
			result = append(result, tasks...)
		}
	} else {
		for _, tasks := range m.tasks {
			result = append(result, tasks...)
		}
	}

	return result, nil
}

// ContainerInspect возвращает информацию о контейнере
func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if container, ok := m.containers[containerID]; ok {
		return container, nil
	}
	return types.ContainerJSON{}, fmt.Errorf("container not found: %s", containerID)
}

// ContainerList возвращает список контейнеров
func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	var result []types.Container

	for _, c := range m.containers {
		result = append(result, types.Container{
			ID:    c.ID,
			Names: []string{c.Name},
			State: c.State.Status,
		})
	}

	return result, nil
}

// ContainerRemove удаляет контейнер
func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	delete(m.containers, containerID)
	return nil
}

// ContainerLogs возвращает логи контейнера
func (m *MockDockerClient) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	logContent := m.logs[containerID]
	if logContent == "" {
		logContent = fmt.Sprintf("Mock log for container %s\n", containerID)
	}
	return io.NopCloser(strings.NewReader(logContent)), nil
}

// NetworkCreate создает сеть
func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	netID := fmt.Sprintf("net_%s_%d", name, time.Now().UnixNano())
	m.networks[netID] = network.Inspect{
		ID:   netID,
		Name: name,
	}
	return network.CreateResponse{ID: netID}, nil
}

// NetworkList возвращает список сетей
func (m *MockDockerClient) NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error) {
	var result []network.Summary
	for _, net := range m.networks {
		result = append(result, network.Summary{
			ID:   net.ID,
			Name: net.Name,
		})
	}
	return result, nil
}

// NetworkInspect возвращает информацию о сети
func (m *MockDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	if net, ok := m.networks[networkID]; ok {
		return net, nil
	}
	return network.Inspect{}, fmt.Errorf("network not found: %s", networkID)
}

// VolumeCreate создает том
func (m *MockDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	volName := options.Name
	if volName == "" {
		volName = fmt.Sprintf("vol_%d", time.Now().UnixNano())
	}

	vol := volume.Volume{
		Name:   volName,
		Driver: options.Driver,
	}
	m.volumes[volName] = vol
	return vol, nil
}

// VolumeList возвращает список томов
func (m *MockDockerClient) VolumeList(ctx context.Context, options volume.ListOptions) (volume.ListResponse, error) {
	var volumes []*volume.Volume
	for _, vol := range m.volumes {
		v := vol
		volumes = append(volumes, &v)
	}
	return volume.ListResponse{Volumes: volumes}, nil
}

// VolumeInspect возвращает информацию о томе
func (m *MockDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	if vol, ok := m.volumes[volumeID]; ok {
		return vol, nil
	}
	return volume.Volume{}, fmt.Errorf("volume not found: %s", volumeID)
}

// NetworkRemove удаляет сеть
func (m *MockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	delete(m.networks, networkID)
	return nil
}

// ImagePull симулирует загрузку образа
func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	response := fmt.Sprintf(`{"status":"Pulling from library/%s","id":"latest"}
{"status":"Download complete","id":"latest"}`, refStr)
	return io.NopCloser(strings.NewReader(response)), nil
}

// Events возвращает поток событий (заглушка)
func (m *MockDockerClient) Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error) {
	eventsChan := make(chan events.Message)
	errChan := make(chan error)

	go func() {
		<-ctx.Done()
		close(eventsChan)
		close(errChan)
	}()

	return eventsChan, errChan
}

// Ping проверяет соединение с Docker
func (m *MockDockerClient) Ping(ctx context.Context) (types.Ping, error) {
	return types.Ping{}, nil
}

// Close закрывает клиент
func (m *MockDockerClient) Close() error {
	return nil
}

// SetContainerHealth устанавливает статус здоровья контейнера (для тестов)
func (m *MockDockerClient) SetContainerHealth(containerID string, status string) {
	if c, ok := m.containers[containerID]; ok {
		if c.State.Health != nil {
			c.State.Health.Status = status
			m.containers[containerID] = c
		}
	}
}

// SetTaskState устанавливает состояние задачи (для тестов)
func (m *MockDockerClient) SetTaskState(serviceID string, taskIndex int, state swarm.TaskState) {
	if tasks, ok := m.tasks[serviceID]; ok && taskIndex < len(tasks) {
		tasks[taskIndex].Status.State = state
		m.tasks[serviceID] = tasks
	}
}

// AddServiceFailedTask добавляет неудачную задачу к сервису (для тестов)
func (m *MockDockerClient) AddServiceFailedTask(serviceID string, errMsg string) {
	if tasks, ok := m.tasks[serviceID]; ok {
		taskID := fmt.Sprintf("failed_task_%d", len(tasks))
		failedTask := swarm.Task{
			ID: taskID,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateFailed,
				Err:   errMsg,
				ContainerStatus: &swarm.ContainerStatus{
					ExitCode: 1,
				},
			},
			DesiredState: swarm.TaskStateShutdown,
		}
		m.tasks[serviceID] = append(tasks, failedTask)
	}
}

// Test_E2E_HealthyDeployment тестирует успешное развертывание стека
func Test_E2E_HealthyDeployment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mockClient := NewMockDockerClient()
	stackName := "test-stack"

	// Создаем compose spec
	composeSpec := &compose.ComposeFile{
		Services: map[string]*compose.Service{
			"web": {
				Image: "nginx:latest",
				Deploy: &compose.DeployConfig{
					Replicas: ptr(2),
				},
			},
		},
		Networks: map[string]*compose.Network{
			"default": {
				Driver: "overlay",
			},
		},
		Volumes: map[string]*compose.Volume{
			"data": {
				Driver: "local",
			},
		},
	}

	// Деплоим стек
	stackDeployer := deployer.NewStackDeployer(mockClient, stackName, 3)
	err := stackDeployer.Deploy(ctx, composeSpec)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	// Проверяем здоровье
	healthMonitor := monitor.NewHealthMonitor(mockClient, stackName, 3)

	// Ждем готовности сервисов
	err = healthMonitor.WaitServicesReady(ctx)
	if err != nil {
		t.Fatalf("WaitServicesReady failed: %v", err)
	}

	// Проверяем здоровье контейнеров
	healthy := healthMonitor.WaitHealthy(ctx)
	if !healthy {
		t.Fatal("WaitHealthy returned false, expected true")
	}

	// Проверяем что сервисы созданы
	services, err := mockClient.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", stackName)),
		),
	})
	if err != nil {
		t.Fatalf("Failed to list services: %v", err)
	}

	if len(services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(services))
	}

	t.Logf("✓ Successfully deployed stack with %d service(s)", len(services))
	t.Log("✓ All containers are healthy")
}

// Test_E2E_FailedDeployment тестирует развертывание с ошибками
func Test_E2E_FailedDeployment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mockClient := NewMockDockerClient()
	stackName := "test-stack-failed"

	composeSpec := &compose.ComposeFile{
		Services: map[string]*compose.Service{
			"failing-service": {
				Image: "broken:latest",
				Deploy: &compose.DeployConfig{
					Replicas: ptr(1),
				},
			},
		},
	}

	// Деплоим стек
	stackDeployer := deployer.NewStackDeployer(mockClient, stackName, 2)
	err := stackDeployer.Deploy(ctx, composeSpec)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	// Получаем ID сервиса
	services, _ := mockClient.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", stackName)),
		),
	})

	if len(services) > 0 {
		serviceID := services[0].ID

		// Добавляем несколько неудачных задач
		mockClient.AddServiceFailedTask(serviceID, "container exited with code 1")
		mockClient.AddServiceFailedTask(serviceID, "container exited with code 1")
		mockClient.AddServiceFailedTask(serviceID, "container exited with code 1")
	}

	// Проверяем здоровье - должно упасть из-за количества failed tasks
	healthMonitor := monitor.NewHealthMonitor(mockClient, stackName, 2)
	healthy := healthMonitor.WaitHealthy(ctx)

	if healthy {
		t.Fatal("WaitHealthy returned true, expected false for failed deployment")
	}

	t.Log("✓ Correctly detected failed deployment")
}

// Test_E2E_LogOutput тестирует вывод логов
func Test_E2E_LogOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mockClient := NewMockDockerClient()
	stackName := "test-stack-logs"

	composeSpec := &compose.ComposeFile{
		Services: map[string]*compose.Service{
			"logger": {
				Image: "alpine:latest",
				Deploy: &compose.DeployConfig{
					Replicas: ptr(1),
				},
			},
		},
	}

	// Деплоим стек
	stackDeployer := deployer.NewStackDeployer(mockClient, stackName, 3)
	err := stackDeployer.Deploy(ctx, composeSpec)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	// Получаем контейнеры
	containers, err := mockClient.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list containers: %v", err)
	}

	if len(containers) == 0 {
		t.Fatal("No containers found")
	}

	// Устанавливаем логи для контейнера
	containerID := containers[0].ID
	mockClient.logs[containerID] = "Test log line 1\nTest log line 2\nApplication started successfully\n"

	// Читаем логи
	logReader, err := mockClient.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		t.Fatalf("Failed to get container logs: %v", err)
	}
	defer logReader.Close()

	logContent, err := io.ReadAll(logReader)
	if err != nil {
		t.Fatalf("Failed to read logs: %v", err)
	}

	logStr := string(logContent)
	if !strings.Contains(logStr, "Application started successfully") {
		t.Fatalf("Expected log content not found. Got: %s", logStr)
	}

	t.Log("✓ Successfully retrieved container logs")
	t.Logf("✓ Log output:\n%s", logStr)
}

// ptr создает указатель на значение (helper)
func ptr[T any](v T) *T {
	return &v
}
