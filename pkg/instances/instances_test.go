package instances

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHosts(t *testing.T) {
	testCases := []struct {
		name        string
		input       map[string]string
		expectedOut []*InstanceInfo
		expectedErr bool
	}{
		{
			name:        "invalid username",
			input:       map[string]string{"localhost": "notusername=core"},
			expectedOut: nil,
			expectedErr: true,
		},
		{
			name:        "invalid DNS address",
			input:       map[string]string{"notlocalhost": "username=core"},
			expectedOut: nil,
			expectedErr: true,
		},
		{
			name:        "invalid username and DNS",
			input:       map[string]string{"invalid": "invalid"},
			expectedOut: nil,
			expectedErr: true,
		},
		{
			name:        "valid ipv6 address",
			input:       map[string]string{"::1": "username=core"},
			expectedOut: nil,
			expectedErr: true,
		},
		{
			name:        "valid dns address",
			input:       map[string]string{"localhost": "username=core"},
			expectedOut: []*InstanceInfo{{Address: "localhost", Username: "core"}},
			expectedErr: false,
		},
		{
			name:        "valid ip address",
			input:       map[string]string{"127.0.0.1": "username=core"},
			expectedOut: []*InstanceInfo{{Address: "127.0.0.1", Username: "core"}},
			expectedErr: false,
		},
		{
			name:        "valid dns and ip addresses",
			input:       map[string]string{"localhost": "username=core", "127.0.0.1": "username=Admin"},
			expectedOut: []*InstanceInfo{{Address: "localhost", Username: "core"}, {Address: "127.0.0.1", Username: "Admin"}},
			expectedErr: false,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			out, err := ParseHosts(test.input)
			if test.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.ElementsMatch(t, test.expectedOut, out)
		})
	}
}
