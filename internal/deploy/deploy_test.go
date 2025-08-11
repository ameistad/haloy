package deploy

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeploymentID(t *testing.T) {
	id1 := CreateDeploymentID()
	time.Sleep(time.Millisecond * 10)
	id2 := CreateDeploymentID()

	fmt.Printf("Deployment IDs: %s, %s\n", id1, id2)
	assert.Len(t, id1, 16, "Deployment ID should be 16 characters long")
	assert.NotEqual(t, id1, id2, "Sequential deployment IDs should be different")
	assert.Regexp(t, `^\d{16}$`, id1, "Deployment ID should contain only digits")
	assert.Greater(t, id2, id1, "Deployment ID should be greater than the previous one")
}
