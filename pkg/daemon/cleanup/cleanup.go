//go:build windows

/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cleanup

import (
	"context"
	"fmt"
	"strings"

	core "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/certs"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/controller"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/envvar"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/manager"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/powershell"
	"github.com/openshift/windows-machine-config-operator/pkg/metadata"
	"github.com/openshift/windows-machine-config-operator/pkg/servicescm"
	"github.com/openshift/windows-machine-config-operator/pkg/windows"
)

// Deconfigure removes all managed services from the instance and the version annotation, if it has an associated node.
// If we are able to get the services ConfigMap tied to the desired version, all services defined in it are cleaned up.
// Otherwise, cleanup is based on the latest services ConfigMap.
// TODO: remove services with the OpenShift managed tag in best effort cleanup https://issues.redhat.com/browse/WINC-853
func Deconfigure(cfg *rest.Config, ctx context.Context, configMapNamespace string) error {
	// Cannot use a cached client as no manager will be started to populate cache
	directClient, err := controller.NewDirectClient(cfg)
	if err != nil {
		return fmt.Errorf("could not create authenticated client from service account: %w", err)
	}
	addrs, err := controller.LocalInterfaceAddresses()
	if err != nil {
		return err
	}
	node, err := controller.GetAssociatedNode(directClient, addrs)
	if err != nil {
		klog.Infof("no associated node found")
	}
	svcMgr, err := manager.New()
	if err != nil {
		klog.Exitf("could not create service manager: %s", err.Error())
	}
	defer svcMgr.Disconnect()

	latestCM, err := servicescm.GetLatest(directClient, ctx, configMapNamespace)
	if err != nil {
		return fmt.Errorf("cannot get latest services ConfigMap from namespace %s: %w", configMapNamespace, err)
	}
	// to start, our source of truth for cleanup data will be the latest services CM.
	// This will only be modified if if the instance is not already configured by the latest version
	dataToCleanup, err := servicescm.Parse(latestCM.Data)
	if err != nil {
		return err
	}

	// to start, we assume that we'll need to do best effort cleanup.
	// This is only be modified if the instance is already configured by the latest version
	bestEffort := true

	if versionCM, err := getVersionedCM(ctx, directClient, configMapNamespace, node); err == nil {
		// If the CMs do not refer to the same version, merge the data
		if versionCM.GetName() != latestCM.GetName() {
			versionCMData, err := servicescm.Parse(versionCM.Data)
			if err != nil {
				return err
			}
			mergedServices := mergeServices(dataToCleanup.Services, versionCMData.Services)
			mergedEnvVars := merge(dataToCleanup.WatchedEnvironmentVars, versionCMData.WatchedEnvironmentVars)
			dataToCleanup = &servicescm.Data{Services: mergedServices, WatchedEnvironmentVars: mergedEnvVars}
		} else {
			bestEffort = false
		}
	} else {
		klog.Warningf("could not get services ConfigMap associated with node version annotation: %s", err.Error())
	}

	if err = removeServices(svcMgr, dataToCleanup.Services, bestEffort); err != nil {
		return err
	}

	envVarsRemoved, err := ensureEnvVarsAreRemoved(dataToCleanup.WatchedEnvironmentVars)
	if err != nil {
		return err
	}

	certsRemoved, err := certs.Reconcile("")
	// rebooting instance to unset the environment variables and available certifictes at the process level as expected
	if envVarsRemoved || certsRemoved {
		// Applying the reboot annotation results in an event picked up by WMCO's node controller to reboot the instance
		if annotationErr := metadata.ApplyRebootAnnotation(ctx, directClient, *node); annotationErr != nil {
			return fmt.Errorf("error setting reboot annotation on node %s: %w", node.Name, annotationErr)
		}
	}
	if err != nil {
		return err
	}

	cleanupContainers()

	if node != nil {
		return metadata.RemoveVersionAnnotation(ctx, directClient, *node)
	}
	return nil
}

