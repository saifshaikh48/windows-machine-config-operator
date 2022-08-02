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

package main

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/controller"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/winsvc"
)

var (
	cleanupCmd = &cobra.Command{
		Use:   "cleanup",
		Short: "Cleans up WMCO-managed Windows services and files",
		Long: "Stops and removes all Windows services and files as part of Node deconfiguration, " +
			"according to information given by Windows Service ConfigMaps present within the cluster",
		Run: runCleanupCmd,
	}
	// preserveNode is an optional flag that instructs WICD to deconfigure an instance without deleting the Node object
	preserveNode bool
)

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.PersistentFlags().BoolVar(&preserveNode, "preserveNode", false,
		"Flag to keep an instance's associated node in the cluster. Defaults to false, which deletes the node object.")
}

func runCleanupCmd(cmd *cobra.Command, args []string) {
	// Cannot use a cached client as no manager will be started to populate cache
	directClient, _, _, err := controller.NewDirectClient(apiServerURL, saCA, saToken)
	if err != nil {
		klog.Exitf("could not create authenticated client from service account: %s", err.Error())
	}

	svcMgr, err := winsvc.NewMgr()
	if err != nil {
		klog.Exitf("could not create service manager: %s", err.Error())
	}

	node, err := controller.GetAssociatedNode(directClient)
	if err != nil {
		klog.Exitf("could not node object associated with this instance: %s", err.Error())
	}

	klog.Infof("cleaning up managed services on node %s", node.Name)
	serviceController := controller.NewServiceController(context.TODO(), directClient, svcMgr, node.Name)
	if err := serviceController.Cleanup(preserveNode); err != nil {
		klog.Exitf(err.Error())
	}
}
