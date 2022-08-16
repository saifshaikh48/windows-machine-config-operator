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
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/cleanup"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/config"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/controller"
	"github.com/openshift/windows-machine-config-operator/pkg/daemon/manager"
)

var (
	cleanupCmd = &cobra.Command{
		Use:   "cleanup",
		Short: "Cleans up WMCO-managed Windows services and files",
		Long: "Stops and removes all Windows services and files as part of Node deconfiguration, " +
			"according to information given by Windows Service ConfigMaps present within the cluster",
		Run: runCleanupCmd,
	}
	// preserveNode is an optional flag that instructs WICD to deconfigure an instance without deleting the Node object.
	// This is useful in the node upgrade scenario.
	preserveNode bool
)

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.PersistentFlags().BoolVar(&preserveNode, "preserveNode", false,
		"If set, deletes the Node associated with this instance. Defaults to false.")
}

func runCleanupCmd(cmd *cobra.Command, args []string) {
	cfg, err := config.FromServiceAccount(apiServerURL, saCA, saToken)
	if err != nil {
		klog.Exitf("error using service account to build config: %s", err.Error())
	}
	// Cannot use a cached client as no manager will be started to populate cache
	directClient, err := controller.NewDirectClient(cfg)
	if err != nil {
		klog.Exitf("could not create authenticated client from service account: %s", err.Error())
	}
	addrs, err := controller.LocalInterfaceAddresses()
	if err != nil {
		klog.Exitf(err.Error())
	}
	svcMgr, err := manager.New()
	if err != nil {
		klog.Exitf("could not create service manager: %s", err.Error())
	}
	if err := cleanup.Deconfigure(directClient, addrs, svcMgr, preserveNode); err != nil {
		klog.Exitf(err.Error())
	}
}
