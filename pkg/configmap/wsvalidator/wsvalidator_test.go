package wsvalidator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDependencies(t *testing.T) {
	testCases := []struct {
		name        string
		input       *[]Service
		expectedErr bool
	}{
		{
			name:        "empty services list",
			input:       &[]Service{},
			expectedErr: false,
		},
		{
			name: "no dependencies",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     1,
				},
			},
			expectedErr: false,
		},
		{
			name: "valid dependencies",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{"new-bootstrap-service"},
					Bootstrap:    false,
					Priority:     1,
				},
				{
					Name:         "test-controller-service-2",
					Command:      "C:\\test-controller-service-2",
					Dependencies: []string{"test-controller-service-2"},
					Bootstrap:    false,
					Priority:     2,
				},
			},
			expectedErr: false,
		},
		{
			name: "bootstrap depends on all non-bootstrap services",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{"test-controller-service", "test-controller-service-2"},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     1,
				},
				{
					Name:         "test-controller-service-2",
					Command:      "C:\\test-controller-service-2",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     2,
				},
			},
			expectedErr: true,
		},
		{
			name: "bootstrap in the middle of non-bootstrap services",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{"test-controller-service"},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     1,
				},
				{
					Name:         "test-controller-service-2",
					Command:      "C:\\test-controller-service-2",
					Dependencies: []string{"new-bootstrap-service"},
					Bootstrap:    false,
					Priority:     2,
				},
			},
			expectedErr: true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			err := validateDependencies(test.input)
			if test.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidatePriorities(t *testing.T) {
	testCases := []struct {
		name        string
		input       *[]Service
		expectedErr bool
	}{
		{
			name:        "empty services list",
			input:       &[]Service{},
			expectedErr: false,
		},
		{
			name: "valid priorities",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{"new-bootstrap-service"},
					Bootstrap:    false,
					Priority:     2,
				},
				{
					Name:         "test-controller-service-2",
					Command:      "C:\\test-controller-service-2",
					Dependencies: []string{"test-controller-service-2"},
					Bootstrap:    false,
					Priority:     5,
				},
			},
			expectedErr: false,
		},
		{
			name: "valid repeated priorities",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "new-bootstrap-service-2",
					Command:      "C:\\tnew-bootstrap-service-2",
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     1,
				},
			},
			expectedErr: false,
		},
		{
			name: "overlapping bootstrap and non-bootstrap priorities",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     0,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{"new-bootstrap-service"},
					Bootstrap:    false,
					Priority:     0,
				},
				{
					Name:         "test-controller-service-2",
					Command:      "C:\\test-controller-service-2",
					Dependencies: []string{"test-controller-service-2"},
					Bootstrap:    false,
					Priority:     5,
				},
			},
			expectedErr: true,
		},
		{
			name: "bootstrap lower priority than all non-bootstrap services",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     2,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     0,
				},
				{
					Name:         "test-controller-service-2",
					Command:      "C:\\test-controller-service-2",
					Dependencies: []string{"test-controller-service"},
					Bootstrap:    false,
					Priority:     1,
				},
			},
			expectedErr: true,
		},
		{
			name: "bootstrap in the middle of non-bootstrap services",
			input: &[]Service{
				{
					Name:    "new-bootstrap-service",
					Command: "C:\\new-service --variable-arg1=NODE_NAME --variable-arg2=NETWORK_IP",
					NodeVariablesInCommand: []NodeVariable{
						{
							Name:               "NODE_NAME",
							JsonPathNodeObject: "metadata.name",
						},
					},
					PowershellVariablesInCommand: []PowershellVariable{
						{
							Name: "NETWORK_IP",
							Path: "C:\\k\\scripts\\get_net_ip.ps",
						},
					},
					Dependencies: []string{},
					Bootstrap:    true,
					Priority:     1,
				},
				{
					Name:         "test-controller-service",
					Command:      "C:\\test-controller-service",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     0,
				},
				{
					Name:         "test-controller-service-2",
					Command:      "C:\\test-controller-service-2",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     2,
				},
			},
			expectedErr: true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			err := validatePriorities(test.input)
			if test.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
