//go:build windows

package fake

import (
	"fmt"
	"sync"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/winsvc"
)

// fakeServiceList mocks out the state of all services on a Windows instance
type fakeServiceList struct {
	m    *sync.Mutex
	svcs map[string]winsvc.Service
}

// write overwrites the given service to the svcs map
func (l *fakeServiceList) write(name string, svc winsvc.Service) {
	l.m.Lock()
	defer l.m.Unlock()
	l.svcs[name] = svc
}

// read returns the entry with the given name, and a bool indicating if it exists or not
func (l *fakeServiceList) read(name string) (winsvc.Service, bool) {
	l.m.Lock()
	defer l.m.Unlock()
	service, exists := l.svcs[name]
	return service, exists
}

// listServiceNames returns a slice of all service names
func (l *fakeServiceList) listServiceNames() []string {
	l.m.Lock()
	defer l.m.Unlock()
	var names []string
	for svcName := range l.svcs {
		names = append(names, svcName)
	}
	return names
}

// remove deletes the entry with the given name, throwing an error if it doesn't exist
func (l *fakeServiceList) remove(name string) error {
	l.m.Lock()
	defer l.m.Unlock()
	_, exists := l.svcs[name]
	if !exists {
		return errors.New("service does not exist")
	}
	delete(l.svcs, name)
	return nil
}

func newFakeServiceList() *fakeServiceList {
	return &fakeServiceList{
		m:    &sync.Mutex{},
		svcs: make(map[string]winsvc.Service),
	}
}

type testMgr struct {
	svcList *fakeServiceList
}

// CreateService installs new service name on the system.
// The service will be executed by running exepath binary.
// Use config c to specify service parameters.
// Any args will be passed as command-line arguments when
// the service is started; these arguments are distinct from
// the arguments passed to Service.Start or via the "Start
// parameters" field in the service's Properties dialog box.
func (t *testMgr) CreateService(name, exepath string, config mgr.Config, args ...string) (winsvc.Service, error) {
	// Throw an error if the service already exists
	if _, ok := t.svcList.read(name); ok {
		return nil, errors.New("service already exists")
	}
	config.BinaryPathName = exepath
	service := FakeService{
		name:   name,
		config: config,
		status: svc.Status{
			State: svc.Stopped,
		},
		serviceList: t.svcList,
	}
	t.svcList.write(name, &service)
	return &service, nil
}

func (t *testMgr) GetServices() (map[string]struct{}, error) {
	svcsList := t.svcList.listServiceNames()
	svcsMap := make(map[string]struct{})
	for _, svc := range svcsList {
		svcsMap[svc] = struct{}{}
	}
	return svcsMap, nil
}

func (t *testMgr) ServiceExists(name string) (bool, error) {
	services, err := t.GetServices()
	if err != nil {
		return false, err
	}
	_, ok := services[name]
	return ok, nil
}

func (t *testMgr) OpenService(name string) (winsvc.Service, error) {
	service, exists := t.svcList.read(name)
	if !exists {
		return nil, fmt.Errorf("service does not exist")
	}
	return service, nil
}

func (t *testMgr) DeleteService(name string) error {
	winSvc, exists := t.svcList.read(name)
	if !exists {
		// Nothing to do if it already does not exist
		return nil
	}
	// Ensure service is stopped before deleting
	if err := winsvc.EnsureServiceState(winSvc, svc.Stopped); err != nil {
		return errors.Wrapf(err, "failed to stop service %s", name)
	}
	return t.svcList.remove(name)
}

func (t *testMgr) DeleteServiceAndDependents(name string) error {
	existingSvcs, err := t.GetServices()
	if err != nil {
		return errors.Wrap(err, "could not determine existing Windows services")
	}
	// Nothing to do if it already does not exist
	if _, present := existingSvcs[name]; !present {
		return nil
	}

	winSvcObj, err := t.OpenService(name)
	if err != nil {
		return errors.Wrapf(err, "unable to open %s service handler", name)
	}
	defer winSvcObj.Close()

	status, err := winSvcObj.Query()
	if err != nil {
		return errors.Wrapf(err, "unable to query %s service status", name)
	}
	// Ensure no dependent services are running before we can safely stop this one
	if status.State != svc.Stopped {
		if err := t.removeDependentServices(name, existingSvcs); err != nil {
			return err
		}
	}
	return t.DeleteService(name)
}

// removeDependentServices stops and deletes all services that depend on the given service
func (t *testMgr) removeDependentServices(rootService string, allServices map[string]struct{}) error {
	for service := range allServices {
		// Re-query existing services to avoid processing one that has already been removed
		exists, err := t.ServiceExists(service)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}

		existingWinSvcObj, err := t.OpenService(service)
		if err != nil {
			return errors.Wrapf(err, "error opening %s service handler", service)
		}
		existingWinSvcConfig, err := existingWinSvcObj.Config()
		if err != nil {
			return errors.Wrapf(err, "error getting configuration for service %s", service)
		}
		for _, dependency := range existingWinSvcConfig.Dependencies {
			if dependency != rootService {
				continue
			}
			// Found an existing service that has the root one as a dependency, process it if it's not already stopped
			status, err := existingWinSvcObj.Query()
			if err != nil {
				return errors.Wrapf(err, "error querying %s service status", service)
			}
			if status.State == svc.Stopped {
				continue
			}
			if err = t.DeleteServiceAndDependents(service); err != nil {
				return err
			}
		}
	}
	return nil
}

func NewTestMgr(existingServices map[string]*FakeService) *testMgr {
	testMgr := &testMgr{newFakeServiceList()}
	if existingServices != nil {
		for name, svc := range existingServices {
			svc.serviceList = testMgr.svcList
			testMgr.svcList.svcs[name] = svc
		}
	}
	return testMgr
}
