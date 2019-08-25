/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	monitorsv1alpha1 "peishu/demo-controller/api/v1alpha1"

	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// ConfigWatchReconciler reconciles a ConfigWatch object
type ConfigWatchReconciler struct {
	client.Client
	Log logr.Logger
}

type WatchType int

const (
	WatchConfigMap WatchType = 0
	WatchSecret    WatchType = 1
)

// +kubebuilder:rbac:groups=monitors.peishu.io,resources=configwatches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitors.peishu.io,resources=configwatches/status,verbs=get;update;patch

func (r *ConfigWatchReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("configwatch", req.NamespacedName)

	// Get the CR
	var cw monitorsv1alpha1.ConfigWatch

	if err := r.Get(ctx, req.NamespacedName, &cw); err != nil {
		log.Error(err, "unable to fetch configwatch")
		return ctrl.Result{}, ignoreNotFound(err)
	}

	// Get the clientset
	clientset, err := getClientset()
	if err != nil {
		panic(err.Error())
	}

	// Get informers
	informerFactory := informers.NewSharedInformerFactory(clientset, time.Second*30)
	configmapInformer := informerFactory.Core().V1().ConfigMaps().Informer()
	secretInformer := informerFactory.Core().V1().Secrets().Informer()

	// Register eventhandler to watch configmap updates
	configmapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			r.handleUpdateEvent(oldObj, newObj, &cw, clientset, WatchConfigMap)
		},
	})

	// Register eventhandler to watch secret updates
	secretInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			r.handleUpdateEvent(oldObj, newObj, &cw, clientset, WatchSecret)
		},
	})

	informerFactory.Start(wait.NeverStop)
	informerFactory.WaitForCacheSync(wait.NeverStop)

	return ctrl.Result{}, nil
}

func (r *ConfigWatchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitorsv1alpha1.ConfigWatch{}).
		Complete(r)
}

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

func getPodList(clientset kubernetes.Clientset, namespace string) (*corev1.PodList, error) {
	podList, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	return podList, err
}

func getClientset() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func getPodNames(podList *corev1.PodList, cw *monitorsv1alpha1.ConfigWatch, watchType WatchType) []string {
	var podNames []string
	var podFound bool = false

	for _, pod := range podList.Items {
		// we only look for running pods
		if pod.Status.Phase != "Running" {
			continue
		}

		for _, container := range pod.Spec.Containers {
			// if a pod is found, reset the found flag and move to the next pod
			if podFound {
				podFound = false
				break
			}
			for _, env := range container.Env {
				switch watchType {
				case WatchConfigMap:
					if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil && env.ValueFrom.ConfigMapKeyRef.Name == cw.Spec.ConfigMapToWatch {
						podNames = append(podNames, pod.Name)
						podFound = true // signal a pod was found and break out the inner for loop
						break
					}
				case WatchSecret:
					if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == cw.Spec.SecretToWatch {
						podNames = append(podNames, pod.Name)
						podFound = true // signal a pod was found and break out the inner for loop
						break
					}
				}

			}
		}
	}

	return podNames
}

func (r *ConfigWatchReconciler) handleUpdateEvent(oldObj, newObj interface{}, cw *monitorsv1alpha1.ConfigWatch, clientset *kubernetes.Clientset, watchType WatchType) {
	log := r.Log.WithValues("configwatch", "handelUpdatedEvent")
	var watchedPods []string

	switch watchType {
	case WatchConfigMap:
		oldConfigmap := oldObj.(*corev1.ConfigMap)
		newConfigmap := newObj.(*corev1.ConfigMap)

		if oldConfigmap.Name != cw.Spec.ConfigMapToWatch || oldConfigmap.Name == "controller-leader-election-helper" || oldConfigmap.ResourceVersion == newConfigmap.ResourceVersion {
			return
		}

		log.Info("Configmap " + newConfigmap.Name + " has been changed")
		podList, err := clientset.CoreV1().Pods(cw.Spec.Namespace).List(metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		watchedPods = getPodNames(podList, cw, WatchConfigMap)
	case WatchSecret:
		oldSecret := oldObj.(*corev1.Secret)
		newSecret := newObj.(*corev1.Secret)

		if oldSecret.Name != cw.Spec.SecretToWatch || oldSecret.Name == "controller-leader-election-helper" || oldSecret.ResourceVersion == newSecret.ResourceVersion {
			return
		}

		log.Info("Secret " + newSecret.Name + " has been changed")
		podList, err := clientset.CoreV1().Pods(cw.Spec.Namespace).List(metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		watchedPods = getPodNames(podList, cw, WatchSecret)

	}

	r.deletePods(watchedPods, cw, clientset)
}

func (r *ConfigWatchReconciler) deletePods(watchedPods []string, cw *monitorsv1alpha1.ConfigWatch, clientset *kubernetes.Clientset) {
	log := r.Log.WithValues("configwatch", "deletePods")
	ctx := context.Background()
	for _, podName := range watchedPods {

		err := clientset.CoreV1().Pods(cw.Spec.Namespace).Delete(podName, &metav1.DeleteOptions{})
		if err != nil {
			//panic(err.Error())
			log.Error(err, "Operation failed in deleting pod "+podName+".")
			return
		}

		// after recycled the pod, it's time to record the operation to the Events
		cw.Status.Message = "Pod " + podName + " has been deleted and will be recreated."
		// ignore "the object has been modified; please apply your changes to the latest version and try again" error
		_ = r.Status().Update(ctx, cw)
	}
}
