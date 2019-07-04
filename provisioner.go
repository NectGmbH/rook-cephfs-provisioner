package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	ref "k8s.io/client-go/tools/reference"
)

const finalizerName = "nect.com/rook-cephfs-provisioner"

// Provisioner is an k8s client which is able to provision persistent volumes for rook's cephfs.
type Provisioner struct {
	client           clientset.Interface
	storageClass     string
	fsName           string
	clusterNamespace string
	localPath        string
}

// NewProvisioner creates a new k8s volume provisioner for rook's cephfs.
func NewProvisioner(client clientset.Interface, storageClass string, fsName string, clusterNamespace string, localPath string) *Provisioner {
	return &Provisioner{
		client:           client,
		storageClass:     storageClass,
		fsName:           fsName,
		clusterNamespace: clusterNamespace,
		localPath:        localPath,
	}
}

// Handle handles the passed pvc by checking whether we care about the pvc and then creating a matching pv.
func (p *Provisioner) Handle(pvc *v1.PersistentVolumeClaim) error {
	if pvc.Spec.StorageClassName == nil {
		glog.Warningf("ignored pvc `%s` in namespace `%s` since it's selected storage class is nil", pvc.Name, pvc.Namespace)
	} else if *pvc.Spec.StorageClassName != p.storageClass {
		glog.V(3).Infof("ignored pvc `%s` in namespace `%s` since it's selected storage class `%s` doesn't match `%s`", pvc.Name, pvc.Namespace, pvc.Spec.StorageClassName, p.storageClass)
		return nil
	}

	if pvc.GetDeletionTimestamp() != nil { // TODO: check if our finalizer is first in list
		err := p.handleDeletion(pvc)
		if err != nil {
			return fmt.Errorf("couldn't delete pvc, see: %v", err)
		}

		return nil
	}

	if pvc.Status.Phase != v1.ClaimPending {
		glog.V(3).Infof("skipping pvc `%s` in namespace `%s` since it's not in phase pending", pvc.Name, pvc.Namespace)
		return nil
	}

	return p.handleUnboundPVC(pvc)
}

func (p *Provisioner) handleUnboundPVC(pvc *v1.PersistentVolumeClaim) error {
	glog.V(3).Infof("started binding of pvc `%s` in namespace `%s`", pvc.Name, pvc.Namespace)

	// TODO: Add finalizer to pvc
	pvc, err := p.addFinalizerToPVC(pvc)
	if err != nil {
		return fmt.Errorf("couldn't append finalizer to pvc, see: %v", err)
	}

	// Create /storage/pvc-fooo-bar-baz-qux
	err = p.createLocalPathForPVC(pvc)
	if err != nil {
		return fmt.Errorf("couldn't create localPath, see: %v", err)
	}

	// Create PV and deploy to k8s
	_, err = p.createPVforPVC(pvc)
	if err != nil {
		return fmt.Errorf("couldn't create pv for pvc, see: %v", err)
	}

	glog.V(3).Infof("finished binding of pvc `%s` in namespace `%s`", pvc.Name, pvc.Namespace)

	return nil
}

func (p *Provisioner) createLocalPathForPVC(pvc *v1.PersistentVolumeClaim) error {
	localPath := p.getLocalPathForPVC(pvc)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		glog.V(4).Infof("creating local path `%s` for pvc `%s` in namespace `%s`", localPath, pvc.Name, pvc.Namespace)

		err := os.Mkdir(localPath, 0777)
		if err != nil {
			return fmt.Errorf("couldn't create pvc folder at `%s`, see: %v", localPath, err)
		}

		glog.V(4).Infof("created local path `%s` for pvc `%s` in namespace `%s`", localPath, pvc.Name, pvc.Namespace)
	} else {
		glog.V(4).Infof("local path `%s` already existing for pvc `%s` in namespace `%s`", localPath, pvc.Name, pvc.Namespace)
	}

	return nil
}

func (p *Provisioner) isPVAlreadyExistsError(pv *v1.PersistentVolume, err error) bool {
	errMsg := fmt.Sprintf("persistentvolumes \"%s\" already exists", pv.Name)
	return err != nil && strings.Index(err.Error(), errMsg) != -1
}

func (p *Provisioner) createPVforPVC(pvc *v1.PersistentVolumeClaim) (*v1.PersistentVolume, error) {
	// TOOD: check if err is sth like "already existing" and continue
	glog.V(4).Infof("creating pv for pvc `%s` in namespace `%s`", pvc.Name, pvc.Namespace)
	pv, err := p.newPVforPVC(pvc)
	if err != nil {
		return nil, fmt.Errorf("couldn't create pv, see: %v", err)
	}

	newPV, err := p.client.CoreV1().PersistentVolumes().Create(pv)
	if p.isPVAlreadyExistsError(pv, err) {
		glog.V(4).Infof("skipping creation of pv `%s` for pvc `%s` in namespace `%s` since it already exists", pv.Name, pvc.Name, pvc.Namespace)
		return pv, nil
	} else if err != nil {
		return nil, fmt.Errorf("couldn't deploy pv, see: %v", err)
	}

	glog.V(4).Infof("created pv `%s` for pvc `%s` in namespace `%s`", pv.Name, pvc.Name, pvc.Namespace)

	return newPV, nil
}

