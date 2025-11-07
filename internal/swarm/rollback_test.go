package swarm

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func TestCreateSnapshot(t *testing.T) {
	tests := []struct {
		name                 string
		existingServices     []swarm.Service
		expectedFirstDeploy  bool
		expectedServiceCount int
	}{
		{
			name:                 "First deployment - no services",
			existingServices:     []swarm.Service{},
			expectedFirstDeploy:  true,
			expectedServiceCount: 0,
		},
		{
			name: "Existing deployment - has services",
			existingServices: []swarm.Service{
				{
					ID: "service1",
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{
							Name: "test_web",
							Labels: map[string]string{
								"com.docker.stack.namespace": "test",
							},
						},
					},
					PreviousSpec: &swarm.ServiceSpec{
						Annotations: swarm.Annotations{
							Name: "test_web_old",
						},
					},
				},
			},
			expectedFirstDeploy:  false,
			expectedServiceCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCli := &MockDockerClient{
				services: tt.existingServices,
			}

			deployer := NewStackDeployer(mockCli, "test", 3)
			ctx := context.Background()

			snapshot, err := deployer.CreateSnapshot(ctx)
			if err != nil {
				t.Fatalf("CreateSnapshot failed: %v", err)
			}

			if snapshot.IsFirstDeploy != tt.expectedFirstDeploy {
				t.Errorf("IsFirstDeploy = %v, want %v", snapshot.IsFirstDeploy, tt.expectedFirstDeploy)
			}

			if len(snapshot.Services) != tt.expectedServiceCount {
				t.Errorf("Services count = %d, want %d", len(snapshot.Services), tt.expectedServiceCount)
			}

			if snapshot.StackName != "test" {
				t.Errorf("StackName = %s, want test", snapshot.StackName)
			}

			if snapshot.CreatedAt.IsZero() {
				t.Error("CreatedAt should not be zero")
			}
		})
	}
}

func TestRollback_FirstDeploy(t *testing.T) {
	// Setup: service created during first deploy
	mockCli := &MockDockerClient{
		services: []swarm.Service{
			{
				ID: "new_service1",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name:   "test_web",
						Labels: map[string]string{"com.docker.stack.namespace": "test"},
					},
				},
			},
		},
	}

	deployer := NewStackDeployer(mockCli, "test", 3)
	ctx := context.Background()

	snapshot := &StackSnapshot{
		StackName:     "test",
		CreatedAt:     time.Now(),
		Services:      map[string]ServiceSnapshot{},
		ExistingIDs:   map[string]bool{},
		IsFirstDeploy: true,
	}

	err := deployer.Rollback(ctx, snapshot)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Should have removed the new service
	if len(mockCli.removedServices) != 1 {
		t.Errorf("Expected 1 service removed, got %d", len(mockCli.removedServices))
	}

	if mockCli.removedServices[0] != "new_service1" {
		t.Errorf("Expected service new_service1 removed, got %s", mockCli.removedServices[0])
	}
}

func TestRollback_UpdateExistingServices(t *testing.T) {
	// Setup: existing service with previous spec
	oldSpec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   "test_web",
			Labels: map[string]string{"version": "1", "com.docker.stack.namespace": "test"},
		},
	}

	oldService := swarm.Service{
		ID: "service1",
		Meta: swarm.Meta{
			Version: swarm.Version{
				Index: 9,
			},
		},
		Spec: oldSpec,
	}

	currentService := swarm.Service{
		ID: "service1",
		Meta: swarm.Meta{
			Version: swarm.Version{
				Index: 10,
			},
		},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "test_web",
				Labels: map[string]string{"version": "2", "com.docker.stack.namespace": "test"},
			},
		},
		PreviousSpec: &oldSpec,
	}

	mockCli := &MockDockerClient{
		services: []swarm.Service{currentService},
	}

	deployer := NewStackDeployer(mockCli, "test", 3)
	ctx := context.Background()

	snapshot := &StackSnapshot{
		StackName: "test",
		CreatedAt: time.Now(),
		Services: map[string]ServiceSnapshot{
			"service1": {
				Service: oldService,
				Tasks:   []swarm.Task{},
			},
		},
		ExistingIDs:   map[string]bool{"service1": true},
		IsFirstDeploy: false,
	}

	err := deployer.Rollback(ctx, snapshot)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Should have updated the service
	if len(mockCli.updatedServices) != 1 {
		t.Errorf("Expected 1 service updated, got %d", len(mockCli.updatedServices))
	}

	if mockCli.updatedServices[0] != "service1" {
		t.Errorf("Expected service1 updated, got %s", mockCli.updatedServices[0])
	}
}

func TestRollback_RemoveNewAndRestoreOld(t *testing.T) {
	// Setup: one existing service + one new service
	oldSpec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   "test_web",
			Labels: map[string]string{"version": "1", "com.docker.stack.namespace": "test"},
		},
	}

	oldService := swarm.Service{
		ID: "existing_service",
		Meta: swarm.Meta{
			Version: swarm.Version{Index: 9},
		},
		Spec: oldSpec,
	}

	mockCli := &MockDockerClient{
		services: []swarm.Service{
			{
				ID: "existing_service",
				Meta: swarm.Meta{
					Version: swarm.Version{Index: 10},
				},
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name:   "test_web",
						Labels: map[string]string{"version": "2", "com.docker.stack.namespace": "test"},
					},
				},
				PreviousSpec: &oldSpec,
			},
			{
				ID: "new_service",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name:   "test_api",
						Labels: map[string]string{"com.docker.stack.namespace": "test"},
					},
				},
			},
		},
	}

	deployer := NewStackDeployer(mockCli, "test", 3)
	ctx := context.Background()

	snapshot := &StackSnapshot{
		StackName: "test",
		CreatedAt: time.Now(),
		Services: map[string]ServiceSnapshot{
			"existing_service": {
				Service: oldService,
				Tasks:   []swarm.Task{},
			},
		},
		ExistingIDs:   map[string]bool{"existing_service": true},
		IsFirstDeploy: false,
	}

	err := deployer.Rollback(ctx, snapshot)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Should have removed new service
	if len(mockCli.removedServices) != 1 {
		t.Errorf("Expected 1 service removed, got %d", len(mockCli.removedServices))
	}

	if mockCli.removedServices[0] != "new_service" {
		t.Errorf("Expected new_service removed, got %s", mockCli.removedServices[0])
	}

	// Should have updated existing service
	if len(mockCli.updatedServices) != 1 {
		t.Errorf("Expected 1 service updated, got %d", len(mockCli.updatedServices))
	}

	if mockCli.updatedServices[0] != "existing_service" {
		t.Errorf("Expected existing_service updated, got %s", mockCli.updatedServices[0])
	}
}
