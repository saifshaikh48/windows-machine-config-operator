package ignition

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	ignCfg "github.com/coreos/ignition/v2/config/v3_2"
	ignCfgTypes "github.com/coreos/ignition/v2/config/v3_2/types"
	mcfg "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	"github.com/pkg/errors"
	"github.com/vincent-petithory/dataurl"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/windows-machine-config-operator/pkg/cluster"
	"github.com/openshift/windows-machine-config-operator/pkg/nodeconfig/payload"
)

//+kubebuilder:rbac:groups="machineconfiguration.openshift.io",resources=machineconfigs,verbs=list

const (
	// k8sDir is the remote kubernetes executable directory
	k8sDir = "C:\\k\\"
	// kubeletSystemdName is the name of the systemd service that the kubelet runs under,
	// this is used to parse the kubelet args
	kubeletSystemdName = "kubelet.service"
	// CloudConfigOption is the kubelet CLI option for the cloud configuration file
	CloudConfigOption = "cloud-config"
	// CloudProviderOption is the kubelet CLI option for cloud provider
	CloudProviderOption = "cloud-provider"
	// RenderedWorkerPrefix allows identification of the rendered worker MachineConfig, the combination of all worker
	// MachineConfigs.
	RenderedWorkerPrefix = "rendered-worker-"
)

var (
	// RenderedWorker holds the contents of the cluster's worker node ignition file
	RenderedWorker *Ignition
	//go:embed templates/kubelet_config.json
	baseConfig string
)

// Ignition is a representation of an Ignition file
type Ignition struct {
	config ignCfgTypes.Config
	// BootstrapFiles holds paths to all the files required to start the kubelet service
	BootstrapFiles []string
}

// kubeletConf holds the values to populate needed fields in the the base kubelet config file
type kubeletConf struct {
	// ClientCAFile specifies location to client certificate
	ClientCAFile string
	// ClusterDNS is the IP address of the DNS server used for all containers
	ClusterDNS string
}

// New returns a new instance of Ignition
func New(c client.Client, clusterServiceCIDR string) error {
	log := ctrl.Log.WithName("ignition")
	machineConfigs := &mcfg.MachineConfigList{}
	err := c.List(context.TODO(), machineConfigs)
	if err != nil {
		return err
	}
	renderedWorker, err := getLatestRenderedWorker(machineConfigs.Items)
	if err != nil {
		return err
	}

	configuration, report, err := ignCfg.Parse(renderedWorker.Spec.Config.Raw)
	if err != nil || report.IsFatal() {
		return errors.Errorf("failed to parse MachineConfig ignition: %v\nReport: %v", err, report)
	}
	RenderedWorker = &Ignition{
		config: configuration,
	}
	log.V(1).Info("parsed", "machineconfig", renderedWorker.Name, "ignition version", configuration.Ignition.Version)

	RenderedWorker.BootstrapFiles, err = setupBootstrapFiles(clusterServiceCIDR)
	return err
}

// setupBootstrapFiles creates all prerequisite files required to start kubelet and returns their paths
func setupBootstrapFiles(clusterServiceCIDR string) ([]string, error) {
	log := ctrl.Log.WithName("setupBootstrapFiles")
	log.Info("in setupBootstrapFiles")
	bootstrapFiles, err := createFilesFromIgnition()
	if err != nil {
		return nil, err
	}
	log.Info("executed GetFilesFromIgnition", "files", bootstrapFiles)

	clusterDNS, err := cluster.GetDNS(clusterServiceCIDR)
	if err != nil {
		return nil, err
	}
	initKubeletConfigPath, err := createKubeletConf(clusterDNS)
	if err != nil {
		return nil, err
	}
	log.Info("created kubelet.conf", "path", initKubeletConfigPath)
	bootstrapFiles = append(bootstrapFiles, initKubeletConfigPath)
	log.Info("added bootstrap files", "files to transfer", bootstrapFiles)
	return bootstrapFiles, nil
}

// createFilesFromIgnition creates files any it can from ignition: bootstrap kubeconfig, cloud-config, kubelet cert
func createFilesFromIgnition() ([]string, error) {
	// For each new file in the ignition file check if is a file we are interested in, if so, decode it
	// and write the contents to a temporary destination path
	filesToTranslate := map[string]struct{}{
		payload.BootstrapKubeconfigPath: {},
		payload.KubeletCACertPath:       {},
	}

	kubeletArgs, err := RenderedWorker.GetKubeletArgs()
	if err != nil {
		return nil, err
	}
	if _, ok := kubeletArgs[CloudConfigOption]; ok {
		filesToTranslate[payload.CloudConfigPath] = struct{}{}
	}

	if _, err := os.Stat(payload.GeneratedDir); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(payload.GeneratedDir, os.ModePerm)
		if err != nil {
			return nil, err
		}
	}
	filesFromIgnition := []string{}
	for _, ignFile := range RenderedWorker.config.Storage.Files {
		if _, ok := filesToTranslate[ignFile.Node.Path]; ok {
			if ignFile.Contents.Source == nil {
				return nil, errors.Errorf("could not process %s: File is empty", ignFile.Node.Path)
			}
			contents, err := dataurl.DecodeString(*ignFile.Contents.Source)
			if err != nil {
				return nil, errors.Wrapf(err, "could not decode %s", ignFile.Node.Path)
			}
			newContents := contents.Data
			dest := filepath.Join(payload.GeneratedDir, filepath.Base(ignFile.Node.Path))
			if err = os.WriteFile(dest, newContents, 0644); err != nil {
				return nil, fmt.Errorf("could not write to %s: %s", dest, err)
			}
			filesFromIgnition = append(filesFromIgnition, dest)
		}
	}
	return filesFromIgnition, nil
}

