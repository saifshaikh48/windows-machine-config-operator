package wsvalidator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/windows-machine-config-operator/version"
)

// ServicesConfigMap is the name of the ConfigMap detailing service configuration for a specific WMCO version
var ServicesConfigMap string

// init runs once, initializing global variables
func init() {
	ServicesConfigMap = getServicesConfigMapName()
}

const (
	// servicesKey contains all data required to configure the required Windows services on any instance
	// that is to be added to the cluster as a Node. The value for this key is a Service object JSON array.
	servicesKey = "services"
	// filesKey is a key within the services ConfigMap. The value for this key is a FileInfo object JSON array.
	filesKey = "files"
)

// nodeVariable is a variable whose value is sourced from the node object associated with a Windows instance
type NodeVariable struct {
	// Name is the variable Name as it appears in commands
	Name string `json:"name"`
	// JsonPathNodeObject is the jsonPath of a field within the instance's Node object
	JsonPathNodeObject string `json:"jsonPathNodeObject"`
}

// powershellVariable is a variable whose value is is determined from the output of a PowerShell script
type PowershellVariable struct {
	// Name is the variable name as it appears in commands
	Name string `json:"name"`
	// Path is the location of the PowerShell script to be run
	Path string `json:"path"`
}

// Service represents the configuration spec of a Windows service
type Service struct {
	// Name is the name of the Windows service
	Name string `json:"name"`
	// Command is command that will be executed. This could potentially include strings whose values will be derived
	// from NodeVariablesInCommand and PowershellVariablesInCommand.
	Command string `json:"path"`
	// Before a command is run on a Windows instance, all node and PowerShell variables will be replaced by their values
	NodeVariablesInCommand       []NodeVariable       `json:"nodeVariablesInCommand,omitempty"`
	PowershellVariablesInCommand []PowershellVariable `json:"powershellVariablesInCommand,omitempty"`
	// Dependencies is a list of service names that this service is dependent on
	Dependencies []string `json:"dependencies,omitempty"`
	// Bootstrap is a boolean flag indicating whether this service should be handled as part of node bootstrapping
	Bootstrap bool `json:"bootstrap"`
	// Priority is a non-negative integer that will be used to order the creation of the services.
	// Priority 0 is created first
	Priority uint `json:"priority"`
}

// FileInfo contains the path and checksum of files copied to the instance by WMCO
type FileInfo struct {
	// Path is the filepath of a file on an instance
	Path string `json:"path"`
	// Checksum is used to validate that a file has not been changed
	Checksum string `json:"checksum"`
}

// GenerateWindowsServiceConfigMap creates an immutable service ConfigMap which provides WICD with the specifications
// for each Windows service that must be created on a Windows instance.
func GenerateWindowsServiceConfigMap(name, namespace string) *core.ConfigMap {
	immutable := true
	cm := &core.ConfigMap{
		ObjectMeta: meta.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Immutable: &immutable,
		Data:      make(map[string]string),
	}

	//TODO: Fill in data as services are added to the ConfigMap definition
	services, _ := json.Marshal([]Service{})
	cm.Data[servicesKey] = string(services)
	files, _ := json.Marshal([]FileInfo{})
	cm.Data[filesKey] = string(files)

	return cm
}

// ParseAndValidate converts ConfigMap data into the objects representing a Windows services ConfigMap and
// ensures the data adheres to the expected schema
func ParseAndValidate(data map[string]string) (*[]FileInfo, *[]Service, error) {
	services := &[]Service{}
	files := &[]FileInfo{}

	value, ok := data[servicesKey]
	if !ok {
		return nil, nil, errors.Errorf("expected `%s` key to exist", servicesKey)
	}
	if err := json.Unmarshal([]byte(value), services); err != nil {
		return nil, nil, errors.Wrapf(err, "`%s` must represent list of Service", servicesKey)
	}

	value, ok = data[filesKey]
	if !ok {
		return nil, nil, errors.Errorf("expected `%s` key to exist", filesKey)
	}
	if err := json.Unmarshal([]byte(value), files); err != nil {
		return nil, nil, errors.Wrapf(err, "`%s` must represent list of FileInfo", filesKey)
	}

	if err := validateDependencies(services); err != nil {
		return nil, nil, err
	}
	if err := validatePriorities(services); err != nil {
		return nil, nil, err
	}
	return files, services, nil
}

// internal helper functions

// validateDependencies ensures that no bootstrap service depends on a non-bootstrap service
func validateDependencies(services *[]Service) error {
	boostrapServices := []Service{}
	nonBoostrapServices := []Service{}
	for _, svc := range *services {
		if svc.Bootstrap {
			boostrapServices = append(boostrapServices, svc)
		} else {
			nonBoostrapServices = append(nonBoostrapServices, svc)
		}
	}

	for _, bootstrapSvc := range boostrapServices {
		for _, dependency := range bootstrapSvc.Dependencies {
			for _, nonBootstrapSvc := range nonBoostrapServices {
				if dependency == nonBootstrapSvc.Name {
					return errors.Errorf("bootstrap service %s cannot depend on non-boostrap service %s",
						bootstrapSvc.Name, nonBootstrapSvc.Name)
				}
			}
		}
	}
	return nil
}

// validatePriorities ensures that each service that has the bootstrap flag set as true has a higher priority than all
// non-bootstrap services. There should be no overlap in the priorities of bootstrap services and controller services.
func validatePriorities(services *[]Service) error {
	// sort services to ascending priority, bootstrap services towards the front of slice
	sort.Slice(*services, func(i, j int) bool {
		return (*services)[i].Priority < (*services)[j].Priority
	})

	// ensure no bootstrap service is after a controller service in the ordered list
	nonBoostrapSeen := false
	lastBoostrapPriority := uint(0)
	for _, svc := range *services {
		if svc.Bootstrap {
			if nonBoostrapSeen {
				return errors.Errorf("bootstrap service %s priority must be higher than all controller services",
					svc.Name)
			}
			lastBoostrapPriority = svc.Priority
		} else {
			// corner case if two adjacent bootstrap and controller services have the same priority
			if svc.Priority == lastBoostrapPriority {
				return errors.Errorf("non-service %s priority must not overlap with bootstrap services", svc.Name)
			}
			nonBoostrapSeen = true
		}
	}
	return nil
}

// getServicesConfigMapName returns the ConfigMap with the naming scheme:
// windows-services-<MajorVersion>-<MinorVersion>-<PatchVersion>-<CommitHash>
func getServicesConfigMapName() string {
	// resource names cannot have `+` in them
	sanitizedVersion := strings.ReplaceAll(version.Get(), "+", "-")
	return fmt.Sprintf("windows-services-%s", sanitizedVersion)
}
