package main

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	informers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Controller represents a controller used for binding pvcs to rook cephfs pvs
type Controller struct {
	kubeClient clientset.Interface

	pvcLister  listers.PersistentVolumeClaimLister
	pvcsSynced cache.InformerSynced

	handler func(*v1.PersistentVolumeClaim) error

	queue workqueue.RateLimitingInterface
}

// NewController creates a new rook cephfs provisioning controller
func NewController(
	client clientset.Interface,
	informer informers.PersistentVolumeClaimInformer,
	handler func(*v1.PersistentVolumeClaim) error,
) *Controller {

	cc := &Controller{
		kubeClient: client,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "pvc"),
		handler:    handler,
	}

	// Manage the addition/update of pvcs
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pvc := obj.(*v1.PersistentVolumeClaim)
			glog.V(4).Infof("Adding pvc %s", pvc.Name)
			cc.enqueue(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			oldPVC := old.(*v1.PersistentVolumeClaim)
			glog.V(4).Infof("Updating pvc %s", oldPVC.Name)
			cc.enqueue(new)
		},
		DeleteFunc: func(obj interface{}) {
			pvc, ok := obj.(*v1.PersistentVolumeClaim)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					glog.V(2).Infof("Couldn't get object from tombstone %#v", obj)
					return
				}
				pvc, ok = tombstone.Obj.(*v1.PersistentVolumeClaim)
				if !ok {
					glog.V(2).Infof("Tombstone contained object that is not a pvc: %#v", obj)
					return
				}
			}
			glog.V(4).Infof("Deleting pvc %s", pvc.Name)
			cc.enqueue(obj)
		},
	})
	cc.pvcLister = informer.Lister()
	cc.pvcsSynced = informer.Informer().HasSynced
	return cc
}

// Run the controller workers.
func (cc *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer cc.queue.ShutDown()

	glog.Infof("Starting pvc controller")
	defer glog.Infof("Shutting down pvc controller")

	if !cache.WaitForCacheSync(stopCh, cc.pvcsSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(cc.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (cc *Controller) runWorker() {
	for cc.processNextWorkItem() {
	}
}

func (cc *Controller) processNextWorkItem() bool {
	cKey, quit := cc.queue.Get()
	if quit {
		return false
	}

	defer cc.queue.Done(cKey)

	if err := cc.sync(cKey.(string)); err != nil {
		cc.queue.AddRateLimited(cKey)
		utilruntime.HandleError(fmt.Errorf("Sync %v failed with : %v", cKey, err))

		return true
	}

	cc.queue.Forget(cKey)
	return true

}

func (cc *Controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %+v: %v", obj, err))
		return
	}
	cc.queue.Add(key)
}

func (cc *Controller) sync(key string) error {
	startTime := time.Now()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	defer func() {
		glog.V(4).Infof("Finished syncing pvc %q (%v)", key, time.Since(startTime))
	}()

	pvc, err := cc.pvcLister.PersistentVolumeClaims(namespace).Get(name)
	if errors.IsNotFound(err) {
		glog.V(3).Infof("pvc has been deleted: %v", key)
		return nil
	}
	if err != nil {
		return err
	}

	// need to operate on a copy so we don't mutate the pvc in the shared cache
	pvc = pvc.DeepCopy()

	err = cc.handler(pvc)
	if err != nil {
		return fmt.Errorf("couldn't sync pvc `%s` in namespace `%s`, see: %v", name, namespace, err)
	}

	return nil
}
