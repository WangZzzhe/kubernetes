package qosresourcemanager

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/pkg/apis/resourceplugin/v1alpha1"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager/errors"
	"k8s.io/kubernetes/pkg/kubelet/cm/qosresourcemanager/checkpoint"
	"path/filepath"
)

type BasicImpl struct {
	socketname string
	socketdir  string

	// podResources contains pod to allocated resources mapping.
	podResources      *podResourcesChk
	checkpointManager checkpointmanager.CheckpointManager

	// Map of resource name "A" to resource name "B" during QoS Resource Manager allocation period.
	// It's useful for the same kind resource with different types. (eg. maps best-effort-cpu to cpu)
	resourceNamesMap map[string]string
}

func NewBasicImpl(socketPath string, resourceNamesMap map[string]string) (*BasicImpl, error) {

	if socketPath == "" || !filepath.IsAbs(socketPath) {
		return nil, fmt.Errorf(errBadSocket+" %s", socketPath)
	}

	dir, file := filepath.Split(socketPath)
	checkpointManager, err := checkpointmanager.NewCheckpointManager(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize checkpoint manager: %v", err)
	}

	bi := &BasicImpl{
		socketname: file,
		socketdir:  dir,

		podResources:      newPodResourcesChk(),
		resourceNamesMap:  resourceNamesMap,
		checkpointManager: checkpointManager,
	}

	return bi, nil
}

func (bi *BasicImpl) Start() error {
	err := bi.readCheckpoint()
	if err != nil {
		klog.Errorf("[basicresourcemanager] read checkpoint fail: %v", err)
		return err
	}

	return nil
}

// checkpointFile returns resource plugin checkpoint file path.
func (bi *BasicImpl) checkpointFile() string {
	return filepath.Join(bi.socketdir, kubeletQoSResourceManagerCheckpoint)
}

// Reads resource to container allocation information from disk
func (bi *BasicImpl) readCheckpoint() error {
	resEntries := make([]checkpoint.PodResourcesEntry, 0)
	cp := checkpoint.New(resEntries)
	err := bi.checkpointManager.GetCheckpoint(kubeletQoSResourceManagerCheckpoint, cp)
	if err != nil {
		if err == errors.ErrCheckpointNotFound {
			klog.Warningf("[basicresourcemanager] Failed to retrieve checkpoint for %q: %v", kubeletQoSResourceManagerCheckpoint, err)
			return nil
		}
		return err
	}

	podResources := cp.GetData()
	bi.podResources.fromCheckpointData(podResources)
	return nil
}

func (bi *BasicImpl) WriteCheckpoint() error {
	return bi.writeCheckpoint()
}

// Checkpoints resource to container allocation information to disk.
func (bi *BasicImpl) writeCheckpoint() error {
	data := checkpoint.New(bi.podResources.toCheckpointData())
	err := bi.checkpointManager.CreateCheckpoint(kubeletQoSResourceManagerCheckpoint, data)
	if err != nil {
		err2 := fmt.Errorf("[basicresourcemanager] failed to write checkpoint file %q: %v", kubeletQoSResourceManagerCheckpoint, err)
		klog.Warning(err2)
		return err2
	}
	return nil
}

func (bi *BasicImpl) UpdatePodResources(
	resourceAllocation map[string]*v1alpha1.ResourceAllocationInfo,
	pod *v1.Pod, container *v1.Container, resource string) {
	for accResourceName, allocationInfo := range resourceAllocation {
		if allocationInfo == nil {
			klog.Warningf("[basicresourcemanager] allocation request for resources %s - accompanying resource: %s for pod: %s/%s, container: %s got nil allocation infomation",
				resource, accResourceName, pod.Namespace, pod.Name, container.Name)
			continue
		}

		klog.V(4).Infof("[basicresourcemanager] allocation information for resources %s - accompanying resource: %s for pod: %s/%s, container: %s is %v",
			resource, accResourceName, pod.Namespace, pod.Name, container.Name, *allocationInfo)

		bi.podResources.insert(string(pod.UID), container.Name, accResourceName, allocationInfo)
	}
}

// getMappedResourceName returns mapped resource name of input "resourceName" in m.resourceNamesMap if there is the mapping entry,
// or it will return input "resourceName".
// If both the input "resourceName" and the mapped resource name are requested, it will return error.
func (bi *BasicImpl) getMappedResourceName(resourceName string, requests v1.ResourceList) (string, error) {
	if _, found := bi.resourceNamesMap[resourceName]; !found {
		return resourceName, nil
	}

	mappedResourceName := bi.resourceNamesMap[resourceName]

	_, foundReq := requests[v1.ResourceName(resourceName)]
	_, foundMappedReq := requests[v1.ResourceName(mappedResourceName)]

	if foundReq && foundMappedReq {
		return mappedResourceName, fmt.Errorf("both %s and mapped %s are requested", resourceName, mappedResourceName)
	}

	klog.Infof("[basicresourcemanager.getMappedResourceName] map resource name: %s to %s", resourceName, mappedResourceName)

	return mappedResourceName, nil
}

func (bi *BasicImpl) IsContainerRequestResource(container *v1.Container, resourceName string) (bool, error) {
	if container == nil {
		return false, nil
	}

	for k := range container.Resources.Requests {
		requestedResourceName, err := bi.getMappedResourceName(string(k), container.Resources.Requests)

		if err != nil {
			return false, err
		}

		if requestedResourceName == resourceName {
			return true, nil
		}
	}

	return false, nil
}

func (bi *BasicImpl) ContainerAllResources(podUID string, containerName string) ResourceAllocation {
	return bi.podResources.containerAllResources(podUID, containerName)
}

func (bi *BasicImpl) DeletePod(podUID string) {
	bi.podResources.deletePod(podUID)
}

func (bi *BasicImpl) Pods() sets.String {
	return bi.podResources.pods()
}
