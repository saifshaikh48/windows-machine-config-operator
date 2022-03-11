package wsvalidator

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/version"
)

// ServicesConfigMap is the name of the ConfigMap detailing service configuration for a specific WMCO version
var ServicesConfigMap string

// init runs once, initializing global variables
func init() {
	ServicesConfigMap = getServicesConfigMapName()
}

// getServicesConfigMapName returns the ConfigMap with the naming scheme:
// windows-services-<MajorVersion>-<MinorVersion>-<PatchVersion>-<CommitHash>
func getServicesConfigMapName() string {
	// resource names cannot have `+` in them
	sanitizedVersion := strings.ReplaceAll(version.Get(), "+", "-")
	return fmt.Sprintf("windows-services-%s", sanitizedVersion)
}
