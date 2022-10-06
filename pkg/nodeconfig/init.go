package nodeconfig

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// cache holds the information of the nodeConfig that is invariant for multiple reconciliation cycles. We'll use this
// information when we don't want to get the information from the global context coming from reconciler
// but to have something at nodeConfig package locally which will be passed onto other structs.
type cache struct {
	// apiServerEndpoint is the address which clients can interact with the API server through
	apiServerEndpoint string
}

// cache has the information related to nodeConfig that should not be changed.
var nodeConfigCache = cache{}

// init populates the cache that we need for nodeConfig
func init() {
	var kubeAPIServerEndpoint string
	log := ctrl.Log.WithName("nodeconfig").WithName("init")

	kubeAPIServerEndpoint, err := discoverKubeAPIServerEndpoint()
	if err != nil {
		log.Error(err, "unable to find kube api server endpoint")
		return
	}
	// populate the cache
	nodeConfigCache.apiServerEndpoint = kubeAPIServerEndpoint
}
