package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
	//"strings"
	//"os/exec"
	//"bytes"


	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/kubernetes/scheme"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	infrav1exp "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterv1exp "sigs.k8s.io/cluster-api/exp/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"github.com/Azure/go-autorest/autorest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kubedrain "k8s.io/kubectl/pkg/drain" // go look at - https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/v1.9.2/azure/scope/machinepoolmachine.go#L372 for how to edit it
	"sigs.k8s.io/cluster-api/controllers/remote"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	//"k8s.io/client-go/tools/remotecommand"
)

type writer struct {
	logFunc func(args ...interface{})
}

// Write passes string(p) into writer's logFunc and always returns len(p).
func (w writer) Write(p []byte) (n int, err error) {
	w.logFunc(string(p))
	return len(p), nil
}

func waitForPodRunning(clientset *kubernetes.Clientset, namespace, name string) (*corev1.Pod, error) {
	ctx := context.TODO()

	timeout := 5 * time.Minute
	deadline := time.Now().Add(timeout)

	for {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		if pod.Status.Phase == corev1.PodRunning {
			return pod, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for pod to reach Running phase")
		}

		time.Sleep(5 * time.Second)
	}
}

// Generated from example definition: https://github.com/Azure/azure-rest-api-specs/blob/5d2adf9b7fda669b4a2538c65e937ee74fe3f966/specification/compute/resource-manager/Microsoft.Compute/GalleryRP/stable/2022-03-03/examples/sharedGalleryExamples/SharedGallery_Get.json
func main() {
	config, err := clientcmd.BuildConfigFromFlags("", "/home/bennystream/.kube/config")
	if err != nil {
		panic(err)
	}

	s := scheme.Scheme
	infrav1exp.AddToScheme(s)
	infrav1.AddToScheme(s)
	clusterv1exp.AddToScheme(s)
	clusterv1.AddToScheme(s)

	c, err := client.New(config, client.Options{Scheme: s})
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	machinePoolName := "machinepool-25514-mp-0"

	resourceGroupName := "capi-quickstart"

	mp := &clusterv1exp.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      machinePoolName,
		},
	}

	amp := &infrav1exp.AzureMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      machinePoolName,
		},
	}
	err = c.Get(ctx, client.ObjectKeyFromObject(mp), mp)
	if err != nil {
		panic(err)
	}

	err = c.Get(ctx, client.ObjectKeyFromObject(amp), amp)
	if err != nil {
		panic(err)
	}

	replicaCount := amp.Status.Replicas

	healthyAmpm := &infrav1exp.AzureMachinePoolMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "-1",
		},
	}

	for i := 0; i < int(replicaCount); i++ { // step 1
		healthyAmpm = &infrav1exp.AzureMachinePoolMachine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      machinePoolName + "-" + strconv.Itoa(i),
			},
		}
		err = c.Get(ctx, client.ObjectKeyFromObject(healthyAmpm), healthyAmpm)
		if err != nil {
			panic(err)
		}	
	}
	
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("failed to obtain a credential: %v", err)
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "machinepool-25514"},
	}

	clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
		AzureClients: scope.AzureClients{
			Authorizer: autorest.NullAuthorizer{},
		},
		Client:  c,
		Cluster: cluster,
		AzureCluster: &infrav1.AzureCluster{
			Spec: infrav1.AzureClusterSpec{
				AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
					Location:       os.Getenv("AZURE_LOCATION"),
					SubscriptionID: os.Getenv("AZURE_SUBSCRIPTION_ID"),
				},
				ResourceGroup: "machinepool-25514",
				NetworkSpec: infrav1.NetworkSpec{
					Vnet: infrav1.VnetSpec{Name: resourceGroupName + "-vnet", ResourceGroup: "machinepool-25514"},
				},
			},
		},
	})

	myscope, err := scope.NewMachinePoolMachineScope(scope.MachinePoolMachineScopeParams{
		Client:                  c,
		MachinePool:             mp,
		AzureMachinePool:        amp,
		AzureMachinePoolMachine: healthyAmpm,
		ClusterScope:            clusterScope,
	})

	err = myscope.CordonAndDrain(ctx) // step 2
	if err != nil {
		log.Fatalf("failed to drain: %v", err)
	}

	snapshotFactory, err := armcompute.NewSnapshotsClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create snapshotFactory: %v", err)
	}

	_ , error := snapshotFactory.BeginCreateOrUpdate(ctx, resourceGroupName, "example-snapshot", armcompute.Snapshot{ // step 3
		Location: to.Ptr("East US"),
		Properties: &armcompute.SnapshotProperties{
			CreationData: &armcompute.CreationData{
				CreateOption: to.Ptr(armcompute.DiskCreateOptionCopy),
				SourceURI:    to.Ptr("/subscriptions/addeefcb-5be9-41a9-91d6-3307915e1428/resourceGroups/" + resourceGroupName + "/providers/Microsoft.Compute/disks/capi-quickstart-control-plane-8h9dd_OSDisk"),
			},
		},
	}, nil)

	if error != nil {
		log.Fatalf("failed to create snapshot: %v", error)
	}

	node, found, err := myscope.GetNode(ctx)
	if err != nil {
		log.Fatalf("failed to find node: %v", err)
	} else if !found {
		log.Fatalf("failed to find node with the ProviderID")
	}

	MachinePoolMachineScopeName := "azuremachinepoolmachine-scope"

	restConfig, err := remote.RESTConfig(ctx, MachinePoolMachineScopeName, c, client.ObjectKey{
		Name:      myscope.ClusterName(),
		Namespace: myscope.AzureMachinePoolMachine.Namespace,
	})

	if err != nil {
		log.Fatalf("Error creating a remote client while deleting Machine, won't retry: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	
	if err != nil {
		log.Fatalf("Error creating a remote client while deleting Machine, won't retry: %v", err)
	}
	

	var nodeAddress string
	for _, address := range node.Status.Addresses {
    	if address.Type == corev1.NodeInternalIP || address.Type == corev1.NodeHostName {
        	nodeAddress = address.Address
        	break
    	}
	}

	if nodeAddress == "" {
    	panic("Failed to retrieve the node's address")
	}

	fmt.Println(nodeAddress)

	_ = nodeAddress

	get_sleep := "apk add --no-cache coreutils"
	sleepy := "sleep 4"
	cat_test := "cat /etc/hostname && echo \"\" > /etc/hostname"
	rm_command := "/bin/rm -rf /var/lib/cloud/data/* /var/lib/cloud/instance /var/lib/cloud/instances/* /var/lib/waagent/history/* /var/lib/waagent/events/* /var/log/journal/*"
	replace_machine_id_command := "/bin/cp /dev/null /etc/machine-id"
	command := []string{"sh", "-c", get_sleep + " && " + sleepy + " && " + cat_test + " && " + rm_command + " && " + replace_machine_id_command + " && " + cat_test}
	//command := []string{"sh", "-c", cat_test}


	runAsUser := int64(0)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "exec-pod-",
		},
		Spec: corev1.PodSpec{
			NodeName:   node.GetName(),
			Containers: []corev1.Container{
				{
					Name:  "exec-container",
					Image: "alpine:latest", // Replace with your desired image
					Command: command,
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: &runAsUser, // Run as root user
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	
	createdPod, err := kubeClient.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}
	_ = createdPod
	/*
	waitForPodRunning(kubeClient, createdPod.Namespace, createdPod.Name)
	

	req := kubeClient.CoreV1().RESTClient().Post().Resource("pods").Name(pod.GetName()).
		Namespace(createdPod.GetNamespace()).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command: command,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     true,
	}
	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		panic("AHH")
	}

	var stdout, stderr bytes.Buffer

	exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	fmt.Println("Output:", stdout.String())
	fmt.Println("Error:", stderr.String())
	*/
	drainer := &kubedrain.Helper{
		Client:              kubeClient,
		Ctx:                 ctx,
		Force:               true,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		GracePeriodSeconds:  -1,
		Timeout: 20 * time.Second,
		OnPodDeletedOrEvicted: func(pod *corev1.Pod, usingEviction bool) {
			usingEviction = false
		},
		Out:    writer{klog.Info},
		ErrOut: writer{klog.Error},
	}
	
	if err := kubedrain.RunCordonOrUncordon(drainer, node, false); err != nil { // step 4
		fmt.Println("Failed to uncordon")
	}

	galleryLocation := os.Getenv("AZURE_LOCATION")
	galleryName := "GalleryInstantiation1"

	gallery := armcompute.Gallery{
		Location: &galleryLocation,
	}
	
	galleryFactory, err := armcompute.NewGalleriesClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create gallery: %v", err)
	}

	galleryFactory.BeginCreateOrUpdate(ctx, resourceGroupName, galleryName, gallery, nil)

	galleryImageFactory, err := armcompute.NewGalleryImagesClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create galleryImageFactory: %v", err)
	}
	
	_ , error = galleryImageFactory.BeginCreateOrUpdate(ctx, resourceGroupName, galleryName, "myGalleryImage", armcompute.GalleryImage{
		Location: to.Ptr(os.Getenv("AZURE_LOCATION")),
		Properties: &armcompute.GalleryImageProperties{
			HyperVGeneration: to.Ptr(armcompute.HyperVGenerationV1),
			Identifier: &armcompute.GalleryImageIdentifier{
				Offer:     to.Ptr("myOfferName"),
				Publisher: to.Ptr("myPublisherName"),
				SKU:       to.Ptr("mySkuName"),
			},
			OSState: to.Ptr(armcompute.OperatingSystemStateTypesGeneralized),
			OSType:  to.Ptr(armcompute.OperatingSystemTypesLinux),
		},
	}, nil)

	if error != nil {
		log.Fatalf("failed to create image: %v", error)
	}

	galleryImageVersionFactory, err := armcompute.NewGalleryImageVersionsClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create galleryImageVersionFactory: %v", err)
	}

	poller, err := galleryImageVersionFactory.BeginCreateOrUpdate(ctx, resourceGroupName, galleryName, "myGalleryImage", "1.0.0", armcompute.GalleryImageVersion{
		Location: to.Ptr("East US"),
		Properties: &armcompute.GalleryImageVersionProperties{
			SafetyProfile: &armcompute.GalleryImageVersionSafetyProfile{
				AllowDeletionOfReplicatedLocations: to.Ptr(false),
			},
			StorageProfile: &armcompute.GalleryImageVersionStorageProfile{
				OSDiskImage: &armcompute.GalleryOSDiskImage{
					Source: &armcompute.GalleryDiskImageSource{
						ID: to.Ptr("subscriptions/" + os.Getenv("AZURE_SUBSCRIPTION_ID") + "/resourceGroups/" + resourceGroupName + "/providers/Microsoft.Compute/snapshots/example-snapshot"),
					},
				},
			},
		},
	}, nil) // step 5

	if err != nil {
		log.Fatalf("failed to finish the request: %v", err)
	} 
	_ = poller

	_ , error = snapshotFactory.BeginDelete(ctx, resourceGroupName, "example-snapshot", nil) // step 6

	if error != nil {
		log.Fatalf("failed to delete snapshot: %v", error)
	}

}
// force update capzcontrolplanemanager can be used for check, can also just add annotation/label to edit at all for reconcile, may need to play around