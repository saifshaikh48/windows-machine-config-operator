/*
Copyright 2024.

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

package controllers

import (
	"context"
	"fmt"
	"strings"

	mcfg "github.com/openshift/api/machineconfiguration/v1"
	core "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	kubeTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/openshift/windows-machine-config-operator/pkg/certificates"
	"github.com/openshift/windows-machine-config-operator/pkg/cluster"
	"github.com/openshift/windows-machine-config-operator/pkg/nodeconfig"
	"github.com/openshift/windows-machine-config-operator/pkg/secrets"
	"github.com/openshift/windows-machine-config-operator/pkg/signer"
)

//+kubebuilder:rbac:groups="machineconfiguration.openshift.io",resources=controllerconfigs,verbs=list;watch

const (
	// ControllerConfigController is the name of this controller in logs and other outputs.
	ControllerConfigController = "controllerconfig"
)

// ControllerConfigReconciler holds the info required to reconcile information held in ControllerConfigs
type ControllerConfigReconciler struct {
	instanceReconciler
}

// NewControllerConfigReconciler returns a pointer to a new ControllerConfigReconciler
func NewControllerConfigReconciler(mgr manager.Manager, clusterConfig cluster.Config,
	watchNamespace string) (*ControllerConfigReconciler, error) {
	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("error creating kubernetes clientset: %w", err)
	}

	return &ControllerConfigReconciler{
		instanceReconciler: instanceReconciler{
			client:             mgr.GetClient(),
			log:                ctrl.Log.WithName("controllers").WithName(ControllerConfigController),
			k8sclientset:       clientset,
			clusterServiceCIDR: clusterConfig.Network().GetServiceCIDR(),
			watchNamespace:     watchNamespace,
			recorder:           mgr.GetEventRecorderFor(ControllerConfigController),
		},
	}, nil
}

// Reconcile reacts to ControllerConfig changes in order to ensure the correct state of certificates on Windows nodes
func (r *ControllerConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cc mcfg.ControllerConfig
	err := r.client.Get(ctx, req.NamespacedName, &cc)
	if err != nil {
		if k8sapierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.signer, err = signer.Create(kubeTypes.NamespacedName{Namespace: r.watchNamespace,
		Name: secrets.PrivateKeySecret}, r.client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to create signer from private key secret: %w", err)
	}

	caBundle := ""
	for _, bundle := range cc.Spec.ImageRegistryBundleUserData {
		// Accumulate the certs into a single bundle, adding a comment with the image repo name for observability
		caBundle += fmt.Sprintf("# %s\n%s\n\n", strings.ReplaceAll(bundle.File, "..", ":"), bundle.Data)
	}
	for _, bundle := range cc.Spec.ImageRegistryBundleData {
		// Accumulate the certs into a single bundle, adding a comment with the image repo name for observability
		caBundle += fmt.Sprintf("# %s\n%s\n\n", strings.ReplaceAll(bundle.File, "..", ":"), bundle.Data)
	}
	// merge registry certs with proxy certs, if enabled
	if cluster.IsProxyEnabled() {
		proxyCA := &core.ConfigMap{}
		if err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: r.watchNamespace,
			Name: certificates.ProxyCertsConfigMap}, proxyCA); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to get ConfigMap %s: %w", certificates.ProxyCertsConfigMap, err)
		}
		caBundle += proxyCA.Data[certificates.CABundleKey]
	}

	// fetch all Windows nodes (Machine and BYOH instances)
	winNodes := &core.NodeList{}
	if err = r.client.List(ctx, winNodes, client.MatchingLabels{core.LabelOSStable: "windows"}); err != nil {
		return ctrl.Result{}, fmt.Errorf("error listing Windows nodes: %w", err)
	}
	// loop Windows nodes and trigger kubelet CA update
	for _, winNode := range winNodes.Items {
		winInstance, err := r.instanceFromNode(&winNode)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error creating instance for node %s: %w", winNode.Name, err)
		}
		nodeConfig, err := nodeconfig.NewNodeConfig(r.client, r.k8sclientset, r.clusterServiceCIDR,
			r.watchNamespace, winInstance, r.signer, nil, nil, r.platform)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error creating nodeConfig for instance %s: %w", winInstance.Address, err)
		}

		// copy the current kubelet CA file content to the Windows instance
		r.log.Info("updating kubelet CA client certificates in", "node", winNode.Name)
		if err := nodeConfig.UpdateKubeletClientCA(cc.Spec.KubeAPIServerServingCAData); err != nil {
			return ctrl.Result{}, fmt.Errorf("error updating kubelet CA certificate in node %s: %w", winNode.Name, err)
		}

		if err := nodeConfig.UpdateTrustedCABundleFile(caBundle); err != nil {
			return ctrl.Result{}, fmt.Errorf("error updating kubelet CA certificate in node %s: %w", winNode.Name, err)
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ControllerConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mccName := "machine-config-controller"
	mccPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == mccName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == mccName
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == mccName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcfg.ControllerConfig{}, builder.WithPredicates(mccPredicate)).
		Complete(r)
}
