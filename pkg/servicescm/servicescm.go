package servicescm

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/version"
)

// Name is the name of the Windows Services ConfigMap, detailing service configuration for a specific WMCO version
var Name string

// init runs once, initializing global variables
func init() {
	Name = getName()
}

// getName returns the name of the ConfigMap, using the following naming convention:
// windows-services-<WMCOFullVersion>
func getName() string {
	return fmt.Sprintf("windows-services-%s", version.Get())
}
