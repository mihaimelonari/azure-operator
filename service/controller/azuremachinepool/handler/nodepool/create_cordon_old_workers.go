package nodepool

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/coreos/go-semver/semver"
	apiextensionslabels "github.com/giantswarm/apiextensions/v3/pkg/label"
	"github.com/giantswarm/microerror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api-provider-azure/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/giantswarm/azure-operator/v5/pkg/handler/nodes/state"
	"github.com/giantswarm/azure-operator/v5/pkg/label"
	"github.com/giantswarm/azure-operator/v5/pkg/project"
	"github.com/giantswarm/azure-operator/v5/pkg/tenantcluster"
	"github.com/giantswarm/azure-operator/v5/service/controller/key"
)

const (
	// UnschedulablePatch is the JSON patch structure being applied to nodes using
	// a strategic merge patch in order to cordon them.
	UnschedulablePatch = `{"spec":{"unschedulable":true}}`
)

func (r *Resource) cordonOldWorkersTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	azureMachinePool, err := key.ToAzureMachinePool(obj)
	if err != nil {
		return DeploymentUninitialized, microerror.Mask(err)
	}

	cluster, err := util.GetClusterFromMetadata(ctx, r.CtrlClient, azureMachinePool.ObjectMeta)
	if err != nil {
		return DeploymentUninitialized, microerror.Mask(err)
	}

	if !cluster.GetDeletionTimestamp().IsZero() {
		r.Logger.Debugf(ctx, "Cluster is being deleted, skipping reconciling node pool")
		return currentState, nil
	}

	tenantClusterK8sClient, err := r.tenantClientFactory.GetClient(ctx, cluster)
	if tenantcluster.IsAPINotAvailableError(err) {
		r.Logger.Debugf(ctx, "tenant API not available yet")
		r.Logger.Debugf(ctx, "canceling resource")

		return currentState, nil
	} else if err != nil {
		return currentState, microerror.Mask(err)
	}

	r.Logger.Debugf(ctx, "finding all tenant cluster nodes")

	oldNodes, newNodes, err := r.sortNodesByTenantVMState(ctx, tenantClusterK8sClient, &azureMachinePool, key.NodePoolInstanceName)
	if err != nil {
		return currentState, microerror.Mask(err)
	}
	if len(newNodes) < len(oldNodes) {
		// Wait until there's enough new nodes up.
		r.Logger.Debugf(ctx, "number of new nodes (%d) is smaller than number of old nodes (%d)", len(newNodes), len(oldNodes))
		r.Logger.Debugf(ctx, "canceling resource")
		return currentState, nil
	}

	r.Logger.Debugf(ctx, "found %d old and %d new nodes from tenant cluster", len(oldNodes), len(newNodes))
	r.Logger.Debugf(ctx, "ensuring old nodes are cordoned")

	oldNodesCordoned, err := r.ensureNodesCordoned(ctx, tenantClusterK8sClient, oldNodes)
	if err != nil {
		return DeploymentUninitialized, microerror.Mask(err)
	}

	if oldNodesCordoned < len(oldNodes) {
		r.Logger.Debugf(ctx, "not all old nodes are still cordoned; %d pending", len(oldNodes)-oldNodesCordoned)

		return currentState, nil
	}

	r.Logger.Debugf(ctx, "ensured all old nodes (%d) are cordoned", oldNodesCordoned)

	return WaitForWorkersToBecomeReady, nil
}

// ensureNodesCordoned ensures that given tenant cluster nodes are cordoned.
func (r *Resource) ensureNodesCordoned(ctx context.Context, tenantClusterK8sClient ctrlclient.Client, nodes []corev1.Node) (int, error) {
	var count int
	for _, n := range nodes {
		// Node already cordoned?
		if n.Spec.Unschedulable {
			count++
			continue
		}

		err := tenantClusterK8sClient.Patch(context.Background(), &n, ctrlclient.RawPatch(types.StrategicMergePatchType, []byte(UnschedulablePatch)))
		if apierrors.IsNotFound(err) {
			// On manual operations or during auto-scaling it may happen that
			// node gets terminated while instances are processed. It's ok from
			// cordoning point of view since the node would get deleted later
			// anyway.
		} else if err != nil {
			return 0, microerror.Mask(err)
		}

		count++
	}

	return count, nil
}

