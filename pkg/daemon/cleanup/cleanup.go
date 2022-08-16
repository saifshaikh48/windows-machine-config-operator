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
	"net"
	"strings"

	"github.com/pkg/errors"
	core "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/controller"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/manager"
	"github.com/openshift/windows-machine-config-operator/pkg/nodeconfig"
	"github.com/openshift/windows-machine-config-operator/pkg/servicescm"
	"github.com/openshift/windows-machine-config-operator/pkg/windows"
)

// Deconfigure removes an instance from the cluster. If the parameter flag is true, the node object is not deleted.
// If we are able to get the services ConfigMap tied to the desired version, all services defined in it are cleaned up.
// Otherwise, perform cleanup based on a combination of the OpenShift managed tag and the latest services ConfigMap.
func Deconfigure(c client.Client, netInterfaceAddresses []net.Addr, svcMgr manager.Manager, preserveNode bool) error {
	var err error
	var node *core.Node
	ctx := context.TODO()
	cm := &core.ConfigMap{}
	// Fetch the CM of the desired version
	err = func() error {
		node, err = controller.GetAssociatedNode(c, netInterfaceAddresses)
		if err != nil {
			return err
		}
		desiredVersion, present := node.Annotations[nodeconfig.DesiredVersionAnnotation]
		if !present {
			return errors.Wrapf(err, "node missing desired version annotation")
		}
		return c.Get(ctx,
			client.ObjectKey{Namespace: controller.WMCONamespace, Name: servicescm.NamePrefix + desiredVersion}, cm)
	}()
	if err != nil {
		klog.Warningf(err.Error())
		if err = outOfSyncCleanup(c, ctx, svcMgr); err != nil {
			return err
		}
	} else {
		// Happy path, no issues getting the desired services ConfigMap
		if err = removeCMServices(c, ctx, svcMgr, cm); err != nil {
			return err
		}
	}
	// Delete node if needed
	if preserveNode || node == nil {
		return nil
	}
	klog.Infof("deleting node %s", node.GetName())
	return c.Delete(ctx, node)
}

// removeCMServices removes all the services defined in the given ConfigMap from this instance.
// The given Node object is deleted if the given flag is false
func removeCMServices(c client.Client, ctx context.Context, svcMgr manager.Manager, cm *core.ConfigMap) error {
	cmData, err := servicescm.Parse(cm.Data)
	if err != nil {
		return err
	}
	// Build up log message and failures
	servicesRemoved := []string{}
	var failedRemovals error
	// The services are ordered by increasing priority already, so stop them in reverse order to avoid dependency issues
	for i := len(cmData.Services) - 1; i >= 0; i-- {
		service := cmData.Services[i]
		if err := svcMgr.DeleteService(service.Name); err != nil {
			failedRemovals = errors.Errorf("unable to delete %s (%v); %s", service.Name, err, failedRemovals)
		}
		servicesRemoved = append(servicesRemoved, service.Name)
	}
	if failedRemovals != nil {
		return failedRemovals
	}
	klog.Infof("deleted services %v from ConfigMap %s", servicesRemoved, cm.Name)
	return nil
}

// outOfSyncCleanup removes all services on the instance with the Openshift managed tag, as well as any services defined
// in the latest retreivable version of the services ConfigMap
func outOfSyncCleanup(c client.Client, ctx context.Context, svcMgr manager.Manager) error {
	// Try to get the latest services ConfigMap. If it exists, the services in it will be deconfigured.
	// This will enable clean up instances even if service descriptions were somehow overwritten.
	cm, err := servicescm.GetLatestServicesCM(c, ctx, controller.WMCONamespace)
	if err != nil {
		klog.Warningf("unable to find latest services ConfigMap: %s", err.Error())
	} else {
		if err = removeCMServices(c, ctx, svcMgr, cm); err != nil {
			return err
		}
	}

	klog.Infof("cleaning up all remaining %s Windows services", windows.ManagedTag)
	allServices, err := svcMgr.GetServices()
	if err != nil {
		return errors.Wrap(err, "could not determine existing Windows services")
	}
	// Build up any failures
	var failedRemovals error
	for service := range allServices {
		exists, err := svcMgr.ServiceExists(service)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}

		winSvcObj, err := svcMgr.OpenService(service)
		if err != nil {
			failedRemovals = errors.Errorf("unable to open %s (%v); %s", service, err, failedRemovals)
			continue
		}
		defer winSvcObj.Close()

		config, err := winSvcObj.Config()
		if err != nil {
			failedRemovals = errors.Errorf("unable to get %s service config (%v); %s", service, err, failedRemovals)
			continue
		}
		// Only remove the service if it has the expected tag
		if !strings.HasPrefix(config.Description, windows.ManagedTag) {
			continue
		}
		if err = svcMgr.DeleteServiceAndDependents(service); err != nil {
			failedRemovals = errors.Errorf("unable to delete %s (%v); %s", service, err, failedRemovals)
		}
	}
	return failedRemovals
}
