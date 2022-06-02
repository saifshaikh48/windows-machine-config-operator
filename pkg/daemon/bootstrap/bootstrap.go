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

package bootstrap

import (
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/controller"
	"github.com/openshift/windows-machine-config-operator/pkg/servicescm"
)

func Execute(kubeconfigPath, encodedCMData string) error {
	ctx := ctrl.SetupSignalHandler()
	cmData, err := servicescm.DecodeAndUnmarshall(encodedCMData)
	if err != nil {
		errors.Wrap(err, "unable to decode ConfigMap data")
	}
	_, serviceController, err := controller.NewControllerWithManager(ctx, kubeconfigPath)
	if err != nil {
		return err
	}
	return serviceController.Bootstrap(cmData.Services)
}
