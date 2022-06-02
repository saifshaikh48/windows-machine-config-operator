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

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/bootstrap"
)

var (
	bootstrapCmd = &cobra.Command{
		Use:   "bootstrap",
		Short: "Starts required Windows Services to bootstrap a Node",
		Long: "Starts and configures all required Windows Services on an instance according to information given by " +
			"Windows Service ConfigMaps, resulting in the creation of a new Node object",
		Run: runBootstrapCmd,
	}
	// encodedCMData holds encoded data from the Windows services ConfigMap
	// TODO: Remove this when the WICD controller has permissions to watch ConfigMaps
	encodedCMData string
)

func init() {
	rootCmd.AddCommand(bootstrapCmd)
	// TODO: make kubeconfig flag persistent + required upon rootCmd so all subcommands also have access
	bootstrapCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig")
	bootstrapCmd.MarkPersistentFlagRequired("kubeconfig")
	bootstrapCmd.PersistentFlags().StringVar(&encodedCMData, "services-data", "", "Windows services ConfigMap data")
	bootstrapCmd.MarkPersistentFlagRequired("services-data")
}

func runBootstrapCmd(cmd *cobra.Command, args []string) {
	klog.Info("starting services to bootstrap node")
	if err := bootstrap.Execute(kubeconfig, encodedCMData); err != nil {
		klog.Exitf(err.Error())
	}
}