func (r *Resource) sortNodesByTenantVMState(ctx context.Context, tenantClusterK8sClient ctrlclient.Client, azureMachinePool *v1alpha3.AzureMachinePool, instanceNameFunc func(nodePoolId, instanceID string) string) ([]corev1.Node, []corev1.Node, error) {
	virtualMachineScaleSetsClient, err := r.ClientFactory.GetVirtualMachineScaleSetsClient(ctx, azureMachinePool.ObjectMeta)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}

	virtualMachineScaleSetVMsClient, err := r.ClientFactory.GetVirtualMachineScaleSetVMsClient(ctx, azureMachinePool.ObjectMeta)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}

	vmss, err := virtualMachineScaleSetsClient.Get(ctx, key.ClusterID(azureMachinePool), key.NodePoolVMSSName(azureMachinePool))
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}

	var nodeList *corev1.NodeList
	{
		nodeList = &corev1.NodeList{}

		labelSelector := ctrlclient.MatchingLabels{apiextensionslabels.MachinePool: azureMachinePool.Name}
		err := tenantClusterK8sClient.List(ctx, nodeList, labelSelector)
		if err != nil {
			return nil, nil, microerror.Mask(err)
		}
	}

	var allWorkerInstances []compute.VirtualMachineScaleSetVM
	{
		r.Logger.Debugf(ctx, "finding all worker VMSS instances")

		allWorkerInstances, err = r.GetVMSSInstances(ctx, virtualMachineScaleSetVMsClient, key.ClusterID(azureMachinePool), key.NodePoolVMSSName(azureMachinePool))
		if err != nil {
			return nil, nil, microerror.Mask(err)
		}

		r.Logger.Debugf(ctx, "found %d worker VMSS instances", len(allWorkerInstances))
	}

	nodeMap := make(map[string]corev1.Node)
	for _, n := range nodeList.Items {
		nodeMap[n.GetName()] = n
	}

	var oldNodes []corev1.Node
	var newNodes []corev1.Node
	for _, i := range allWorkerInstances {
		name := instanceNameFunc(azureMachinePool.Name, *i.InstanceID)

		n, found := nodeMap[name]
		if !found {
			// When VMSS is scaling up there might be VM instances that haven't
			// registered as nodes in k8s yet. Hence not all instances are
			// found from node list.
			continue
		}

		outdated, err := r.isWorkerInstanceFromPreviousRelease(ctx, tenantClusterK8sClient, azureMachinePool.Name, i, vmss)
		if err != nil {
			return nil, nil, microerror.Mask(err)
		}
		if *outdated {
			oldNodes = append(oldNodes, n)
		} else {
			newNodes = append(newNodes, n)
		}
	}

	return oldNodes, newNodes, nil
}

func (r *Resource) getK8sWorkerNodeForInstance(ctx context.Context, tenantClusterK8sClient ctrlclient.Client, nodePoolId string, instance compute.VirtualMachineScaleSetVM) (*corev1.Node, error) {
	name := key.NodePoolInstanceName(nodePoolId, *instance.InstanceID)

	nodeList := &corev1.NodeList{}
	labelSelector := ctrlclient.MatchingLabels{apiextensionslabels.MachinePool: nodePoolId}
	err := tenantClusterK8sClient.List(ctx, nodeList, labelSelector)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	nodes := nodeList.Items
	for _, n := range nodes {
		if n.GetName() == name {
			return &n, nil
		}
	}

	// Node related to this instance was not found.
	return nil, nil
}

func (r *Resource) isWorkerInstanceFromPreviousRelease(ctx context.Context, tenantClusterK8sClient ctrlclient.Client, nodePoolId string, instance compute.VirtualMachineScaleSetVM, vmss compute.VirtualMachineScaleSet) (*bool, error) {
	t := true
	f := false

	n, err := r.getK8sWorkerNodeForInstance(ctx, tenantClusterK8sClient, nodePoolId, instance)
	if err != nil {
		return nil, err
	}

	if n == nil {
		// Kubernetes node related to this instance not found, we consider the node old.
		return &t, nil
	}

	myVersion := semver.New(project.Version())

	v, exists := n.GetLabels()[label.OperatorVersion]
	if !exists {
		// Label does not exist, this normally happens when a new node is coming up but did not finish
		// its kubernetes bootstrap yet and thus doesn't have all the needed labels.
		// We'll ignore this node for now and wait for it to bootstrap correctly.
		return nil, nil
	}

	nodeVersion := semver.New(v)
	if nodeVersion.LessThan(*myVersion) {
		return &t, nil
	} else {
		// Check if instance type is up to date.
		if *instance.Sku.Name != *vmss.Sku.Name {
			return &t, nil
		}
		return &f, nil
	}
}
