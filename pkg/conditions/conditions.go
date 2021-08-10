package conditions

import (
	"context"
	"encoding/json"
	"os"

	operatorframework "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/pkg/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/windows-machine-config-operator/pkg/patch"
)

const (
	UpgradeableFalseMessage = "upgradeIsNotSafe"
	UpgradeableFalseReason  = "The operator is currently processing sub-components."
	UpgradeableTrueMessage  = "upgradeIsSafe"
	UpgradeableTrueReason   = "The operator is safe for upgrade."

	// OperatorConditionName is an environment variable set by OLM identifying the operator's OperatorCondition resource
	OperatorConditionName = "OPERATOR_CONDITION_NAME"
)

// PatchUpgradeable modifies the Upgreadable condition within the operator's OperatorCondition object.
// Creates the Condition if not present, overrides it otherwise. No-op if operator is not OLM managed
func PatchUpgradeable(c client.Client, opNamespace string, status meta.ConditionStatus, reason, message string) error {
	ocName, present := os.LookupEnv(OperatorConditionName)
	if !present {
		// Implies operator is not OLM-managed, do nothing
		return nil
	}
	oc := &operatorframework.OperatorCondition{}
	if err := c.Get(context.TODO(), kubeTypes.NamespacedName{Namespace: opNamespace, Name: ocName}, oc); err != nil {
		return errors.Wrap(err, "unable to get OperatorCondition resource")
	}

	upgradeableCond := []meta.Condition{{
		Type:               operatorframework.Upgradeable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: meta.Now(),
	}}
	patchData, err := json.Marshal([]*patch.JSONPatch{patch.NewJSONPatch("add", "/spec/conditions", upgradeableCond)})
	if err != nil {
		return errors.Wrap(err, "unable to generate new condition's patch data")
	}

	if err = c.Patch(context.TODO(), oc, client.RawPatch(kubeTypes.JSONPatchType, patchData)); err != nil {
		return errors.Wrapf(err, "unable to apply patch %s", patchData)
	}
	return nil

}
