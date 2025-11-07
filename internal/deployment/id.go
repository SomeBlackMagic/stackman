package deployment

import (
	"fmt"
	"math/rand"
	"time"
)

// GenerateDeployID creates a unique identifier for a deployment
// Format: deploy-YYYYMMDD-HHMMSS-RANDOM
// Example: deploy-20250107-143052-a1b2c3d4
func GenerateDeployID() string {
	timestamp := time.Now().Format("20060102-150405")
	randomPart := generateRandomString(8)
	return fmt.Sprintf("deploy-%s-%s", timestamp, randomPart)
}

// generateRandomString creates a random hexadecimal string of specified length
func generateRandomString(length int) string {
	const charset = "0123456789abcdef"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}

// DeployIDLabel is the Docker label key used to mark services and tasks with deployment ID
const DeployIDLabel = "com.stackman.deploy.id"
