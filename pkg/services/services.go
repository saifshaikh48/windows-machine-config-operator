package services

import (
	"fmt"
	"path/filepath"
	"strings"

	config "github.com/openshift/api/config/v1"
	"github.com/pkg/errors"

	"github.com/openshift/windows-machine-config-operator/pkg/ignition"
	"github.com/openshift/windows-machine-config-operator/pkg/nodeconfig"
	"github.com/openshift/windows-machine-config-operator/pkg/servicescm"
	"github.com/openshift/windows-machine-config-operator/pkg/windows"
)

const (
	// See doc link for explanation of log levels:
	// https://docs.openshift.com/container-platform/latest/rest_api/editing-kubelet-log-level-verbosity.html#log-verbosity-descriptions_editing-kubelet-log-level-verbosity
	debugLogLevel    = "4"
	standardLogLevel = "2"
	// hostnameOverrideVarName is a variable that should be replaced with the value of the desired instance hostname
	hostnameOverrideVarName = "HOSTNAME_OVERRIDE"
	NodeIPVar               = "NODE_IP"
)

// GenerateManifest returns the expected state of the Windows service configmap. If debug is true, debug logging
// will be enabled for services that support it.
func GenerateManifest(kubeletArgsFromIgnition map[string]string, vxlanPort, platform string, debug bool) (*servicescm.Data, error) {
	kubeletConfiguration, err := getKubeletServiceConfiguration(kubeletArgsFromIgnition, debug, platform)
	if err != nil {
		return nil, errors.Wrap(err, "could not determine kubelet service configuration spec")
	}
	services := &[]servicescm.Service{{
		Name:                         windows.WindowsExporterServiceName,
		Command:                      windows.WindowsExporterServiceCommand,
		NodeVariablesInCommand:       nil,
		PowershellVariablesInCommand: nil,
		Dependencies:                 nil,
		Bootstrap:                    false,
		Priority:                     1,
	},
		kubeletConfiguration,
		hybridOverlayConfiguration(vxlanPort, debug),
		kubeProxyConfiguration(debug),
	}
	// TODO: All payload filenames and checksums must be added here https://issues.redhat.com/browse/WINC-847
	files := &[]servicescm.FileInfo{}
	return servicescm.NewData(services, files)
}

// hybridOverlayConfiguration returns the Service definition for hybrid-overlay
func hybridOverlayConfiguration(vxlanPort string, debug bool) servicescm.Service {
	hybridOverlayServiceCmd := fmt.Sprintf("%s --node NODE_NAME --k8s-kubeconfig %s --windows-service "+
		"--logfile "+"%shybrid-overlay.log", windows.HybridOverlayPath, windows.KubeconfigPath,
		windows.HybridOverlayLogDir)
	if len(vxlanPort) > 0 {
		hybridOverlayServiceCmd = fmt.Sprintf("%s --hybrid-overlay-vxlan-port %s", hybridOverlayServiceCmd, vxlanPort)
	}

	// check log level and increase hybrid-overlay verbosity if needed
	if debug {
		// append loglevel param using 5 for debug (default: 4)
		// See https://github.com/openshift/ovn-kubernetes/blob/master/go-controller/pkg/config/config.go#L736
		hybridOverlayServiceCmd = hybridOverlayServiceCmd + " --loglevel 5"
	}
	return servicescm.Service{
		Name:    windows.HybridOverlayServiceName,
		Command: hybridOverlayServiceCmd,
		NodeVariablesInCommand: []servicescm.NodeCmdArg{
			{
				Name:               "NODE_NAME",
				NodeObjectJsonPath: "{.metadata.name}",
			},
		},
		PowershellVariablesInCommand: nil,
		Dependencies:                 []string{windows.KubeletServiceName},
		Bootstrap:                    false,
		Priority:                     1,
	}
}

