/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics"
	"k8s.io/kubernetes/pkg/features"
	kubeletconfig "k8s.io/kubernetes/pkg/kubelet/apis/config"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"
	utiltaints "k8s.io/kubernetes/pkg/util/taints"
)

var (
	defaultCFSQuota = metav1.Duration{Duration: 100 * time.Millisecond}
)

// ValidateKubeletConfiguration validates `kc` and returns an error if it is invalid
func ValidateKubeletConfiguration(kc *kubeletconfig.KubeletConfiguration) error {
	allErrors := []error{}

	// Make a local copy of the global feature gates and combine it with the gates set by this configuration.
	// This allows us to validate the config against the set of gates it will actually run against.
	localFeatureGate := utilfeature.DefaultFeatureGate.DeepCopy()
	if err := localFeatureGate.SetFromMap(kc.FeatureGates); err != nil {
		return err
	}

	if kc.NodeLeaseDurationSeconds <= 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: nodeLeaseDurationSeconds must be greater than 0"))
	}
	if !kc.CgroupsPerQOS && len(kc.EnforceNodeAllocatable) > 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: enforceNodeAllocatable (--enforce-node-allocatable) is not supported unless cgroupsPerQOS (--cgroups-per-qos) is set to true"))
	}
	if kc.SystemCgroups != "" && kc.CgroupRoot == "" {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: systemCgroups (--system-cgroups) was specified and cgroupRoot (--cgroup-root) was not specified"))
	}
	if kc.EventBurst < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: eventBurst (--event-burst) %v must not be a negative number", kc.EventBurst))
	}
	if kc.EventRecordQPS < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: eventRecordQPS (--event-qps) %v must not be a negative number", kc.EventRecordQPS))
	}
	if kc.HealthzPort != 0 && utilvalidation.IsValidPortNum(int(kc.HealthzPort)) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: healthzPort (--healthz-port) %v must be between 1 and 65535, inclusive", kc.HealthzPort))
	}
	if !localFeatureGate.Enabled(features.CPUCFSQuotaPeriod) && kc.CPUCFSQuotaPeriod != defaultCFSQuota {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: cpuCFSQuotaPeriod (--cpu-cfs-quota-period) %v requires feature gate CustomCPUCFSQuotaPeriod", kc.CPUCFSQuotaPeriod))
	}
	if localFeatureGate.Enabled(features.CPUCFSQuotaPeriod) && utilvalidation.IsInRange(int(kc.CPUCFSQuotaPeriod.Duration), int(1*time.Microsecond), int(time.Second)) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: cpuCFSQuotaPeriod (--cpu-cfs-quota-period) %v must be between 1usec and 1sec, inclusive", kc.CPUCFSQuotaPeriod))
	}
	if utilvalidation.IsInRange(int(kc.ImageGCHighThresholdPercent), 0, 100) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: imageGCHighThresholdPercent (--image-gc-high-threshold) %v must be between 0 and 100, inclusive", kc.ImageGCHighThresholdPercent))
	}
	if utilvalidation.IsInRange(int(kc.ImageGCLowThresholdPercent), 0, 100) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: imageGCLowThresholdPercent (--image-gc-low-threshold) %v must be between 0 and 100, inclusive", kc.ImageGCLowThresholdPercent))
	}
	if kc.ImageGCLowThresholdPercent >= kc.ImageGCHighThresholdPercent {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: imageGCLowThresholdPercent (--image-gc-low-threshold) %v must be less than imageGCHighThresholdPercent (--image-gc-high-threshold) %v", kc.ImageGCLowThresholdPercent, kc.ImageGCHighThresholdPercent))
	}
	if utilvalidation.IsInRange(int(kc.IPTablesDropBit), 0, 31) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: iptablesDropBit (--iptables-drop-bit) %v must be between 0 and 31, inclusive", kc.IPTablesDropBit))
	}
	if utilvalidation.IsInRange(int(kc.IPTablesMasqueradeBit), 0, 31) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: iptablesMasqueradeBit (--iptables-masquerade-bit) %v must be between 0 and 31, inclusive", kc.IPTablesMasqueradeBit))
	}
	if kc.KubeAPIBurst < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: kubeAPIBurst (--kube-api-burst) %v must not be a negative number", kc.KubeAPIBurst))
	}
	if kc.KubeAPIQPS < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: kubeAPIQPS (--kube-api-qps) %v must not be a negative number", kc.KubeAPIQPS))
	}
	if kc.NodeStatusMaxImages < -1 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: nodeStatusMaxImages (--node-status-max-images) %v must be -1 or greater", kc.NodeStatusMaxImages))
	}
	if kc.MaxOpenFiles < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: maxOpenFiles (--max-open-files) %v must not be a negative number", kc.MaxOpenFiles))
	}
	if kc.MaxPods < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: maxPods (--max-pods) %v must not be a negative number", kc.MaxPods))
	}
	if utilvalidation.IsInRange(int(kc.OOMScoreAdj), -1000, 1000) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: oomScoreAdj (--oom-score-adj) %v must be between -1000 and 1000, inclusive", kc.OOMScoreAdj))
	}
	if kc.PodsPerCore < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: podsPerCore (--pods-per-core) %v must not be a negative number", kc.PodsPerCore))
	}
	if utilvalidation.IsValidPortNum(int(kc.Port)) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: port (--port) %v must be between 1 and 65535, inclusive", kc.Port))
	}
	if kc.ReadOnlyPort != 0 && utilvalidation.IsValidPortNum(int(kc.ReadOnlyPort)) != nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: readOnlyPort (--read-only-port) %v must be between 0 and 65535, inclusive", kc.ReadOnlyPort))
	}
	if kc.RegistryBurst < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: registryBurst (--registry-burst) %v must not be a negative number", kc.RegistryBurst))
	}
	if kc.RegistryPullQPS < 0 {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: registryPullQPS (--registry-qps) %v must not be a negative number", kc.RegistryPullQPS))
	}
	if kc.ServerTLSBootstrap && !localFeatureGate.Enabled(features.RotateKubeletServerCertificate) {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: serverTLSBootstrap %v requires feature gate RotateKubeletServerCertificate", kc.ServerTLSBootstrap))
	}
	if kc.TopologyManagerPolicy != kubeletconfig.NoneTopologyManagerPolicy && !localFeatureGate.Enabled(features.TopologyManager) {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: topologyManagerPolicy %v requires feature gate TopologyManager", kc.TopologyManagerPolicy))
	}

	for _, nodeTaint := range kc.RegisterWithTaints {
		if err := utiltaints.CheckTaintValidation(nodeTaint); err != nil {
			allErrors = append(allErrors, fmt.Errorf("invalid taint: %v", nodeTaint))
		}
		if nodeTaint.TimeAdded != nil {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: taint.TimeAdded is not nil"))
		}
	}

	switch kc.TopologyManagerPolicy {
	case kubeletconfig.NoneTopologyManagerPolicy:
	case kubeletconfig.BestEffortTopologyManagerPolicy:
	case kubeletconfig.RestrictedTopologyManagerPolicy:
	case kubeletconfig.SingleNumaNodeTopologyManagerPolicy:
	case kubeletconfig.NumericTopologyManagerPolicy:
	default:
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: topologyManagerPolicy (--topology-manager-policy) %q must be one of: %q", kc.TopologyManagerPolicy, []string{kubeletconfig.NoneTopologyManagerPolicy, kubeletconfig.BestEffortTopologyManagerPolicy, kubeletconfig.RestrictedTopologyManagerPolicy, kubeletconfig.SingleNumaNodeTopologyManagerPolicy, kubeletconfig.NumericTopologyManagerPolicy}))
	}
	if kc.TopologyManagerScope != kubeletconfig.ContainerTopologyManagerScope && !localFeatureGate.Enabled(features.TopologyManager) {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: topologyManagerScope %v requires feature gate TopologyManager", kc.TopologyManagerScope))
	}
	switch kc.TopologyManagerScope {
	case kubeletconfig.ContainerTopologyManagerScope:
	case kubeletconfig.PodTopologyManagerScope:
	default:
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: topologyManagerScope (--topology-manager-scope) %q must be one of: %q, or %q", kc.TopologyManagerScope, kubeletconfig.ContainerTopologyManagerScope, kubeletconfig.PodTopologyManagerScope))
	}

	if localFeatureGate.Enabled(features.GracefulNodeShutdown) {
		if kc.ShutdownGracePeriodCriticalPods.Duration > kc.ShutdownGracePeriod.Duration {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: shutdownGracePeriodCriticalPods %v must be <= shutdownGracePeriod %v", kc.ShutdownGracePeriodCriticalPods, kc.ShutdownGracePeriod))
		}
		if kc.ShutdownGracePeriod.Duration < 0 || (kc.ShutdownGracePeriod.Duration > 0 && kc.ShutdownGracePeriod.Duration < time.Second) {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: shutdownGracePeriod %v must be either zero or otherwise >= 1 sec", kc.ShutdownGracePeriod))
		}
		if kc.ShutdownGracePeriodCriticalPods.Duration < 0 || (kc.ShutdownGracePeriodCriticalPods.Duration > 0 && kc.ShutdownGracePeriodCriticalPods.Duration < time.Second) {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: shutdownGracePeriodCriticalPods %v must be either zero or otherwise >= 1 sec", kc.ShutdownGracePeriodCriticalPods))
		}
	}
	if (kc.ShutdownGracePeriod.Duration > 0 || kc.ShutdownGracePeriodCriticalPods.Duration > 0) && !localFeatureGate.Enabled(features.GracefulNodeShutdown) {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: specifying shutdownGracePeriod or shutdownGracePeriodCriticalPods requires feature gate GracefulNodeShutdown"))
	}
	if localFeatureGate.Enabled(features.GracefulNodeShutdownBasedOnPodPriority) {
		if len(kc.ShutdownGracePeriodByPodPriority) != 0 && (kc.ShutdownGracePeriod.Duration > 0 || kc.ShutdownGracePeriodCriticalPods.Duration > 0) {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: Cannot specify both shutdownGracePeriodByPodPriority and shutdownGracePeriod at the same time"))
		}
	}
	if !localFeatureGate.Enabled(features.GracefulNodeShutdownBasedOnPodPriority) {
		if len(kc.ShutdownGracePeriodByPodPriority) != 0 {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: Specifying shutdownGracePeriodByPodPriority requires feature gate GracefulNodeShutdownBasedOnPodPriority"))
		}
	}
	if localFeatureGate.Enabled(features.NodeSwap) {
		switch kc.MemorySwap.SwapBehavior {
		case "":
		case kubetypes.LimitedSwap:
		case kubetypes.UnlimitedSwap:
		default:
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: memorySwap.swapBehavior %q must be one of: \"\", %q, or %q", kc.MemorySwap.SwapBehavior, kubetypes.LimitedSwap, kubetypes.UnlimitedSwap))
		}
	}
	if !localFeatureGate.Enabled(features.NodeSwap) && kc.MemorySwap != (kubeletconfig.MemorySwapConfiguration{}) {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: memorySwap.swapBehavior cannot be set when NodeSwap feature flag is disabled"))
	}

	for _, val := range kc.EnforceNodeAllocatable {
		switch val {
		case kubetypes.NodeAllocatableEnforcementKey:
		case kubetypes.SystemReservedEnforcementKey:
			if kc.SystemReservedCgroup == "" {
				allErrors = append(allErrors, fmt.Errorf("invalid configuration: systemReservedCgroup (--system-reserved-cgroup) must be specified when %q contained in enforceNodeAllocatable (--enforce-node-allocatable)", kubetypes.SystemReservedEnforcementKey))
			}
		case kubetypes.KubeReservedEnforcementKey:
			if kc.KubeReservedCgroup == "" {
				allErrors = append(allErrors, fmt.Errorf("invalid configuration: kubeReservedCgroup (--kube-reserved-cgroup) must be specified when %q contained in enforceNodeAllocatable (--enforce-node-allocatable)", kubetypes.KubeReservedEnforcementKey))
			}
		case kubetypes.NodeAllocatableNoneKey:
			if len(kc.EnforceNodeAllocatable) > 1 {
				allErrors = append(allErrors, fmt.Errorf("invalid configuration: enforceNodeAllocatable (--enforce-node-allocatable) may not contain additional enforcements when %q is specified", kubetypes.NodeAllocatableNoneKey))
			}
		default:
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: option %q specified for enforceNodeAllocatable (--enforce-node-allocatable). Valid options are %q, %q, %q, or %q",
				val, kubetypes.NodeAllocatableEnforcementKey, kubetypes.SystemReservedEnforcementKey, kubetypes.KubeReservedEnforcementKey, kubetypes.NodeAllocatableNoneKey))
		}
	}
	switch kc.HairpinMode {
	case kubeletconfig.HairpinNone:
	case kubeletconfig.HairpinVeth:
	case kubeletconfig.PromiscuousBridge:
	default:
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: option %q specified for hairpinMode (--hairpin-mode). Valid options are %q, %q or %q",
			kc.HairpinMode, kubeletconfig.HairpinNone, kubeletconfig.HairpinVeth, kubeletconfig.PromiscuousBridge))
	}
	if kc.ReservedSystemCPUs != "" {
		// --reserved-cpus does not support --system-reserved-cgroup or --kube-reserved-cgroup
		if kc.SystemReservedCgroup != "" || kc.KubeReservedCgroup != "" {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: can't use reservedSystemCPUs (--reserved-cpus) with systemReservedCgroup (--system-reserved-cgroup) or kubeReservedCgroup (--kube-reserved-cgroup)"))
		}
		if _, err := cpuset.Parse(kc.ReservedSystemCPUs); err != nil {
			allErrors = append(allErrors, fmt.Errorf("invalid configuration: unable to parse reservedSystemCPUs (--reserved-cpus) %v, error: %w", kc.ReservedSystemCPUs, err))
		}
	}

	allErrors = append(allErrors, validateReservedMemoryConfiguration(kc)...)

	if err := validateKubeletOSConfiguration(kc); err != nil {
		allErrors = append(allErrors, err)
	}
	allErrors = append(allErrors, metrics.ValidateShowHiddenMetricsVersion(kc.ShowHiddenMetricsForVersion)...)

	if errs := logs.ValidateLoggingConfiguration(&kc.Logging, field.NewPath("logging")); len(errs) > 0 {
		allErrors = append(allErrors, errs.ToAggregate().Errors()...)
	}

	if localFeatureGate.Enabled(features.MemoryQoS) && kc.MemoryThrottlingFactor == nil {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: memoryThrottlingFactor is required when MemoryQoS feature flag is enabled"))
	}
	if kc.MemoryThrottlingFactor != nil && (*kc.MemoryThrottlingFactor <= 0 || *kc.MemoryThrottlingFactor > 1.0) {
		allErrors = append(allErrors, fmt.Errorf("invalid configuration: memoryThrottlingFactor %v must be greater than 0 and less than or equal to 1.0", *kc.MemoryThrottlingFactor))
	}

	return utilerrors.NewAggregate(allErrors)
}
