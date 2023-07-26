package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	//"strings"
	//"os/exec"
	//"bytes"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"

	"github.com/Azure/azure-sdk-for-go/profiles/latest/compute/mgmt/compute"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/go-autorest/autorest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	kubedrain "k8s.io/kubectl/pkg/drain" // go look at - https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/v1.9.2/azure/scope/machinepoolmachine.go#L372 for how to edit it
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	infrav1exp "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	clusterv1exp "sigs.k8s.io/cluster-api/exp/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	//"github.com/Azure/azure-sdk-for-go/services/preview/compute/mgmt/2020-12-01-preview/compute"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	//"k8s.io/client-go/tools/remotecommand"
	//"sigs.k8s.io/cluster-api-provider-azure/azure/converters"
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

		time.Sleep(60 * time.Second)
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

	machinePoolClusterName := "machinepool-10052"

	machinePoolName := machinePoolClusterName + "-mp-0"

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

	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	resourceGroup := machinePoolClusterName
	vmssName := machinePoolName
	_ = subscriptionID
	_ = resourceGroup
	//_ = nodeName

	fmt.Println(os.Getenv("AZURE_CLIENT_ID"))
	credConfig := auth.NewClientCredentialsConfig(os.Getenv("AZURE_CLIENT_ID"), os.Getenv("AZURE_CLIENT_SECRET"), os.Getenv("AZURE_TENANT_ID"))
	authorizer, err := credConfig.Authorizer()
	if err != nil {
		panic(err)
	}

	vmssClient := compute.NewVirtualMachineScaleSetsClient(subscriptionID)
	vmssClient.Authorizer = authorizer

	vmssVMsClient := compute.NewVirtualMachineScaleSetVMsClient(subscriptionID)
	vmssVMsClient.Authorizer = authorizer

	vmssVMs, err := vmssVMsClient.List(context.Background(), resourceGroup, vmssName, "", "", "")
	if err != nil {
		panic(err)
	}
	instanceIds := make([]string, 0)
	for _, vm := range vmssVMs.Values() {
		instanceIds = append(instanceIds, *vm.InstanceID)
	}

	curInstanceID := instanceIds[len(instanceIds)-1]

	healthyAmpm := &infrav1exp.AzureMachinePoolMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      machinePoolName + "-" + curInstanceID,
		},
	}

	err = c.Get(ctx, client.ObjectKeyFromObject(healthyAmpm), healthyAmpm)
	if err != nil {
		panic(err)
	}

	fmt.Printf(machinePoolName + "-" + curInstanceID)

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("failed to obtain a credential: %v", err)
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: machinePoolClusterName},
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
				ResourceGroup: machinePoolClusterName,
				NetworkSpec: infrav1.NetworkSpec{
					Vnet: infrav1.VnetSpec{Name: resourceGroup + "-vnet", ResourceGroup: machinePoolClusterName},
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

	gallery_image := infrav1.AzureComputeGalleryImage{
		SubscriptionID: to.Ptr(os.Getenv("AZURE_SUBSCRIPTION_ID")),
		ResourceGroup:  to.Ptr(resourceGroup),
		Gallery:        "GalleryInstantiation2",
		Name:           "myGalleryImage2",
		Version:        "1.0.1",
	}
	fmt.Println(gallery_image)

	new_image := infrav1.Image{
		ComputeGallery: &gallery_image,
	}

	fmt.Println(new_image)

	amp.Spec.Template.Image = &new_image

	//fmt.Println(.Spec.Template.Image)

	node, found, err := myscope.GetNode(ctx)
	if err != nil {
		log.Fatalf("failed to find node: %v", err)
	} else if !found {
		log.Fatalf("failed to find node with the ProviderID")
	}
	fmt.Println(node.Name)
	fmt.Println(node.GetName())

	/*for _, image := range node.Status.Images {
		fmt.Println(converters.ImageToSDK(image))
	}*/
	fmt.Println(node.Status.NodeInfo.OSImage)

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

	//get_sleep := "apk add --no-cache coreutils && apk add tar"
	//apk_commands := "apk update && apk update && apk search curl && apk -a info curl && apk add curl"
	//prerunCommands := "/usr/bin/nsenter && -m/proc/1/ns/mnt && --"
	/*
		cat_test := "/host/bin/cp -f /host/dev/null /host/etc/hostname"
		rm_command := "/host/bin/rm -rf /host/var/lib/cloud/data/* /host/var/lib/cloud/instances/* /host/var/lib/waagent/history/* /host/var/lib/waagent/events/* /host/var/log/journal/*"
		replace_machine_id_command := "/host/bin/cp /host/dev/null /host/etc/machine-id"
	*/

	sleepy := "sleep 4"
	cat_test := "/bin/cp -f /dev/null /etc/hostname"
	rm_command := "/bin/rm -rf /var/lib/cloud/data/* /var/lib/cloud/instances/* /var/lib/waagent/history/* /var/lib/waagent/events/* /var/log/journal/*"
	replace_machine_id_command := "/bin/cp /dev/null /etc/machine-id"
	kubectl_cleanup := "/bin/rm -rf /run/kubeadm/kubeadm-join-config.yaml"
	// /etc/kubernetes/kubelet.conf /etc/kubernetes/pki/ca.crt
	//insane_kubeadm_sequence := insane_kubeadm_sequence_1 + " && " + insane_kubeadm_sequence_2 + " && " + insane_kubeadm_sequence_3 + " && " + insane_kubeadm_sequence_4
	//command := []string{"sh", "-c", get_sleep + " && " + sleepy + " && " + cat_test + " && " + rm_command + " && " + replace_machine_id_command + " && " + insane_kubeadm_sequence}
	//command := []string{"sh", "-c", cat_test}
	//command = []string{"sh", "-c", "cat /etc/hostname"} // Todo - figure out what was supposed to be removed in /var/lib/cloud/instance, we need /var/lib/cloud/instance/scripts
	//insane_kubeadm_sequence := "wget https://storage.googleapis.com/kubernetes-release/release/v1.27.1/bin/linux/amd64/kubeadm && chmod +x kubeadm && echo y | ./kubeadm reset"
	//command := []string{"sh", "-c", get_sleep + " && " + apk_commands + " && " + sleepy + " && " + insane_kubeadm_sequence + " && " + rm_command + " && " + replace_machine_id_command}
	//insane_kubeadm_sequence := "echo y | /host/usr/bin/kubeadm reset && echo y | /host/usr/bin/kubeadm init"
	//insane_kubeadm_sequence := "/host/bin/rm -rf /host/etc/kubernetes/kubelet.conf /host/etc/kubernetes/pki/ca.crt"
	//kubeadm_step_3 := "curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | gpg --dearmor -o /etc/apt/keyrings/kubernetes-archive-keyring.gpg"
	//kubeadm_step_4 := "echo \"deb [signed-by=/etc/apt/keyrings/kubernetes-archive-keyring.gpg] https://apt.kubernetes.io/ kubernetes-xenial main\" | tee /etc/apt/sources.list.d/kubernetes.list"
	//kubeadm_step_5 := "apt-get update && apt-get install -y kubelet kubeadm kubectl && apt-mark hold kubelet kubeadm kubectl"
	//kubeadm_install := "apt-get update && apt-get install -y apt-transport-https ca-certificates curl && " + kubeadm_step_3 + " && " + kubeadm_step_4 + " && " + kubeadm_step_5
	// && echo y | apt update && echo y | apt upgrade && echo y | apt-get install wget
	//kubeadm_reset := "kubeadm reset"

	// /usr/bin/systemctl stop kubelet.service

	command := []string{"/usr/bin/nsenter", "-m/proc/1/ns/mnt", "--", "/bin/sh", "-xc", sleepy + " && touch rock.txt &&" + cat_test + " && " + rm_command + " && " + replace_machine_id_command + " && " + kubectl_cleanup}
	runAsUser := int64(0)
	isTrue := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "exec-pod-",
		},
		Spec: corev1.PodSpec{
			NodeName:    node.Name,
			HostNetwork: isTrue,
			HostPID:     isTrue,
			Containers: []corev1.Container{ // Node specific selector
				{
					Name:    "exec-container",
					Command: command,
					Image:   "ubuntu:latest",
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  &runAsUser, // Run as root user
						Privileged: &isTrue,
					},
					/*VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "host-root",
							MountPath: "/host",
						},
					},*/
				},
			},
			/*Volumes: []corev1.Volume{
				{
					Name: "host-root",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/",
						},
					},
				},
			},*/
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": node.Name,
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	createdPod, err := kubeClient.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}
	_ = createdPod
	_ = pod

	time.Sleep(60 * time.Second)

	/*err = myscope.CordonAndDrain(ctx) // step 2
	if err != nil {
		//log.Fatalf("failed to drain: %v", err)
		fmt.Println("Broken tilt file: %v", err)
	}*/

	vm, err := vmssVMsClient.Get(ctx, resourceGroup, vmssName, curInstanceID, "")
	if err != nil {
		log.Fatalf("Failed to find VM")
	}
	//az vmss run-command invoke -g "machinepool-29922" -n "machinepool-29922-mp-0" --command-id RunShellScript --instance-id 0 --scripts "kubeadm reset -f > kubres.txt"
	runCommandInput := compute.RunCommandInput{CommandID: to.Ptr("RunShellScript"),
		//Parameters: {RunElevated: to.Ptr(true)},
		Script: &[]string{"kubeadm reset -f > HELP.txt"},
		// /var/lib/waagent/run-command/download/
	}

	result, err := vmssVMsClient.RunCommand(ctx, machinePoolClusterName, machinePoolName, curInstanceID, runCommandInput)
	if err != nil {
		log.Fatalf("failed to run command on VMSS VM %s in resource group %s: %v", curInstanceID, resourceGroup, err)
	}

	time.Sleep(60 * time.Second)

	fmt.Printf("Run Command Result and Error: %v, %v \n", result, err)

	osDisk := vm.StorageProfile.OsDisk.ManagedDisk.ID
	fmt.Println("OS DISK: ", *osDisk)

	if *osDisk == "nil" {
		panic("Disk not found")
	}

	snapshotFactory, err := armcompute.NewSnapshotsClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create snapshotFactory: %v", err)
	}
	_ = snapshotFactory
	fmt.Printf("%T", osDisk)
	fmt.Printf("%T", cred)

	poller, error := snapshotFactory.BeginCreateOrUpdate(ctx, resourceGroup, "example-snapshot2", armcompute.Snapshot{ // step 3
		Location: to.Ptr("East US"),
		Properties: &armcompute.SnapshotProperties{
			CreationData: &armcompute.CreationData{
				CreateOption: to.Ptr(armcompute.DiskCreateOptionCopy),
				SourceURI:    osDisk,
			},
		},
	}, nil)

	if error != nil {
		log.Fatalf("failed to create snapshot: %v", error)
	}

	snapshotPollingInterval := 15 * time.Second

	future, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: snapshotPollingInterval})
	if err != nil {
		return
	}

	_ = future

	drainer := &kubedrain.Helper{
		Client:              kubeClient,
		Ctx:                 ctx,
		Force:               true,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		GracePeriodSeconds:  -1,
		Timeout:             20 * time.Second,
		OnPodDeletedOrEvicted: func(pod *corev1.Pod, usingEviction bool) {
			usingEviction = false
		},
		Out:    writer{klog.Info},
		ErrOut: writer{klog.Error},
	}
	_ = drainer

	/*if err := kubedrain.RunCordonOrUncordon(drainer, node, false); err != nil { // step 4
		fmt.Println("Failed to uncordon")
	}*/

	galleryLocation := os.Getenv("AZURE_LOCATION")
	galleryName := "GalleryInstantiation2"

	gallery := armcompute.Gallery{
		Location: &galleryLocation,
	}

	galleryFactory, err := armcompute.NewGalleriesClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create gallery: %v", err)
	}

	galleryFactory.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, gallery, nil)

	galleryImageFactory, err := armcompute.NewGalleryImagesClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create galleryImageFactory: %v", err)
	}

	pollerGal, err := galleryImageFactory.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, "myGalleryImage2", armcompute.GalleryImage{
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

	if err != nil {
		log.Fatalf("failed to create image: %v", err)
	}

	future2, err := pollerGal.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: snapshotPollingInterval})
	if err != nil {
		return
	}

	_ = future2

	galleryImageVersionFactory, err := armcompute.NewGalleryImageVersionsClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create galleryImageVersionFactory: %v", err)
	}

	pollerDef, err := galleryImageVersionFactory.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, "myGalleryImage2", "1.0.1", armcompute.GalleryImageVersion{
		Location: to.Ptr("East US"),
		Properties: &armcompute.GalleryImageVersionProperties{
			SafetyProfile: &armcompute.GalleryImageVersionSafetyProfile{
				AllowDeletionOfReplicatedLocations: to.Ptr(false),
			},
			StorageProfile: &armcompute.GalleryImageVersionStorageProfile{
				OSDiskImage: &armcompute.GalleryOSDiskImage{
					Source: &armcompute.GalleryDiskImageSource{
						ID: to.Ptr("subscriptions/" + os.Getenv("AZURE_SUBSCRIPTION_ID") + "/resourceGroups/" + resourceGroup + "/providers/Microsoft.Compute/snapshots/example-snapshot2"),
					},
				},
			},
		},
	}, nil) // step 5

	if err != nil {
		log.Fatalf("failed to finish the request: %v", err)
	}

	future3, err := pollerDef.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: snapshotPollingInterval})
	if err != nil {
		return
	}

	_ = future3

	fmt.Printf("%T", poller)
	_ = poller

	/*
		_ , error = snapshotFactory.BeginDelete(ctx, resourceGroupName, "example-snapshot", nil) // step 6

		if error != nil {
			log.Fatalf("failed to delete snapshot: %v", error)
		}*/

}

// force update capzcontrolplanemanager can be used for check, can also just add annotation/label to edit at all for reconcile, may need to play around