// getVersionedCM attempts to the version CM specified by the node's version annotation
func getVersionedCM(ctx context.Context, cli client.Client, configMapNamespace string,
	node *core.Node) (*core.ConfigMap, error) {
	var versionCM *core.ConfigMap
	if node == nil {
		return versionCM, fmt.Errorf("no node object present")
	}
	version, present := node.Annotations[metadata.VersionAnnotation]
	if !present {
		return versionCM, fmt.Errorf("node is missing version annotation")
	}

	// attempt to get the ConfigMap specified by the version annotation
	err := cli.Get(ctx, client.ObjectKey{Namespace: configMapNamespace, Name: servicescm.NamePrefix + version}, versionCM)
	if err != nil {
		return versionCM, err
	}
	return versionCM, nil
}

// mergeServices combines the list of services, prioritizing the data given by s1
func mergeServices(s1, s2 []servicescm.Service) []servicescm.Service {
	services := make(map[string]servicescm.Service)
	for _, service := range s1 {
		services[service.Name] = service
	}
	for _, service := range s2 {
		if _, present := services[service.Name]; !present {
			services[service.Name] = service
		}
	}
	var merged []servicescm.Service
	for _, service := range services {
		merged = append(merged, service)
	}
	return merged

}

// merge returns a combined list of the given lists
func merge(e1, e2 []string) []string {
	watchedEnvVars := make(map[string]struct{})
	for _, item := range e1 {
		watchedEnvVars[item] = struct{}{}
	}
	for _, item := range e2 {
		watchedEnvVars[item] = struct{}{}
	}
	var merged []string
	for item := range watchedEnvVars {
		merged = append(merged, item)
	}
	return merged
}

// removeServices uses the given manager to remove all the given Windows services from this instance.
// The bestEffort flag is used to also remove OpenShift managed services that are not in the given Service slice.
func removeServices(svcMgr manager.Manager, services []servicescm.Service, bestEffort bool) error {
	// Build up log message and failures
	servicesRemoved := []string{}
	failedRemovals := []error{}
	// The services are ordered by increasing priority already, so stop them in reverse order to avoid dependency issues
	for i := len(services) - 1; i >= 0; i-- {
		service := services[i]
		if err := svcMgr.DeleteService(service.Name); err != nil {
			failedRemovals = append(failedRemovals, err)
		} else {
			servicesRemoved = append(servicesRemoved, service.Name)
		}
	}

	if bestEffort {
		// Remove all services that were created through WICD, even if they are no longer in the services ConfigMap spec
		existingSvcs, err := svcMgr.GetServices()
		if err != nil {
			return fmt.Errorf("could not determine existing Windows services: %w", err)
		}
		for name := range existingSvcs {
			winSvcObj, err := svcMgr.OpenService(name)
			if err != nil {
				// WICD is not able to access some system services. If we hit this, it is expected
				continue
			}
			config, err := winSvcObj.Config()
			if err != nil {
				return err
			}
			if strings.Contains(config.Description, windows.ManagedTag) {
				if err := svcMgr.DeleteService(name); err != nil {
					failedRemovals = append(failedRemovals, err)
					winSvcObj.Close()
				} else {
					servicesRemoved = append(servicesRemoved, name)
				}
			}
		}
	}

	klog.Infof("removed services: %q", servicesRemoved)
	if len(failedRemovals) > 0 {
		return fmt.Errorf("%#v", failedRemovals)
	}
	return nil
}

// ensureEnvVarsAreRemoved removes all WICD configured ENV variables from this instance
func ensureEnvVarsAreRemoved(watchedEnvVars []string) (bool, error) {
	return envvar.Reconcile(map[string]string{}, watchedEnvVars)
}

// cleanupContainers makes a best effort to stop all processes with the name containerd-shim-runhcs-v1, stopping
// any containers which were not able to be drained from the Node.
func cleanupContainers() {
	cmdRunner := powershell.NewCommandRunner()
	cmdRunner.Run("Stop-Process -Force -Name containerd-shim-runhcs-v1")
	return
}
