package autogrow

import (
	"testing"

	"gotest.tools/v3/assert"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestNewRunner(t *testing.T) {
}

func TestEnqueueCluster(t *testing.T) {
}

func TestRunnerLeaderElectionRunnable(t *testing.T) {
	var runner manager.LeaderElectionRunnable = &Runner{}

	assert.Assert(t, runner.NeedLeaderElection())
}
