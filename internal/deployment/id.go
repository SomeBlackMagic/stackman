package deployment

import (
	"crypto/rand"
	"fmt"
	"time"
)

const DeployIDLabel = "com.stackman.deploy.id"

func GenerateDeployID() string {
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		panic(fmt.Sprintf("deployment: failed to generate random bytes: %v", err))
	}

	return fmt.Sprintf("deploy-%s-%x", time.Now().UTC().Format("20060102-150405"), randomBytes)
}
