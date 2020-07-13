package processor

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	v1 "k8s.io/api/core/v1"
)

type VolumeProcessor struct {
	ClusterInfoScraper cluster.ClusterScraperInterface
	KubeCluster        *repository.KubeCluster
}

func NewVolumeProcessor(kubeClient cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *VolumeProcessor {
	return &VolumeProcessor{
		ClusterInfoScraper: kubeClient,
		KubeCluster:        kubeCluster,
	}
}

func (p *VolumeProcessor) ProcessVolumes() {
	clusterName := p.KubeCluster.Name
	pvList, err := p.ClusterInfoScraper.GetAllPVs()
	if err != nil {
		glog.Errorf("Failed to get persistent volumes for cluster %s: %v.", clusterName, err)
		return
	}
	glog.V(2).Infof("There are %d volumes.", len(pvList))

	pvcList, err := p.ClusterInfoScraper.GetAllPVCs()
	if err != nil {
		glog.Errorf("Failed to get persistent volumes claims for cluster %s: %v.", clusterName, err)
		return
	}
	glog.V(2).Infof("There are %d volume claims.", len(pvcList))

	allPods, err := p.ClusterInfoScraper.GetAllPods()
	if err != nil {
		glog.Errorf("Failed to get all pods for cluster %s: %v.", clusterName, err)
		return
	}

	// A map which lists the volume to pvc bindings (1 to 1)
	// There can be volumes which are not bound via claims
	// and pods use them directly.
	volumeToClaimMap := make(map[*v1.PersistentVolume]*v1.PersistentVolumeClaim)
	for _, pv := range pvList {
		found := false
		for _, pvc := range pvcList {
			if pvc.Spec.VolumeName == pv.Name {
				volumeToClaimMap[pv] = pvc
				found = true
				break
			}
		}
		if !found {
			volumeToClaimMap[pv] = nil
		}
	}

	// TODO (irfanurrehman): We might need the volume name to map the metrics generated from pod
	// and gathered via kubelet monitor to the individual volumes.
	// TODO (irfanurrehman): (Next Step) To find out how are metrics generated for volumes which
	// are used by multiple pods.
	volumeToPodsMap := make(map[*v1.PersistentVolume][]repository.PodVolume)
	for pv, pvc := range volumeToClaimMap {
		if pvc == nil {
			// Unused volume.
			volumeToPodsMap[pv] = nil
		}
		for _, pod := range allPods {
			for _, vol := range pod.Spec.Volumes {
				claim := vol.VolumeSource.PersistentVolumeClaim
				if claim != nil {
					if pvc != nil {
						if claim.ClaimName == pvc.Name {
							pVol := repository.PodVolume{
								QualifiedPodName: util.PodKeyFunc(pod),
								MountName:        vol.Name,
							}
							volumeToPodsMap[pv] = append(volumeToPodsMap[pv], pVol)
						}
					}
				} else {
					// TODO (irfanurrehman): check other volume sources categorically.
					// This could be another volume type directly used by pod.
					// Pod owner must know all details of the cloud/storage volume
					// and specify that explicitly as details here (There wont be any
					// corresponding PV or PVC resource in k8s).
					// Although archaic this is still possible and we will need to
					// take a call if this needs support.
				}
			}

		}
	}

	p.KubeCluster.VolumeToPodsMap = volumeToPodsMap
	p.KubeCluster.PodToVolumesMap = inverseVolToPodsMap(volumeToPodsMap)
}

func inverseVolToPodsMap(volToPodsMap map[*v1.PersistentVolume][]repository.PodVolume) map[string][]repository.MountedVolume {
	podToVolsMap := make(map[string][]repository.MountedVolume)
	for vol, podVols := range volToPodsMap {
		for _, podVol := range podVols {
			podToVolsMap[podVol.QualifiedPodName] = append(podToVolsMap[podVol.QualifiedPodName],
				repository.MountedVolume{UsedVolume: vol, MountName: podVol.MountName})
		}
	}
	return podToVolsMap
}
