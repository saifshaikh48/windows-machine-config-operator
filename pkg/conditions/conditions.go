package conditions

import (
	"context"

	operatorframework "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/pkg/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/windows-machine-config-operator/version"
)

const (
	UpgradeableFalseMessage = "upgradeIsNotSafe"
	UpgradeableFalseReason  = "The operator is currently upgrading Windows nodes with the latest WMCO version."
	UpgradeableTrueMessage  = "upgradeIsSafe"
	UpgradeableTrueReason   = ""
)

// SetUpgradeable updates the `Upgradable` Condition within the operator's OperatorCondition object.
// Creates the condition if not present, overrides spec with the given values otherwise. Status must be True or False
func SetUpgradeableCond(c client.Client, watchNamespace string, status meta.ConditionStatus,
	reason, message string) error {
	// OperatorCondition object name is of form `windows-machine-config-operator.vX.Y.Z`
	operatorCondition := "windows-machine-config-operator.v" + version.Get()

	operatorCond := &operatorframework.OperatorCondition{}
	err := c.Get(context.TODO(), kubeTypes.NamespacedName{Name: operatorCondition, Namespace: watchNamespace},
		operatorCond)
	if err != nil {
		return errors.Wrap(err, "unable to get OperatorCondition resource")
	}

	newUpgradeableCond := meta.Condition{
		Type:               operatorframework.Upgradeable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: meta.Now(),
	}
	// if Upgradeable condition already exists, update it. Otherwise add it in to the list of conditions
	replaced := false
	for _, cond := range operatorCond.Spec.Overrides {
		if cond.Type == operatorframework.Upgradeable {
			cond = newUpgradeableCond
			replaced = true
		}
	}
	if !replaced {
		operatorCond.Spec.Overrides = append(operatorCond.Spec.Overrides, newUpgradeableCond)
	}

	err = c.Update(context.TODO(), operatorCond)
	if err != nil {
		return errors.Wrapf(err, "unable to update condition %s to status %s", operatorframework.Upgradeable,
			meta.ConditionTrue)
	}

	// may be able to simplify using "github.com/operator-framework/operator-lib/conditions" Condition.Get/Set
	// _ = conditions.Condition.Set(context.TODO(), meta.ConditionTrue, conditions.WithReason("upgradeIsSafe),
	// 	conditions.WithMessage("..."))

	return nil
}