// createKubeletConf creates config file for kubelet, with Windows specific configuration
// Add values in kubelet_config.json files, for additional static fields.
// Add fields in kubeletConf struct for variable fields
func createKubeletConf(clusterDNS string) (string, error) {
	log := ctrl.Log.WithName("createKubeletConf")
	kubeletConfTmpl := template.New("kubeletconf")
	// Parse the template
	kubeletConfTmpl, err := kubeletConfTmpl.Parse(baseConfig)
	if err != nil {
		return "", err
	}
	// Fill up the config file, using kubeletConf struct
	variableFields := kubeletConf{
		ClientCAFile: strings.Join(append(strings.Split(k8sDir, `\`), `kubelet-ca.crt`), `\\`),
	}
	// check clusterDNS
	if clusterDNS != "" {
		// surround with double-quotes for valid JSON format
		variableFields.ClusterDNS = "\"" + clusterDNS + "\""
	}
	// Create kubelet.conf file
	if _, err := os.Stat(payload.GeneratedDir); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(payload.GeneratedDir, os.ModePerm)
		if err != nil {
			return "", err
		}
	}
	kubeletConfFile, err := os.Create(payload.KubeletConfigPath)
	defer kubeletConfFile.Close()
	if err != nil {
		return "", fmt.Errorf("error creating %s: %v", payload.KubeletConfigPath, err)
	}
	log.Info("created", "file", payload.KubeletConfigPath)
	err = kubeletConfTmpl.Execute(kubeletConfFile, variableFields)
	if err != nil {
		return "", fmt.Errorf("error writing data to %v file: %v", payload.KubeletConfigPath, err)
	}
	return kubeletConfFile.Name(), nil
}

// GetKubeletArgs returns a set of arguments for kubelet.exe, as specified in the ignition file
func (ign *Ignition) GetKubeletArgs() (map[string]string, error) {
	var kubeletUnit ignCfgTypes.Unit
	for _, unit := range ign.config.Systemd.Units {
		if unit.Name == kubeletSystemdName {
			kubeletUnit = unit
			break
		}
	}
	if kubeletUnit.Contents == nil {
		return nil, errors.Errorf("ignition missing kubelet systemd unit file")
	}
	argsFromIgnition, err := parseKubeletArgs(*kubeletUnit.Contents)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing kubelet systemd unit args")
	}
	return argsFromIgnition, nil
}

// getLatestRenderedWorker returns the most recently created rendered worker MachineConfig
func getLatestRenderedWorker(machineConfigs []mcfg.MachineConfig) (*mcfg.MachineConfig, error) {
	// Grab the latest rendered-worker MachineConfig by sorting the MachineConfig list by the latest creation
	// timestamp first.
	sort.Slice(machineConfigs, func(i, j int) bool {
		iTimestamp := machineConfigs[i].GetCreationTimestamp()
		jTimestamp := machineConfigs[j].GetCreationTimestamp()
		return jTimestamp.Before(&iTimestamp)
	})
	for _, mc := range machineConfigs {
		if strings.HasPrefix(mc.Name, RenderedWorkerPrefix) {
			if len(mc.Spec.Config.Raw) == 0 {
				continue
			}
			return &mc, nil
		}
	}
	return nil, errors.New("rendered worker MachineConfig not found")
}

// parseKubeletArgs parses a systemd unit file, returning the kubelet args WMCO is interested in
func parseKubeletArgs(unitContents string) (map[string]string, error) {
	// Remove everything before the ExecStart section of the unit file, which contains the command and args of the unit.
	// See unit test file for example systemd unit file
	execSplit := strings.SplitN(unitContents, "ExecStart=", 2)
	if len(execSplit) != 2 {
		return nil, fmt.Errorf("unit missing ExecStart")
	}
	// The ExecStart section is completed with a double newline, so using this as a split string, we can reduce the
	// scope of what we are looking at to everything inside the ExecStart section.
	cmdEndSplit := strings.SplitN(execSplit[1], "\n\n", 2)
	// Each part of the command is separated by an escaped newline
	argumentSplit := strings.Split(cmdEndSplit[0], "\\\n")
	kubeletArgs := make(map[string]string)
	// Skipping the first line, which indicates the binary, look at all the arguments which are key value pairs.
	// As WMCO currently is, we don't need to find any flags (--windows-service, for example), so we can ignore that
	// case. If there was a need for that, this logic would need to be expanded to cover that.
	windowsArgs := []string{CloudProviderOption, CloudConfigOption}
	for _, arg := range argumentSplit[1:] {
		arg = strings.TrimSpace(arg)
		arg = strings.TrimPrefix(arg, "--")
		keyValue := strings.SplitN(arg, "=", 2)
		if len(keyValue) != 2 {
			// Not a key value pair, continue
			continue
		}
		for _, windowsArg := range windowsArgs {
			if windowsArg == keyValue[0] {
				kubeletArgs[keyValue[0]] = keyValue[1]
			}
		}
	}
	return kubeletArgs, nil
}
