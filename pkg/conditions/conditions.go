package conditions

import (
	"context"
	"os"

	operatorframework "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/operator-framework/operator-lib/conditions"
	"github.com/pkg/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			//"error": "unable to set condition Upgradeable: Operation cannot be fulfilled on operatorconditions.operators.coreos.com \"windows-machine-config-operator.v3.0.0\": the object has been modified; please apply your changes to the latest version and try again"
		}
	}
	return nil
}

// isManagedByOLM checks if the operator is managed by OLM based on the presence of a specfic environment variable
func isManagedByOLM() bool {
	_, present := os.LookupEnv(OperatorConditionName)
	return present
}
