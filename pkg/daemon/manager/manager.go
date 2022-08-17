//go:build windows

package manager

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/winsvc"
)

type Manager interface {
	// CreateService creates a Windows service with the given configuration parameters
	CreateService(string, string, mgr.Config, ...string) (winsvc.Service, error)
	// GetServices returns a map of all the Windows services that exist on an instance.
	// The keys are service names and values are empty structs, used as 0 byte placeholders.
	GetServices() (map[string]struct{}, error)
	// OpenService gets the Windows service of the given name if it exists, by which it can be queried or controlled
	OpenService(string) (winsvc.Service, error)
	// DeleteService marks a Windows service of the given name for deletion. No-op if the service already doesn't exist
	DeleteService(string) error
}

// manager is defined as a way for us to redefine the function signatures of mgr.Mgr, so that they can fulfill
// the Mgr interface. When used directly, functions like mgr.Mgr's CreateService() returns a *mgr.Service type. This
// causes issues fitting it to the Mgr interface, even though *mgr.Service implements the Service interface. By
// using the manager wrapper functions, the underlying mgr.Mgr methods can be called, and then the *mgr.Service
// return values can be cast to the Service interface.
type manager mgr.Mgr

func (m *manager) CreateService(name, exepath string, config mgr.Config, args ...string) (winsvc.Service, error) {
	underlyingMgr := (*mgr.Mgr)(m)
	service, err := underlyingMgr.CreateService(name, exepath, config, args...)
	return winsvc.Service(service), err
}

func (m *manager) GetServices() (map[string]struct{}, error) {
	// The most reliable way to determine if a service exists or not is to do a 'list' API call. It is possible to
	// remove this call, and parse the error messages of a service 'open' API call, but I find that relying on human
	// readable errors could cause issues when providing compatibility across different versions of Windows.
	underlyingMgr := (*mgr.Mgr)(m)
	svcList, err := underlyingMgr.ListServices()
	if err != nil {
		return nil, err
	}
	svcs := make(map[string]struct{})
	for _, service := range svcList {
		svcs[service] = struct{}{}
	}
	return svcs, nil
}

func (m *manager) OpenService(name string) (winsvc.Service, error) {
	underlyingMgr := (*mgr.Mgr)(m)
	return underlyingMgr.OpenService(name)
}

func (m *manager) DeleteService(name string) error {
	existingSvcs, err := m.GetServices()
	if err != nil {
		return err
	}
	// Nothing to do if it already does not exist
	if _, present := existingSvcs[name]; !present {
		return nil
	}

	underlyingMgr := (*mgr.Mgr)(m)
	service, err := underlyingMgr.OpenService(name)
	if err != nil {
		return err
	}
	defer service.Close()
	// Ensure service is stopped before deleting
	if err := winsvc.EnsureServiceState(service, svc.Stopped); err != nil {
		return errors.Wrapf(err, "failed to stop service %s", name)
	}
	return service.Delete()
}

func New() (Manager, error) {
	newMgr, err := mgr.Connect()
	if err != nil {
		return nil, err
	}

	return (*manager)(newMgr), nil
}