// kubeProxyConfiguration returns the Service definition for kube-proxy
func kubeProxyConfiguration(debug bool) servicescm.Service {
	sanitizedSubnetAnnotation := strings.ReplaceAll(nodeconfig.HybridOverlaySubnet, ".", "\\.")
	cmd := fmt.Sprintf("%s --windows-service --proxy-mode=kernelspace --feature-gates=WinOverlay=true "+
		"--hostname-override=NODE_NAME --kubeconfig=%s --cluster-cidr=NODE_SUBNET --log-dir=%s --logtostderr=false "+
		"--network-name=%s --source-vip=ENDPOINT_IP --enable-dsr=false", windows.KubeProxyPath, windows.KubeconfigPath,
		windows.KubeProxyLogDir, windows.OVNKubeOverlayNetwork)
	// Set log level
	cmd = fmt.Sprintf("%s %s", cmd, klogVerbosityArg(debug))
	return servicescm.Service{
		Name:    windows.KubeProxyServiceName,
		Command: cmd,
		NodeVariablesInCommand: []servicescm.NodeCmdArg{
			{
				Name:               "NODE_NAME",
				NodeObjectJsonPath: "{.metadata.name}",
			},
			{
				Name:               "NODE_SUBNET",
				NodeObjectJsonPath: fmt.Sprintf("{.metadata.annotations.%s}", sanitizedSubnetAnnotation),
			},
		},
		PowershellVariablesInCommand: []servicescm.PowershellCmdArg{{
			Name: "ENDPOINT_IP",
			Path: windows.NetworkConfScriptPath,
		}},
		Dependencies: []string{windows.HybridOverlayServiceName},
		Bootstrap:    false,
		Priority:     2,
	}
}

// getKubeletServiceConfiguration returns the Service definition for the kubelet
func getKubeletServiceConfiguration(argsFromIginition map[string]string, debug bool, platform string) (servicescm.Service, error) {
	kubeletArgs, err := generateKubeletArgs(argsFromIginition, debug)
	if err != nil {
		return servicescm.Service{}, err
	}
	var powershellVars []servicescm.PowershellCmdArg
	hostnameArg, hostnamePowershellVars := getKubeletHostnameOverride(platform)
	if hostnameArg != "" {
		kubeletArgs = append(kubeletArgs, hostnameArg)
		powershellVars = append(powershellVars, hostnamePowershellVars)
	}

	kubeletServiceCmd := windows.KubeletPath
	for _, arg := range kubeletArgs {
		kubeletServiceCmd += fmt.Sprintf(" %s", arg)
	}
	if platform == string(config.NonePlatformType) {
		// special case substitution handled in WICD itself
		kubeletServiceCmd = fmt.Sprintf("%s --node-ip=%s", kubeletServiceCmd, NodeIPVar)
	}
	return servicescm.Service{
		Name:                         windows.KubeletServiceName,
		Command:                      kubeletServiceCmd,
		Priority:                     0,
		Bootstrap:                    true,
		Dependencies:                 []string{windows.ContainerdServiceName},
		PowershellVariablesInCommand: powershellVars,
		NodeVariablesInCommand:       nil,
	}, nil
}

