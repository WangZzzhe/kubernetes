package qosresourcemanager

import (
	"context"
	"fmt"
	"github.com/opencontainers/selinux/go-selinux"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/pkg/apis/resourceplugin/v1alpha1"
	pluginapi "k8s.io/kubelet/pkg/apis/resourceplugin/v1alpha1"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager/errors"
	"k8s.io/kubernetes/pkg/kubelet/cm/qosresourcemanager/checkpoint"
	"k8s.io/kubernetes/pkg/kubelet/config"
	"k8s.io/kubernetes/pkg/kubelet/metrics"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type BasicImpl struct {
	ctx context.Context

	socketname string
	socketdir  string

	Endpoints map[string]endpointInfo // Key is ResourceName

	// lock when accesing Endpoints and allocatedScalarResourcesQuantity
	mutex sync.Mutex

	server *grpc.Server
	wg     sync.WaitGroup

	// contains allocated scalar resources quantity, keyed by resourceName.
	allocatedScalarResourcesQuantity map[string]float64

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

		Endpoints:                        make(map[string]endpointInfo),
		allocatedScalarResourcesQuantity: make(map[string]float64),

		podResources:      newPodResourcesChk(),
		resourceNamesMap:  resourceNamesMap,
		checkpointManager: checkpointManager,
	}

	return bi, nil
}

type endpointInfo struct {
	e    endpoint
	opts *pluginapi.ResourcePluginOptions
}

func (bi *BasicImpl) Start() error {
	err := bi.readCheckpoint()
	if err != nil {
		klog.Errorf("[basicresourcemanager] read checkpoint fail: %v", err)
		return err
	}

	socketPath := filepath.Join(bi.socketdir, bi.socketname)

	if err = os.MkdirAll(bi.socketdir, 0750); err != nil {
		return err
	}
	if selinux.GetEnabled() {
		if err := selinux.SetFileLabel(bi.socketdir, config.KubeletPluginsDirSELinuxLabel); err != nil {
			klog.Warningf("[qosresourcemanager] Unprivileged containerized plugins might not work. Could not set selinux context on %s: %v", bi.socketdir, err)
		}
	}

	// Removes all stale sockets in m.socketdir. Resource plugins can monitor
	// this and use it as a signal to re-register with the new Kubelet.
	if err := bi.removeContents(bi.socketdir); err != nil {
		klog.Errorf("[qosresourcemanager] Fail to clean up stale contents under %s: %v", bi.socketdir, err)
	}

	s, err := net.Listen("unix", socketPath)
	if err != nil {
		klog.Errorf(errListenSocket+" %v", err)
		return err
	}

	bi.wg.Add(1)
	bi.server = grpc.NewServer([]grpc.ServerOption{}...)

	pluginapi.RegisterRegistrationServer(bi.server, bi)

	ctx, cancel := context.WithCancel(context.Background())
	bi.ctx = ctx
	klog.V(2).Infof("[qosresourcemanager] Serving resource plugin registration server on %q", socketPath)
	go func() {
		defer func() {
			bi.wg.Done()
			cancel()

			if err := recover(); err != nil {
				klog.Fatalf("[qosresourcemanager] Start recover from err: %v", err)
			}
		}()
		bi.server.Serve(s)
	}()

	return nil
}

func (bi *BasicImpl) removeContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		filePath := filepath.Join(dir, name)
		if filePath == bi.checkpointFile() {
			continue
		}
		stat, err := os.Stat(filePath)
		if err != nil {
			klog.Errorf("[qosresourcemanager] Failed to stat file %s: %v", filePath, err)
			continue
		}
		if stat.IsDir() {
			continue
		}
		err = os.RemoveAll(filePath)
		if err != nil {
			errs = append(errs, err)
			klog.Errorf("[qosresourcemanager] Failed to remove file %s: %v", filePath, err)
			continue
		}
	}
	return errorsutil.NewAggregate(errs)
}

// ValidatePlugin validates a plugin if the version is correct and the name has the format of an extended resource
func (bi *BasicImpl) ValidatePlugin(pluginName string, endpoint string, versions []string) error {
	klog.V(2).Infof("Got Plugin %s at endpoint %s with versions %v", pluginName, endpoint, versions)

	if !bi.isVersionCompatibleWithPlugin(versions) {
		return fmt.Errorf("manager version, %s, is not among plugin supported versions %v", pluginapi.Version, versions)
	}

	return nil
}

// RegisterPlugin starts the endpoint and registers it
func (bi *BasicImpl) RegisterPlugin(pluginName string, endpoint string, versions []string) error {
	klog.V(2).Infof("[qosresourcemanager] Registering Plugin %s at endpoint %s", pluginName, endpoint)

	e, err := newEndpointImpl(endpoint, pluginName)
	if err != nil {
		return fmt.Errorf("[qosresourcemanager] failed to dial resource plugin with socketPath %s: %v", endpoint, err)
	}

	options, err := e.client.GetResourcePluginOptions(context.Background(), &pluginapi.Empty{})
	if err != nil {
		return fmt.Errorf("[qosresourcemanager] failed to get resource plugin options: %v", err)
	}

	bi.registerEndpoint(pluginName, options, e)

	return nil
}

