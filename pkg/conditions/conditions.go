package conditions

import (
	"context"

	operatorframework "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/operator-lib/conditions"
	"github.com/pkg/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	UpgradeableFalseMessage = "upgradeIsNotSafe"
	UpgradeableFalseReason  = "The operator is currently upgrading Windows nodes with the latest WMCO version."
	UpgradeableTrueMessage  = "upgradeIsSafe"
	UpgradeableTrueReason   = "No child components under upgrade."
)

// setUpgradeable updates the `Upgradable` Condition within the operator's OperatorCondition object.
// Creates the condition if not present, overrides spec with the given values otherwise. Status must be True or False
func SetUpgradeable(c client.Client, status meta.ConditionStatus, reason,
	message string) error {
	cond, err := conditions.NewCondition(c, operatorframework.ConditionType(operatorframework.Upgradeable))
	if err != nil {
		return err
	}
	err = cond.Set(context.TODO(), status, conditions.WithReason(reason),
		conditions.WithMessage(message))
	if err != nil {
		return errors.Wrapf(err, "unable to set condition %s to status %s", operatorframework.Upgradeable, status)
	}
	return nil
}