// generateKubeletArgs returns the kubelet args required during initial kubelet start up
func generateKubeletArgs(argsFromIgnition map[string]string, debug bool) ([]string, error) {
	containerdEndpointValue := "npipe://./pipe/containerd-containerd"
	certDirectory := "c:\\var\\lib\\kubelet\\pki\\"
	windowsTaints := "os=Windows:NoSchedule"
	kubeletArgs := []string{
		"--config=" + windows.KubeletConfigPath,
		"--bootstrap-kubeconfig=" + windows.BootstrapKubeconfig,
		"--kubeconfig=" + windows.KubeconfigPath,
		"--cert-dir=" + certDirectory,
		"--windows-service",
		"--logtostderr=false",
		"--log-file=" + windows.KubeletLog,
		// Registers the Kubelet with Windows specific taints so that linux pods won't get scheduled onto
		// Windows nodes.
		"--register-with-taints=" + windowsTaints,
		"--node-labels=" + nodeconfig.WindowsOSLabel,
		"--container-runtime=remote",
		"--container-runtime-endpoint=" + containerdEndpointValue,
		"--resolv-conf=",
		"--enforce-node-allocatable=",
	}

	kubeletArgs = append(kubeletArgs, klogVerbosityArg(debug))
	if cloudProvider, ok := argsFromIgnition[ignition.CloudProviderOption]; ok {
		kubeletArgs = append(kubeletArgs, fmt.Sprintf("--%s=%s", ignition.CloudProviderOption, cloudProvider))
	}
	if cloudConfigValue, ok := argsFromIgnition[ignition.CloudConfigOption]; ok {
		// cloud config is placed by WMCO in the c:\k directory with the same file name
		cloudConfigPath := windows.K8sDir + filepath.Base(cloudConfigValue)
		kubeletArgs = append(kubeletArgs, fmt.Sprintf("--%s=%s", ignition.CloudConfigOption, cloudConfigPath))
	}

	return kubeletArgs, nil
}

// klogVerbosityArg returns an argument to set the verbosity for any service that uses klog to log
func klogVerbosityArg(debug bool) string {
	if debug {
		return "--v=" + debugLogLevel
	} else {
		return "--v=" + standardLogLevel
	}
}

// getKubeletHostnameOverride returns the hostname override arg that should be used
func getKubeletHostnameOverride(platformType string) (string, servicescm.PowershellCmdArg) {
	platformType = strings.ToUpper(platformType)
	// define argument with fixed variable name
	hostnameOverrideArg := "--hostname-override=" + hostnameOverrideVarName
	switch platformType {
	case string(config.AWSPlatformType):
		return hostnameOverrideArg, getAWSMetadataHostnamePowershellCmdArg(hostnameOverrideVarName)
	case string(config.GCPPlatformType):
		return hostnameOverrideArg, getGCPMetadataHostnamePowershellCmdArg(hostnameOverrideVarName)
	default:
		return "", servicescm.PowershellCmdArg{}
	}
}

// getAWSMetadataHostnamePowershellCmdArg returns the PowershellCmdArg to resolve the hostname using the instance
// metadata service in AWS. Uses AWS Tools for PowerShell (https://docs.aws.amazon.com/powershell/latest/userguide/pstools-welcome.html)
func getAWSMetadataHostnamePowershellCmdArg(variableName string) servicescm.PowershellCmdArg {
	return servicescm.PowershellCmdArg{
		Name: variableName,
		Path: "Get-EC2InstanceMetadata -Category LocalHostname",
	}
}

// getGCPMetadataHostnamePowershellCmdArg returns the PowershellCmdArg to resolve the hostname using the instance
// metadata service in GCP. Uses GoogleCloud Tools for PowerShell (https://cloud.google.com/tools/powershell/docs/quickstart)
// getGCPMetadataHostname returns the GCP instance hostname from the metadata service.  GCP is an especial case,
// and the resulting hostname is truncated if the length is longer than 63 characters.
func getGCPMetadataHostnamePowershellCmdArg(variableName string) servicescm.PowershellCmdArg {
	return servicescm.PowershellCmdArg{
		Name: variableName,
		Path: `
# MAX_LENGTH is the maximum number of character allowed for the instance's hostname in GCP
$MAX_LENGTH = 63

# get hostname from the instance metadata service
$hostname=Get-GceMetadata -Path "instance/hostname"

# check hostname length
if ($hostname.Length -le $MAX_LENGTH) {
    # up to 63 characters is good, nothing to do!
    return $hostname
}

# find the index of first dot in the FQDN
$firstDotIndex=$hostname.IndexOf(".") 
if (($firstDotIndex -gt 0) -and ($firstDotIndex -le $MAX_LENGTH) ) {
    # and return first part of the FQDN
    return $hostname.Substring(0, $firstDotIndex)
}

# otherwise, return the first 63 characters of the hostname
return $hostname.Substring(0, $MAX_LENGTH)
`}
}
