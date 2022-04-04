package e2e

import (
	"context"
	"testing"

	"github.com/openshift/windows-machine-config-operator/pkg/wiparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func concurrencySuite(t *testing.T) {
	t.Run("Concurrent Deconfigure/Configure Events", testConcurrency)
}

// testConcurrency tests behaviour when the ConfigMap controller reconciles multiple requests at the same time.
func testConcurrency(t *testing.T) {
	if gc.numberOfBYOHNodes == 0 {
		t.Skip("BYOH testing disabled")
	}

	tc, err := NewTestContext()
	require.NoError(t, err)

	// remove BYOH entry from configmap then immediately re-add it
	windowsInstances, err := tc.client.K8s.CoreV1().ConfigMaps(tc.namespace).Get(context.TODO(),
		wiparser.InstanceConfigMap, metav1.GetOptions{})
	require.NoError(t, err, "error retrieving windows-instances ConfigMap")
	require.NotEmpty(t, windowsInstances.Data, "no instances to remove")

	addr, data, err := tc.removeSingleInstanceEntry(windowsInstances)
	require.NoError(t, err)

	err = tc.addSingleInstanceEntry(windowsInstances, addr, data)
	require.NoError(t, err)

	// wait for the node to be removed
	err = tc.waitForWindowsNodes(gc.numberOfBYOHNodes-1, false, true, true)
	require.NoError(t, err, "error waiting for the removal of a node")

	// wait for the node to be successfully re-added
	err = tc.waitForWindowsNodes(gc.numberOfBYOHNodes, false, true, true)
	assert.NoError(t, err, "error waiting for the Windows node to be re-added")
}
