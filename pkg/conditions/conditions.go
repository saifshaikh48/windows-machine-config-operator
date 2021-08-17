package conditions

import (
	"context"
	"os"

	operatorframework "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/operator-framework/operator-lib/conditions"
	"github.com/pkg/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	UpgradeableFalseMessage = "upgradeIsNotSafe"
	UpgradeableFalseReason  = "The operator is currently processing sub-components."
	UpgradeableTrueMessage  = "upgradeIsSafe"
	UpgradeableTrueReason   = "The operator is safe for upgrade."

	// OperatorConditionName is an environment variable set by OLM identifying the operator's OperatorCondition resource
	OperatorConditionName = "OPERATOR_CONDITION_NAME"
)

// setUpgradeable updates the `Upgreadable` Condition within the operator's OperatorCondition object.
// Creates the condition if not present, overrides condition spec otherwise. No-op if operator is not OLM managed
func SetUpgradeable(c client.Client, status meta.ConditionStatus, reason, message string) error {
	if isManagedByOLM() {
		cond, err := conditions.NewCondition(c, operatorframework.ConditionType(operatorframework.Upgradeable))
		if err != nil {
			return err
		}
		err = cond.Set(context.TODO(), status, conditions.WithReason(reason), conditions.WithMessage(message))
		if err != nil {
			return errors.Wrapf(err, "unable to set condition %s to status %s", operatorframework.Upgradeable, status)
		}
	}
	return nil
}

// isManagedByOLM checks if the operator is managed by OLM based on the presence of a specfic environment variable
func isManagedByOLM() bool {
	_, present := os.LookupEnv(OperatorConditionName)
	return present
}

// SetUpgradeableCond updates the `Upgradeable` Condition within the operator's OperatorCondition object.
// Creates the condition if not present, overrides spec with the given values otherwise.
func SetUpgradeableCond(c client.Client, watchNamespace string, status meta.ConditionStatus,
	reason, message string) error {
	// OperatorCondition object name is of form `windows-machine-config-operator.vX.Y.Z`
	operatorCondition := "windows-machine-config-operator.v3.0.0"
	//"error": "unable to set condition Upgradeable: Operation cannot be fulfilled on operatorconditions.operators.coreos.com \"windows-machine-config-operator.v3.0.0\": the object has been modified; please apply your changes to the latest version and try again"
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