// DeRegisterPlugin deregisters the plugin
// TODO work on the behavior for deregistering plugins
// e.g: Should we delete the resource
func (bi *BasicImpl) DeRegisterPlugin(pluginName string) {
	bi.mutex.Lock()
	defer bi.mutex.Unlock()

	if eI, ok := bi.Endpoints[pluginName]; ok {
		eI.e.stop()
	}
}

// Register registers a resource plugin.
func (bi *BasicImpl) Register(ctx context.Context, r *pluginapi.RegisterRequest) (*pluginapi.Empty, error) {
	klog.Infof("[qosresourcemanager] Got registration request from resource plugin with resource name %q", r.ResourceName)
	metrics.ResourcePluginRegistrationCount.WithLabelValues(r.ResourceName).Inc()
	var versionCompatible bool
	for _, v := range pluginapi.SupportedVersions {
		if r.Version == v {
			versionCompatible = true
			break
		}
	}
	if !versionCompatible {
		errorString := fmt.Sprintf(errUnsupportedVersion, r.Version, pluginapi.SupportedVersions)
		klog.Infof("Bad registration request from resource plugin with resource name %q: %s", r.ResourceName, errorString)
		return &pluginapi.Empty{}, fmt.Errorf(errorString)
	}

	// TODO: for now, always accepts newest resource plugin. Later may consider to
	// add some policies here, e.g., verify whether an old resource plugin with the
	// same resource name is still alive to determine whether we want to accept
	// the new registration.
	success := make(chan bool)
	go bi.addEndpoint(r, success)
	select {
	case pass := <-success:
		if pass {
			klog.Infof("[qosresourcemanager] Register resource plugin for %s success", r.ResourceName)
			return &pluginapi.Empty{}, nil
		}
		klog.Errorf("[qosresourcemanager] Register resource plugin for %s fail", r.ResourceName)
		return &pluginapi.Empty{}, fmt.Errorf("failed to register resource %s", r.ResourceName)
	case <-ctx.Done():
		klog.Errorf("[qosresourcemanager] Register resource plugin for %s timeout", r.ResourceName)
		return &pluginapi.Empty{}, fmt.Errorf("timeout to register resource %s", r.ResourceName)
	}
}

func (bi *BasicImpl) addEndpoint(r *pluginapi.RegisterRequest, success chan<- bool) {
	new, err := newEndpointImpl(filepath.Join(bi.socketdir, r.Endpoint), r.ResourceName)
	if err != nil {
		klog.Errorf("[qosresourcemanager] Failed to dial resource plugin with request %v: %v", r, err)
		success <- false
		return
	}
	bi.registerEndpoint(r.ResourceName, r.Options, new)
	success <- true
}

func (bi *BasicImpl) isVersionCompatibleWithPlugin(versions []string) bool {
	// TODO(sunjianyu): Currently this is fine as we only have a single supported version. When we do need to support
	// multiple versions in the future, we may need to extend this function to return a supported version.
	// E.g., say kubelet supports v1beta1 and v1beta2, and we get v1alpha1 and v1beta1 from a resource plugin,
	// this function should return v1beta1
	for _, version := range versions {
		for _, supportedVersion := range pluginapi.SupportedVersions {
			if version == supportedVersion {
				return true
			}
		}
	}
	return false
}

func (bi *BasicImpl) registerEndpoint(resourceName string, options *pluginapi.ResourcePluginOptions, e endpoint) {
	bi.mutex.Lock()
	defer bi.mutex.Unlock()

	old, ok := bi.Endpoints[resourceName]

	if ok && !old.e.isStopped() {
		klog.V(2).Infof("[qosresourcemanager] stop old endpoint: %v", old.e)
		old.e.stop()
	}

	bi.Endpoints[resourceName] = endpointInfo{e: e, opts: options}
	klog.V(2).Infof("[qosresourcemanager] Registered endpoint %v", e)
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

// Reads resource to container allocation information from disk, and populates
// m.allocatedScalarResourcesQuantity accordingly.
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

	allocatedScalarResourcesQuantity := bi.podResources.scalarResourcesQuantity()

	bi.mutex.Lock()
	bi.allocatedScalarResourcesQuantity = allocatedScalarResourcesQuantity

	allocatedResourceNames := bi.podResources.allAllocatedResourceNames()

	for _, allocatedResourceName := range allocatedResourceNames.UnsortedList() {
		bi.Endpoints[allocatedResourceName] = endpointInfo{e: newStoppedEndpointImpl(allocatedResourceName), opts: nil}
	}

	bi.mutex.Unlock()

	return nil
}

// checkpointFile returns resource plugin checkpoint file path.
func (bi *BasicImpl) checkpointFile() string {
	return filepath.Join(bi.socketdir, kubeletQoSResourceManagerCheckpoint)
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
