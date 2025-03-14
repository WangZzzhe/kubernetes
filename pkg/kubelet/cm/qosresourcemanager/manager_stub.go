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

package qosresourcemanager

import (
	v1 "k8s.io/api/core/v1"
	pluginapi "k8s.io/kubelet/pkg/apis/resourceplugin/v1alpha1"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager"
	"k8s.io/kubernetes/pkg/kubelet/config"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/pkg/kubelet/lifecycle"
	"k8s.io/kubernetes/pkg/kubelet/pluginmanager/cache"
	"k8s.io/kubernetes/pkg/kubelet/status"
	schedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

// ManagerStub provides a simple stub implementation for the Resource Manager.
type ManagerStub struct{}

// NewManagerStub creates a ManagerStub.
func NewManagerStub() (Manager, error) {
	return &ManagerStub{}, nil
}

// Start simply returns nil.
func (h *ManagerStub) Start(activePods ActivePodsFunc, sourcesReady config.SourcesReady, podstatusprovider status.PodStatusProvider, containerRuntime runtimeService) error {
	return nil
}

// Stop simply returns nil.
func (h *ManagerStub) Stop() error {
	return nil
}

// Allocate simply returns nil.
func (h *ManagerStub) Allocate(pod *v1.Pod, container *v1.Container) error {
	return nil
}

// UpdatePluginResources simply returns nil.
func (h *ManagerStub) UpdatePluginResources(node *schedulerframework.NodeInfo, attrs *lifecycle.PodAdmitAttributes) error {
	return nil
}

// GetResourceRunContainerOptions simply returns nil, nil.
func (h *ManagerStub) GetResourceRunContainerOptions(pod *v1.Pod, container *v1.Container) (*kubecontainer.ResourceRunContainerOptions, error) {
	return nil, nil
}

// GetCapacity simply returns nil capacity and empty removed resource list.
func (h *ManagerStub) GetCapacity() (v1.ResourceList, v1.ResourceList, []string) {
	return nil, nil, []string{}
}

// GetWatcherHandler returns plugin watcher interface
func (h *ManagerStub) GetWatcherHandler() cache.PluginHandler {
	return nil
}

// GetTopologyHints returns an empty TopologyHint map
func (h *ManagerStub) GetTopologyHints(pod *v1.Pod, container *v1.Container) map[string][]topologymanager.TopologyHint {
	return map[string][]topologymanager.TopologyHint{}
}

// GetPodTopologyHints returns an empty TopologyHint map
func (h *ManagerStub) GetPodTopologyHints(pod *v1.Pod) map[string][]topologymanager.TopologyHint {
	return map[string][]topologymanager.TopologyHint{}
}

// ShouldResetExtendedResourceCapacity returns false
func (h *ManagerStub) ShouldResetExtendedResourceCapacity() bool {
	return false
}

// GetTopologyAwareResources returns nil, nil
func (h *ManagerStub) GetTopologyAwareResources(pod *v1.Pod, container *v1.Container) (*pluginapi.GetTopologyAwareResourcesResponse, error) {
	return nil, nil
}

// GetTopologyAwareAllocatableResources returns nil, nil
func (h *ManagerStub) GetTopologyAwareAllocatableResources() (*pluginapi.GetTopologyAwareAllocatableResourcesResponse, error) {
	return nil, nil
}

// UpdateAllocatedResources frees any resources that are bound to terminated pods.
func (h *ManagerStub) UpdateAllocatedResources() {
}
