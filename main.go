package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"


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
)

type writer struct {
	logFunc func(args ...interface{})
}

// Write passes string(p) into writer's logFunc and always returns len(p).
func (w writer) Write(p []byte) (n int, err error) {
	w.logFunc(string(p))
	return len(p), nil
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

	machinePoolName := "machinepool-11435-mp-0"

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

	//fmt.Println("provider id:", amp.Spec.ProviderID)

	replicaCount := amp.Status.Replicas

	healthyAmpm := &infrav1exp.AzureMachinePoolMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "-1",
		},
	}

	for i := 0; i < int(replicaCount); i++ {
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
	//fmt.Println(healthyAmpm)
	//ampMachines := amp.AzureMachinePoolList
	//fmt.Println(ampMachines)
	
	
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("failed to obtain a credential: %v", err)
	}


	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "machinepool-11435"},
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
				ResourceGroup: "machinepool-11435",
				NetworkSpec: infrav1.NetworkSpec{
					Vnet: infrav1.VnetSpec{Name: "capi-quickstart-rg-vnet", ResourceGroup: "machinepool-11435"},
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

	err = myscope.CordonAndDrain(ctx)
	if err != nil {
		log.Fatalf("failed to drain: %v", err)
	}

	//fmt.Println(healthyAmpm)
	/*val1, val2, err := myscope.GetNode(ctx)
	if err != nil {
		log.Fatalf("failed to drain: %v", err)
	}*/
	/*_ = val1
	_ = val2*/
	//fmt.Println(val1, " ", val2)

	clientFactory, err := armcompute.NewClientFactory(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
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

	galleryFactory.BeginCreateOrUpdate(ctx, "capi-quickstart-rg", galleryName, gallery, nil)

	galleryImageFactory, err := armcompute.NewGalleryImagesClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create galleryImageFactory: %v", err)
	}

	snapshotFactory, err := armcompute.NewSnapshotsClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create snapshotFactory: %v", err)
	}

	_ , error := snapshotFactory.BeginCreateOrUpdate(ctx, "capi-quickstart-rg", "example-snapshot", armcompute.Snapshot{
		Location: to.Ptr("East US"),
		Properties: &armcompute.SnapshotProperties{
			CreationData: &armcompute.CreationData{
				CreateOption: to.Ptr(armcompute.DiskCreateOptionCopy),
				SourceURI:    to.Ptr("/subscriptions/addeefcb-5be9-41a9-91d6-3307915e1428/resourceGroups/CAPI-QUICKSTART-RG/providers/Microsoft.Compute/disks/capi-quickstart-control-plane-gfpz4_OSDisk"),
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


	drainer := &kubedrain.Helper{
		Client:              kubeClient,
		Ctx:                 ctx,
		Force:               true,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		GracePeriodSeconds:  -1,
		// If a pod is not evicted in 20 seconds, retry the eviction next time the
		// machine gets reconciled again (to allow other machines to be reconciled).
		Timeout: 20 * time.Second,
		OnPodDeletedOrEvicted: func(pod *corev1.Pod, usingEviction bool) {
			usingEviction = false
		},
		Out:    writer{klog.Info},
		ErrOut: writer{klog.Error},
	}
	_ = drainer

	fmt.Println(node.Spec.Unschedulable)
	//cordonHelper := kubedrain.NewCordonHelper(node)

	
	if err := kubedrain.RunCordonOrUncordon(drainer, node, false); err != nil {
		fmt.Println("Failed to uncordon")
	}

	fmt.Println(node.Spec.Unschedulable)

	/*
	drainer := &kubedrain.Helper{
		Client:              kubeClient,
		Ctx:                 ctx,
		Force:               true,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		GracePeriodSeconds:  -1,
		// If a pod is not evicted in 20 seconds, retry the eviction next time the
		// machine gets reconciled again (to allow other machines to be reconciled).
		Timeout: 20 * time.Second,
		OnPodDeletedOrEvicted: func(pod *corev1.Pod, usingEviction bool) {
			verbStr := "Deleted"
			if usingEviction {
				verbStr = "Evicted"
			}
			log.V(4).Info(fmt.Sprintf("%s pod from Node", verbStr),
				"pod", fmt.Sprintf("%s/%s", pod.Name, pod.Namespace))
		},
		Out:    writer{klog.Info},
		ErrOut: writer{klog.Error},
	}*/

	/*_ , error = snapshotFactory.BeginDelete(ctx, "capi-quickstart-rg", "example-snapshot", nil)

	if error != nil {
		log.Fatalf("failed to delete snapshot: %v", error)
	}*/

	
	/*
	_ , error = galleryImageFactory.BeginCreateOrUpdate(ctx, "capi-quickstart-rg", galleryName, "myGalleryImage", armcompute.GalleryImage{
		Location: to.Ptr(os.Getenv("AZURE_LOCATION")),
		Properties: &armcompute.GalleryImageProperties{
			StorageProfile: &armcompute.ImageStorageProfile{
				OSDisk: &armcompute.ImageOSDisk{
					Snapshot: &armcompute.SubResource{
						ID: to.Ptr("subscriptions/" + os.Getenv("AZURE_SUBSCRIPTION_ID") + "/resourceGroups/" + "CAPI-QUICKSTART-RG" + "/providers/Microsoft.Compute/snapshots/example-snapshot"),
					},
					OSState: to.Ptr(armcompute.OperatingSystemStateTypesGeneralized),
					OSType:  to.Ptr(armcompute.OperatingSystemTypesLinux),
				},
				ZoneResilient: to.Ptr(false),
			},
		},
	}, nil)
	*/


	
	_ , error = galleryImageFactory.BeginCreateOrUpdate(ctx, "capi-quickstart-rg", galleryName, "myGalleryImage", armcompute.GalleryImage{
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

	poller, err := galleryImageVersionFactory.BeginCreateOrUpdate(ctx, "CAPI-QUICKSTART-RG", galleryName, "myGalleryImage", "1.0.0", armcompute.GalleryImageVersion{
		Location: to.Ptr("East US"),
		Properties: &armcompute.GalleryImageVersionProperties{
			SafetyProfile: &armcompute.GalleryImageVersionSafetyProfile{
				AllowDeletionOfReplicatedLocations: to.Ptr(false),
			},
			StorageProfile: &armcompute.GalleryImageVersionStorageProfile{
				OSDiskImage: &armcompute.GalleryOSDiskImage{
					Source: &armcompute.GalleryDiskImageSource{
						ID: to.Ptr("subscriptions/" + os.Getenv("AZURE_SUBSCRIPTION_ID") + "/resourceGroups/" + "CAPI-QUICKSTART-RG" + "/providers/Microsoft.Compute/snapshots/example-snapshot"),
					},
				},
			},
		},
	}, nil)

	if err != nil {
		log.Fatalf("failed to finish the request: %v", err)
	}

	_ = poller
	_ = galleryImageVersionFactory

	_ = galleryFactory
	_ = clientFactory
	
	/*
	galleryFactory.BeginCreateOrUpdate()

	res, err := clientFactory.NewSharedGalleryImagesClient(clientFactory)
if err != nil {
	log.Fatalf("failed to finish the request: %v", err)
}*/

	/*
	res, err := clientFactory.NewSharedGalleriesClient().Get(ctx, "eastus", "galleryUniqueName", nil)
	if err != nil {
		log.Fatalf("failed to finish the request: %v", err)
	}*/
	// You could use response here. We use blank identifier for just demo purposes.
	
	/*
	_ = res*/
	
	// If the HTTP response code is 200 as defined in example definition, your response structure would look as follows. Please pay attention that all the values in the output are fake values for just demo purposes.
	// res.SharedGallery = armcompute.SharedGallery{
	// 	Name: to.Ptr("myGalleryName"),
	// 	Location: to.Ptr("myLocation"),
	// 	Identifier: &armcompute.SharedGalleryIdentifier{
	// 		UniqueID: to.Ptr("/SharedGalleries/galleryUniqueName"),
	// 	},
	// }
}
