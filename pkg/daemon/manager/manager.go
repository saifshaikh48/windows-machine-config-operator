//go:build windows

package manager

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"k8s.io/klog/v2"

	"github.com/openshift/windows-machine-config-operator/pkg/daemon/winsvc"
)

type Manager interface {
	// CreateService creates a Windows service with the given configuration parameters
	CreateService(string, string, mgr.Config, ...string) (winsvc.Service, error)
	// GetServices returns a map of all the Windows services that exist on an instance.
	// The keys are service names and values are empty structs, used as 0 byte placeholders.
	GetServices() (map[string]struct{}, error)
	// ServiceExists checks if the given service is managed by this manager object
	ServiceExists(string) (bool, error)
	// OpenService gets the Windows service of the given name if it exists, by which it can be queried or controlled
	OpenService(string) (winsvc.Service, error)
	// DeleteService marks a Windows service of the given name for deletion. No-op if the service already doesn't exist
	DeleteService(string) error
	// DeleteServiceAndDependents deletes the given service and all services that depend on it.
	DeleteServiceAndDependents(string) error
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

func (m *manager) ServiceExists(name string) (bool, error) {
	services, err := m.GetServices()
	if err != nil {
		return false, err
	}
	_, ok := services[name]
	return ok, nil
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

func (m *manager) DeleteServiceAndDependents(name string) error {
	existingSvcs, err := m.GetServices()
	if err != nil {
		return errors.Wrap(err, "could not determine existing Windows services")
	}
	// Nothing to do if it already does not exist
	if _, present := existingSvcs[name]; !present {
		return nil
	}

	winSvcObj, err := m.OpenService(name)
	if err != nil {
		return errors.Wrapf(err, "unable to open %s service handler", name)
	}
	defer winSvcObj.Close()

	status, err := winSvcObj.Query()
	if err != nil {
		return errors.Wrapf(err, "unable to query %s service status", name)
	}
	if status.State != svc.Stopped {
		// Ensure no dependent services are running before we can safely stop this one
		if err := m.removeDependentServices(name, existingSvcs); err != nil {
			return err
		}
	}
	klog.Infof("removing service %s", name)
	return m.DeleteService(name)
}

// removeDependentServices stops and deletes all services that depend on the given service
func (m *manager) removeDependentServices(rootService string, allServices map[string]struct{}) error {
	for service := range allServices {
		exists, err := m.ServiceExists(service)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}

		existingWinSvcObj, err := m.OpenService(service)
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
			// Found an existing service that has this one as a dependency, process it if it's not already stopped
			status, err := existingWinSvcObj.Query()
			if err != nil {
				return errors.Wrapf(err, "error querying %s service status", service)
			}
			if status.State == svc.Stopped {
				continue
			}
			klog.Infof("processing service %s as it depends on %s", service, rootService)
			if err := m.DeleteServiceAndDependents(service); err != nil {
				return err
			}
			// After removing a service, need to update the existing services
			if allServices, err = m.GetServices(); err != nil {
				return errors.Wrap(err, "could not determine existing Windows services")
			}
		}
	}
	return nil
}

func New() (Manager, error) {
	newMgr, err := mgr.Connect()
	if err != nil {
		return nil, err
	}

	return (*manager)(newMgr), nil
}
