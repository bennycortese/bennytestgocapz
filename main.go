package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"


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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"github.com/Azure/go-autorest/autorest"
)

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

	fmt.Println("provider id:", amp.Spec.ProviderID)

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
	fmt.Println(healthyAmpm)
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
	
	/*
	err = myscope.CordonAndDrain(ctx)
	if err != nil {
		log.Fatalf("failed to drain: %v", err)
	}*/
	val1, val2, err := myscope.GetNode(ctx)
	if err != nil {
		log.Fatalf("failed to drain: %v", err)
	}
	_ = val1
	_ = val2
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

	imageFactory, err := armcompute.NewImagesClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create imageFactory: %v", err)
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

	/*_ , error = snapshotFactory.BeginDelete(ctx, "capi-quickstart-rg", "example-snapshot", nil)

	if error != nil {
		log.Fatalf("failed to delete snapshot: %v", error)
	}*/

	


	
	_ , error = imageFactory.BeginCreateOrUpdate(ctx, "capi-quickstart-rg", "myImage", armcompute.Image{
		Location: to.Ptr(os.Getenv("AZURE_LOCATION")),
		Properties: &armcompute.ImageProperties{
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
	_ = imageFactory

	if error != nil {
		log.Fatalf("failed to create image: %v", error)
	}

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
