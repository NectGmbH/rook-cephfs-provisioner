package main

import (
	"flag"
	"time"

	"github.com/golang/glog"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var kubeconfig, masterURL, storageClass, fsName, clusterNamespace, localPath string

	flag.StringVar(&storageClass, "storage-class", "", "The name of the storage class whose pvcs should be provisioned.")
	flag.StringVar(&fsName, "fs-name", "", "The name of the cephfilesystems.ceph.rook.io resource which should be used for dynamic provisioning.")
	flag.StringVar(&clusterNamespace, "cluster-namespace", "", "The namespace containing the cephfs.")
	flag.StringVar(&localPath, "local-path", "", "The local path where the whole cephfs is mounted.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.Parse()

	if storageClass == "" {
		glog.Fatalf("missing storage class name.")
	}

	if fsName == "" {
		glog.Fatalf("missing fsName.")
	}

	if clusterNamespace == "" {
		glog.Fatalf("missing cluster-namespace.")
	}

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, time.Second*30)
	provisioner := NewProvisioner(kubeClient, storageClass, fsName, clusterNamespace, localPath)

	controller := NewController(
		kubeClient,
		kubeInformerFactory.Core().V1().PersistentVolumeClaims(),
		provisioner.Handle,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	kubeInformerFactory.Start(stopCh)
	controller.Run(2, stopCh)
}