func (p *Provisioner) addFinalizerToPVC(pvc *v1.PersistentVolumeClaim) (*v1.PersistentVolumeClaim, error) {
	finalizers := pvc.GetFinalizers()

	if len(finalizers) > 0 {
		for _, f := range finalizers {
			if f == finalizerName {
				return pvc, nil
			}
		}
	}

	pvc.Finalizers = append(pvc.Finalizers, finalizerName)

	newPVC, err := p.client.CoreV1().PersistentVolumeClaims(pvc.Namespace).Update(pvc)
	if err != nil {
		return nil, fmt.Errorf("couldn't update pvc, see: %v", err)
	}

	return newPVC, nil
}

func (p *Provisioner) tryRemoveFinalizerFromPVC(pvc *v1.PersistentVolumeClaim) (*v1.PersistentVolumeClaim, error) {
	finalizers := pvc.GetFinalizers()

	if len(finalizers) == 1 && finalizers[0] == finalizerName {
		pvc.Finalizers = nil
	} else if len(finalizers) > 1 && finalizers[0] == finalizerName {
		pvc.Finalizers = pvc.Finalizers[1:]
	}

	newPVC, err := p.client.CoreV1().PersistentVolumeClaims(pvc.Namespace).Update(pvc)
	if err != nil {
		return nil, fmt.Errorf("couldn't update pvc, see: %v", err)
	}

	return newPVC, nil
}

func (p *Provisioner) handleDeletion(pvc *v1.PersistentVolumeClaim) error {
	// Check whether it's our time to finalize the pvc.
	finalizers := pvc.GetFinalizers()
	if len(finalizers) > 0 && finalizers[0] != finalizerName {
		glog.V(3).Infof("skipping cleanup of pvc `%s` in namespace `%s` since there are other finalizers", pvc.Name, pvc.Namespace)
		return nil
	}

	glog.V(3).Infof("started cleanup of pvc `%s` in namespace `%s`", pvc.Name, pvc.Namespace)

	// Remove files from cephfs
	path := p.getLocalPathForPVC(pvc)
	err := os.RemoveAll(path)
	if err != nil {
		return fmt.Errorf("couldn't delete `%s`, see: %v", path, err)
	}

	// Remove PV
	err = p.client.CoreV1().PersistentVolumes().Delete(p.getPVNameForPVC(pvc), &metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("couldn't delete pv, see: %v", err)
	}

	// Remove finalizer
	pvc, err = p.tryRemoveFinalizerFromPVC(pvc)
	if err != nil {
		return fmt.Errorf("couldn't remove finalizer from pvc, see: %v", err)
	}

	glog.V(3).Infof("finished cleanup of pvc `%s` in namespace `%s`", pvc.Name, pvc.Namespace)

	return nil
}

func (p *Provisioner) getLocalPathForPVC(pvc *v1.PersistentVolumeClaim) string {
	return path.Join(p.localPath, p.getPVNameForPVC(pvc))
}

func (p *Provisioner) getPVNameForPVC(pvc *v1.PersistentVolumeClaim) string {
	return fmt.Sprintf("pvc-%s", pvc.UID)
}

func (p *Provisioner) newPVforPVC(pvc *v1.PersistentVolumeClaim) (*v1.PersistentVolume, error) {
	claimRef, err := ref.GetReference(scheme.Scheme, pvc)
	if err != nil {
		return nil, fmt.Errorf("couldn't get reference to pvc, see: %v", err)
	}

	return &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: p.getPVNameForPVC(pvc),
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName: p.storageClass,
			AccessModes:      pvc.Spec.AccessModes,
			Capacity: v1.ResourceList{
				// TODO set ceph.quota.max_bytes on volume path.
				v1.ResourceName(v1.ResourceStorage): pvc.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			ClaimRef: claimRef,
			PersistentVolumeSource: v1.PersistentVolumeSource{
				FlexVolume: &v1.FlexPersistentVolumeSource{
					Driver: "ceph.rook.io/rook",
					FSType: "ceph",
					Options: map[string]string{
						"clusterNamespace": p.clusterNamespace,
						"fsName":           p.fsName,
						"path":             "/" + p.getPVNameForPVC(pvc),
					},
				},
			},
		},
	}, nil
}
