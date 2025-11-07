package parts

import (
	"context"
	"fmt"
	"log"
	"time"

	"stackman/deployer"
)

// createSnapshot creates a snapshot of current stack state before deployment
func CreateSnapshot(ctx context.Context, stackDeployer *deployer.StackDeployer) *deployer.StackSnapshot {
	log.Println("Creating snapshot of current stack state...")
	snapshot, err := stackDeployer.CreateSnapshot(ctx)
	if err != nil {
		log.Printf("Warning: failed to create snapshot: %v", err)
		log.Println("Continuing without rollback capability")
		return nil
	}
	return snapshot
}

// rollback restores the stack to a previous snapshot state
func Rollback(ctx context.Context, stackDeployer *deployer.StackDeployer, snapshot *deployer.StackSnapshot) {
	if snapshot == nil || len(snapshot.Services) == 0 {
		log.Println("No snapshot available, cannot rollback")
		return
	}

	fmt.Println("Starting rollback to previous state...")

	// Create new context with timeout for rollback
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer rollbackCancel()

	if err := stackDeployer.Rollback(rollbackCtx, snapshot); err != nil {
		log.Printf("Rollback failed: %v", err)
		log.Println("Manual intervention may be required")
		return
	}

	fmt.Println("Rollback completed successfully")
}
