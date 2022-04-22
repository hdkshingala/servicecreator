package controller

import (
	"context"
	"log"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	informers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	serviceKey = "infracloud.io/service"
)

type Controller struct {
	client         kubernetes.Interface
	lister         listers.DeploymentLister
	wq             workqueue.RateLimitingInterface
	depCacheSynced cache.InformerSynced
	timeStarted    time.Time
}

func NewController(clientset kubernetes.Interface, depInformer informers.DeploymentInformer) *Controller {
	cont := &Controller{
		client:         clientset,
		lister:         depInformer.Lister(),
		wq:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "kluster"),
		depCacheSynced: depInformer.Informer().HasSynced,
		timeStarted:    time.Now().UTC(),
	}

	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    cont.handleAdd,
			UpdateFunc: cont.handleUpdate,
			DeleteFunc: cont.handleDelete,
		},
	)

	return cont
}

func (cont *Controller) handleAdd(new interface{}) {
	deploy, ok := new.(*appsv1.Deployment)
	if !ok {
		log.Printf("Error converting object '%+v' to Deployment when Add was called.\n", new)
	}

	if _, ok := cont.checkAnnotation(deploy); cont.timeStarted.Sub(deploy.CreationTimestamp.Time.UTC()) > 0*time.Second {
		return
	} else if !ok {
		log.Printf("Skipping creation of service for deployment:'%s' as the required annotation is not present.", deploy.Name)
		return
	} else {
		cont.wq.Add(deploy)
	}
}

func (cont *Controller) handleUpdate(old, new interface{}) {
	oldDeploy, ok1 := old.(*appsv1.Deployment)
	newDeploy, ok2 := new.(*appsv1.Deployment)
	if !ok1 || !ok2 {
		log.Printf("Error converting object '%+v' or '%+v' to Deployment when Update was called.\n", old, new)
	}

	val1, ok1 := cont.checkAnnotation(oldDeploy)
	val2, ok2 := cont.checkAnnotation(newDeploy)
	if oldDeploy.ResourceVersion == newDeploy.ResourceVersion {
		return
	} else if ok1 && ok2 && val1 == val2 {
		log.Printf("Skipping creation of service for deployment:'%s' as the service is already present.", oldDeploy.Name)
		return
	} else if ok1 && !ok2 {
		newDeploy = newDeploy.DeepCopy()
		newDeploy.Annotations[serviceKey] = "delete"
		cont.wq.Add(newDeploy)
	} else if !ok2 {
		log.Printf("Skipping creation of service for deployment:'%s' as the required annotation is not present.", newDeploy.Name)
		return
	} else {
		cont.wq.Add(newDeploy)
	}
}

func (cont *Controller) handleDelete(old interface{}) {
	deploy, ok := old.(*appsv1.Deployment)
	if !ok {
		log.Printf("Error converting object '%+v' to Deployment when Add was called.\n", old)
	}

	if _, ok := cont.checkAnnotation(deploy); !ok {
		log.Printf("Skipping deletion of service for deployment:'%s' as the service is not created.", deploy.Name)
		return
	} else {
		deploy = deploy.DeepCopy()
		deploy.Annotations[serviceKey] = "delete"
		cont.wq.Add(deploy)
	}
}

func (cont *Controller) Run(ch chan struct{}) error {
	if bool := cache.WaitForCacheSync(ch, cont.depCacheSynced); !bool {
		log.Println("cache was not synced.")
	}

	go wait.Until(cont.worker, time.Second, ch)
	<-ch
	return nil
}

func (cont *Controller) worker() {
	for cont.process() {
	}
}

func (cont *Controller) process() bool {
	item, shutDown := cont.wq.Get()
	deploy := item.(*appsv1.Deployment)
	if shutDown {
		log.Println("Cache is closed")
		return false
	}

	defer cont.wq.Forget(item)

	if deploy.Namespace == "kube-system" || deploy.Namespace == "local-path-storage" {
		return true
	}

	ctx := context.Background()
	if val, _ := cont.checkAnnotation(deploy); val == "delete" {
		log.Printf("Deleting service for deployment %s.\n", deploy.Name)
		// delete service
		err := cont.client.CoreV1().Services(deploy.Namespace).Delete(ctx, deploy.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Printf("Error received while deleting service '%s', error %s\n", deploy.Name, err.Error())
			return false
		}

		return true
	}

	log.Printf("Creating service for deployment %s.\n", deploy.Name)
	err := cont.createService(ctx, deploy)
	if err != nil {
		log.Printf("Error received which creating resources with name: %s, %s", deploy.Name, err.Error())
		return false
	}

	return true
}

func (cont *Controller) createService(ctx context.Context, deploy *appsv1.Deployment) error {
	port, _ := cont.checkAnnotation(deploy)
	intPort, err := strconv.Atoi(port)
	if err != nil {
		return err
	}

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploy.Name,
			Namespace: deploy.Namespace,
			Labels:    deploy.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: deploy.Spec.Template.Labels,
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: int32(intPort),
				},
			},
		},
	}
	_, err = cont.client.CoreV1().Services(deploy.Namespace).Create(ctx, &svc, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Printf("Updating service for deployment %s.\n", deploy.Name)
			_, err = cont.client.CoreV1().Services(deploy.Namespace).Update(ctx, &svc, metav1.UpdateOptions{})
			if err != nil {
				log.Printf("Error received which updating service with name: %s, %s", deploy.Name, err.Error())
				return err
			}
		} else {
			log.Printf("Error received which creating service with name: %s, %s", deploy.Name, err.Error())
			return err
		}
	}

	return nil
}

func (cont *Controller) checkAnnotation(deploy *v1.Deployment) (string, bool) {
	val, ok := deploy.Annotations[serviceKey]
	return val, ok
}
