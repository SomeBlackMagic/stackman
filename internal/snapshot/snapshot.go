package snapshot

import (
	"context"
	"fmt"
	"log"

	"github.com/SomeBlackMagic/stackman/internal/swarm"
)

// CreateSnapshot creates a snapshot of the current stack state before deployment
// Returns error if snapshot creation fails to prevent deployment without rollback capability
func CreateSnapshot(ctx context.Context, stackDeployer *swarm.StackDeployer) (*swarm.StackSnapshot, error) {
	log.Println("Creating snapshot of current stack state...")
	snapshot, err := stackDeployer.CreateSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot (rollback will not be available): %w", err)
	}
	log.Println("Snapshot created successfully")
	return snapshot, nil
}

// Rollback rollback restores the stack to a previous snapshot state
func Rollback(ctx context.Context, stackDeployer *swarm.StackDeployer, snapshot *swarm.StackSnapshot) {
	if snapshot == nil {
		log.Println("No snapshot available, cannot rollback")
		return
	}

	fmt.Println("Starting rollback to previous state...")

	if err := stackDeployer.Rollback(ctx, snapshot); err != nil {
		log.Printf("Rollback failed: %v", err)
		log.Println("Manual intervention may be required")
		return
	}

	fmt.Println("Rollback completed successfully")
}
