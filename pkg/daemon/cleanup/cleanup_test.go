//go:build windows

package cleanup

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/fake"
	"github.com/openshift/windows-machine-config-operator/pkg/nodeconfig"
	"github.com/openshift/windows-machine-config-operator/pkg/servicescm"
	"github.com/openshift/windows-machine-config-operator/pkg/windows"
)

func TestExecute(t *testing.T) {
	testIO := []struct {
		name                      string
		existingServices          map[string]*fake.FakeService
		expectedRemovedServices   []string
		expectedRemainingServices []string
		preserveNode              bool
		servicesCMExists          bool
		configMapServices         []servicescm.Service
	}{
		// Tests with a valid services ConfigMap in the cluster
		{
			name:                      "No services",
			existingServices:          map[string]*fake.FakeService{},
			expectedRemovedServices:   []string{},
			expectedRemainingServices: []string{},
			preserveNode:              false,
			servicesCMExists:          true,
			configMapServices:         []servicescm.Service{},
		},
		{
			name:                      "Upgrade scenario with no services",
			existingServices:          map[string]*fake.FakeService{},
			expectedRemovedServices:   []string{},
			expectedRemainingServices: []string{},
			preserveNode:              true,
			servicesCMExists:          true,
			configMapServices:         []servicescm.Service{},
		},
		{
			name:                      "Defined service doesn't exist on node",
			existingServices:          map[string]*fake.FakeService{},
			expectedRemovedServices:   []string{"test1"},
			expectedRemainingServices: []string{},
			preserveNode:              false,
			servicesCMExists:          true,
			configMapServices: []servicescm.Service{
				{
					Name:    "test1",
					Command: "test1 --node-name=NODENAME",
					NodeVariablesInCommand: []servicescm.NodeCmdArg{{
						Name:               "NODENAME",
						NodeObjectJsonPath: "{.metadata.name}",
					}},
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     0,
				},
			},
		},
		{
			name: "Single service",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 --node-name=node",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1"},
			expectedRemainingServices: []string{},
			preserveNode:              false,
			servicesCMExists:          true,
			configMapServices: []servicescm.Service{
				{
					Name:    "test1",
					Command: "test1 --node-name=NODENAME",
					NodeVariablesInCommand: []servicescm.NodeCmdArg{{
						Name:               "NODENAME",
						NodeObjectJsonPath: "{.metadata.name}",
					}},
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     0,
				},
			},
		},
		{
			name: "Upgrade scenario with multiple services",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 arg1",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
				"test2": fake.NewFakeService(
					"test2",
					mgr.Config{
						BinaryPathName: "test2 arg1 arg2",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test2",
					},
					svc.Status{State: svc.Running},
				),
				"test3-unmanaged": fake.NewFakeService(
					"test3-unmanaged",
					mgr.Config{
						BinaryPathName: "test3-unmanaged",
						Dependencies:   []string{},
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1", "test2"},
			expectedRemainingServices: []string{"test3-unmanaged"},
			preserveNode:              true,
			servicesCMExists:          true,
			configMapServices: []servicescm.Service{
				{
					Name:         "test1",
					Command:      "test1 arg1",
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     0,
				},
				{
					Name:         "test2",
					Command:      "test2 arg1 arg2",
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     0,
				},
			},
		},
		{
			name: "Multiple services with only managed dependencies",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 arg1",
						Dependencies:   []string{"test2", "test4"},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
				"test2": fake.NewFakeService(
					"test2",
					mgr.Config{
						BinaryPathName: "test2 arg1 arg2",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test2",
					},
					svc.Status{State: svc.Running},
				),
				"test3": fake.NewFakeService(
					"test3",
					mgr.Config{
						BinaryPathName: "test3",
						Dependencies:   []string{"test4"},
						Description:    windows.ManagedTag + " test3",
					},
					svc.Status{State: svc.Running},
				),
				"test4": fake.NewFakeService(
					"test4",
					mgr.Config{
						BinaryPathName: "test4",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test4",
					},
					svc.Status{State: svc.Running},
				),
				"test5-unmanaged": fake.NewFakeService(
					"test5-unmanaged",
					mgr.Config{
						BinaryPathName: "test5-unmanaged",
						Dependencies:   []string{},
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1", "test5-umnanaged", "test3", "test4"},
			expectedRemainingServices: []string{"test2"},
			preserveNode:              false,
			servicesCMExists:          true,
			configMapServices: []servicescm.Service{
				{
					Name:         "test1",
					Command:      "test1 arg1",
					Dependencies: []string{"test3", "test4"},
					Bootstrap:    false,
					Priority:     3,
				},
				{
					Name:         "test3",
					Command:      "test3",
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     1,
				},
				{
					Name:         "test4",
					Command:      "test4",
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     0,
				},
				{
					Name:         "test5-unmanaged",
					Command:      "test5-unmanaged",
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     0,
				},
			},
		},
		{
			name: "Multiple services including unmanaged dependencies",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 arg1",
						Dependencies:   []string{"test2", "test3-unmanaged"},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
				"test2": fake.NewFakeService(
					"test2",
					mgr.Config{
						BinaryPathName: "test2 arg1 arg2",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test2",
					},
					svc.Status{State: svc.Running},
				),
				"test3-unmanaged": fake.NewFakeService(
					"test3-unmanaged",
					mgr.Config{
						BinaryPathName: "test3-unmanaged",
						Dependencies:   []string{},
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1", "test2", "test3-unmanaged"},
			expectedRemainingServices: []string{},
			preserveNode:              false,
			servicesCMExists:          true,
			configMapServices: []servicescm.Service{
				{
					Name:         "test1",
					Command:      "test1 arg1",
					Dependencies: []string{"test2", "test3-unmanaged"},
					Bootstrap:    false,
					Priority:     3,
				},
				{
					Name:         "test2",
					Command:      "test2 arg1 arg2",
					Dependencies: []string{},
					Bootstrap:    false,
					Priority:     2,
				},
				{
					Name:         "test3-unmanaged",
					Command:      "test3-unmanaged",
					Dependencies: nil,
					Bootstrap:    false,
					Priority:     0,
				},
			},
		},
		// Tests without desired services ConfigMap in the cluster
		{
			name:                      "No services, desired services CM not present",
			existingServices:          map[string]*fake.FakeService{},
			expectedRemovedServices:   []string{},
			expectedRemainingServices: []string{},
			preserveNode:              false,
			servicesCMExists:          false,
			configMapServices:         []servicescm.Service{},
		},
		{
			name:                      "Upgrade scenario with no services, desired services CM not present",
			existingServices:          map[string]*fake.FakeService{},
			expectedRemovedServices:   []string{},
			expectedRemainingServices: []string{},
			preserveNode:              true,
			servicesCMExists:          false,
			configMapServices:         []servicescm.Service{},
		},
		{
			name: "Single managed service, desired services CM not present",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 arg1",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1"},
			expectedRemainingServices: []string{},
			preserveNode:              false,
			servicesCMExists:          false,
			configMapServices:         []servicescm.Service{},
		},
		{
			name: "Single unmanaged service, desired services CM not present",
			existingServices: map[string]*fake.FakeService{
				"test3-unmanaged": fake.NewFakeService(
					"test3-unmanaged",
					mgr.Config{
						BinaryPathName: "test3-unmanaged arg1",
						Dependencies:   []string{},
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test3-unmanaged"},
			expectedRemainingServices: []string{},
			preserveNode:              false,
			servicesCMExists:          false,
			configMapServices:         []servicescm.Service{},
		},
		{
			name: "Upgrade scenario with multiple services, desired services CM not present",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 arg1",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
				"test2": fake.NewFakeService(
					"test2",
					mgr.Config{
						BinaryPathName: "test2 arg1 arg2",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test2",
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1", "test2"},
			expectedRemainingServices: []string{},
			preserveNode:              true,
			servicesCMExists:          false,
			configMapServices:         []servicescm.Service{},
		},
		{
			name: "Multiple services with only managed dependencies, desired services CM not present",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 arg1",
						Dependencies:   []string{"test2", "test4"},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
				"test2": fake.NewFakeService(
					"test2",
					mgr.Config{
						BinaryPathName: "test2 arg1 arg2",
						Dependencies:   []string{"test4"},
						Description:    windows.ManagedTag + " test2",
					},
					svc.Status{State: svc.Running},
				),
				"test3": fake.NewFakeService(
					"test3",
					mgr.Config{
						BinaryPathName: "test3",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test3",
					},
					svc.Status{State: svc.Stopped},
				),
				"test4": fake.NewFakeService(
					"test4",
					mgr.Config{
						BinaryPathName: "test4",
						Dependencies:   []string{},
						Description:    windows.ManagedTag + " test4",
					},
					svc.Status{State: svc.Running},
				),
				"test5-unmanaged": fake.NewFakeService(
					"test5-unmanaged",
					mgr.Config{
						BinaryPathName: "test5-unmanaged",
						Dependencies:   []string{},
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1", "test2", "test3", "test4"},
			expectedRemainingServices: []string{"test5-unmanaged"},
			preserveNode:              false,
			servicesCMExists:          false,
			configMapServices:         []servicescm.Service{},
		},
		{
			name: "Multiple services including unmanaged dependencies, desired services CM not present",
			existingServices: map[string]*fake.FakeService{
				"test1": fake.NewFakeService(
					"test1",
					mgr.Config{
						BinaryPathName: "test1 arg1",
						Dependencies:   []string{"test2"},
						Description:    windows.ManagedTag + " test1",
					},
					svc.Status{State: svc.Running},
				),
				"test2": fake.NewFakeService(
					"test2",
					mgr.Config{
						BinaryPathName: "test2 arg1 arg2",
						Dependencies:   []string{"test4-unmanaged"},
						Description:    windows.ManagedTag + " test2",
					},
					svc.Status{State: svc.Running},
				),
				"test3-unmanaged": fake.NewFakeService(
					"test3-unmanaged",
					mgr.Config{
						BinaryPathName: "test3-unmanaged",
						Dependencies:   []string{"test4-unmanaged"},
					},
					svc.Status{State: svc.Running},
				),
				"test4-unmanaged": fake.NewFakeService(
					"test4-unmanaged",
					mgr.Config{
						BinaryPathName: "test4-unmanaged",
						Dependencies:   []string{},
					},
					svc.Status{State: svc.Running},
				),
			},
			expectedRemovedServices:   []string{"test1", "test2", "test3-unmanaged"},
			expectedRemainingServices: []string{"test4-unmanaged"},
			preserveNode:              false,
			servicesCMExists:          false,
			configMapServices:         []servicescm.Service{},
		},
	}

	oldCM, err := servicescm.Generate(servicescm.NamePrefix+"old",
		"openshift-windows-machine-config-operator", &servicescm.Data{[]servicescm.Service{{
			Name: "test3-unmanaged", Command: "test3-unmanaged", Dependencies: nil, Bootstrap: false, Priority: 0}},
			[]servicescm.FileInfo{}})
	require.NoError(t, err)

	nodeIP := "192.168.10.10"
	testAddrs := []net.Addr{&net.IPNet{IP: net.ParseIP(nodeIP)}}

	for _, test := range testIO {
		t.Run(test.name, func(t *testing.T) {
			desiredVersion := "testversion"
			// This ConfigMap's name must match with the given Node object's desired-version annotation
			cm, err := servicescm.Generate(servicescm.NamePrefix+desiredVersion,
				"openshift-windows-machine-config-operator", &servicescm.Data{test.configMapServices,
					[]servicescm.FileInfo{}})
			require.NoError(t, err)
			clusterObjs := []client.Object{
				// This is the node object that will be used in these test cases
				&core.Node{
					ObjectMeta: meta.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							nodeconfig.DesiredVersionAnnotation: desiredVersion,
						},
					},
					Status: core.NodeStatus{
						Addresses: []core.NodeAddress{{Address: nodeIP}},
					},
				},
				oldCM,
			}
			if test.servicesCMExists {
				clusterObjs = append(clusterObjs, cm)
			}
			fakeClient := clientfake.NewClientBuilder().WithObjects(clusterObjs...).Build()

			winSvcMgr := fake.NewTestMgr(test.existingServices)
			err = Deconfigure(fakeClient, testAddrs, winSvcMgr, test.preserveNode)
			require.NoError(t, err)
			allServices, err := winSvcMgr.GetServices()
			require.NoError(t, err)

			for _, svc := range test.expectedRemovedServices {
				_, present := allServices[svc]
				assert.Falsef(t, present, "service %s not removed as expected", svc)
			}
			for _, svc := range test.expectedRemainingServices {
				_, present := allServices[svc]
				assert.Truef(t, present, "service %s not present as expected", svc)
			}
			// Depending on if the test is an upgrade cleanup case, check if the node object has been deleted or not
			nodes := &core.NodeList{}
			require.NoError(t, fakeClient.List(context.TODO(), nodes))
			if test.preserveNode {
				assert.NotEmpty(t, nodes.Items)
			} else {
				assert.Empty(t, nodes.Items)
			}
		})
	}
}
