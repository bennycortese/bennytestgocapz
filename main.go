package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	infrav1exp "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterv1exp "sigs.k8s.io/cluster-api/exp/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Generated from example definition: https://github.com/Azure/azure-rest-api-specs/blob/5d2adf9b7fda669b4a2538c65e937ee74fe3f966/specification/compute/resource-manager/Microsoft.Compute/GalleryRP/stable/2022-03-03/examples/sharedGalleryExamples/SharedGallery_Get.json
func main() {
	config, err := clientcmd.BuildConfigFromFlags("", "/home/bennystream/.kube/config")
	if err != nil {
		panic(err)
	}

	s := runtime.NewScheme()
	infrav1exp.AddToScheme(s)
	infrav1.AddToScheme(s)
	clusterv1exp.AddToScheme(s)
	clusterv1.AddToScheme(s)

	c, err := client.New(config, client.Options{Scheme: s})
	if err != nil {
		panic(err)
	}

	amp := &infrav1exp.AzureMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "machinepool-6423-mp-0",
		},
	}
	err = c.Get(context.TODO(), client.ObjectKeyFromObject(amp), amp)
	if err != nil {
		panic(err)
	}
	fmt.Println("provider id:", amp.Spec.ProviderID)

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("failed to obtain a credential: %v", err)
	}
	ctx := context.Background()
	clientFactory, err := armcompute.NewClientFactory(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	galleryLocation := os.Getenv("AZURE_LOCATION")
	galleryName := "GalleryInstantiation"

	gallery := armcompute.Gallery{
		Location: &galleryLocation,
	}
	

	galleryFactory, err := armcompute.NewGalleriesClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create gallery: %v", err)
	}

	galleryFactory.BeginCreateOrUpdate(ctx, "capi-snapshot-group", galleryName, gallery, nil)

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
