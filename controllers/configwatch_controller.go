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

	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	//"k8s.io/client-go/util/workqueue"
)

// ConfigWatchReconciler reconciles a ConfigWatch object
type ConfigWatchReconciler struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=monitors.peishu.io,resources=configwatches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitors.peishu.io,resources=configwatches/status,verbs=get;update;patch

func (r *ConfigWatchReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("configwatch", req.NamespacedName)

	// (1) Get information from CR
	var cw monitorsv1alpha1.ConfigWatch

	if err := r.Get(ctx, req.NamespacedName, &cw); err != nil {
		log.Error(err, "unable to fetch configwatch")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, ignoreNotFound(err)
	}

	// (2) Get the configmap being watched
	// config, err := rest.InClusterConfig()
	// if err != nil {
	// 	panic(err.Error())
	// }

	// clientset, err := kubernetes.NewForConfig(config)
	// if err != nil {
	// 	panic(err.Error())
	// }
	clientset, err := getClientset()
	if err != nil {
		panic(err.Error())
	}

	// (3) Find pods that use the configmap

	var watchedPodsForConfigMap []string //[]corev1.Pod
	var watchedPodsForSecret []string

	// Get all pods under the given namespace
	podList, err := clientset.CoreV1().Pods(cw.Spec.Namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	watchedPodsForConfigMap, watchedPodsForSecret = getPodNameLists(podList, &cw)

	log.Info("Totoal pods that depends on the configmap found: " + strconv.Itoa(len(watchedPodsForConfigMap)))
	log.Info("Totoal pods that depends on the secrets found: " + strconv.Itoa(len(watchedPodsForSecret)))

	// NOTE: according to "Under the hood of Kubebuilder framework" (https://itnext.io/under-the-hood-of-kubebuilder-framework-ff6b38c10796):
	// "kubebuilder provides a generic controller that acts as a wrapper for our custom controller. It is based on the sample-controller.
	// It defines the queue in which Objects are delivered by Informers using event handlers (not shown).
	// The queue itself is not exposed to our custom controller." -- in other words, there is no needs to explictly use queue/handler in
	// or code here
	// Create a workqueue
	//queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	// (4) monitor configmap events
	informerFactory := informers.NewSharedInformerFactory(clientset, time.Second*30)
	configmapInformer := informerFactory.Core().V1().ConfigMaps().Informer()
	secretInformer := informerFactory.Core().V1().Secrets().Informer()

	configmapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		// AddFunc: func(obj interface{}) {
		// 	log.Info("configmap added")
		// },
		// DeleteFunc: func(obj interface{}) {
		// 	log.Info("configmag deleted:")
		// },
		UpdateFunc: func(oldObj, newObj interface{}) {

			oldConfigmap := oldObj.(*corev1.ConfigMap)
			newConfigmap := newObj.(*corev1.ConfigMap)

			if oldConfigmap.ResourceVersion != newConfigmap.ResourceVersion && oldConfigmap.Name != "controller-leader-election-helper" {

				log.Info("configmap " + newConfigmap.Name + " has been changed")
				// delete affected pods to force recreation
				for _, podName := range watchedPodsForConfigMap {
					err = clientset.CoreV1().Pods(cw.Spec.Namespace).Delete(podName, &metav1.DeleteOptions{})
					if err != nil {
						panic(err.Error())
					}

					// after recycled the pod, it's time to record the operation to the Events
					cw.Status.Message = "Pod " + podName + " has been deleted and will be recreated."
					//TODO: write to Events collection of the CR
					err := r.Status().Update(ctx, &cw)
					if err != nil {
						panic(err.Error())
					}
					//queue up the task -- no longer needed, see comments above.
					//queue.Add(podName)

					// refresh the lists and get out
					watchedPodsForConfigMap, watchedPodsForSecret = getPodNameLists(podList, &cw)
					return
				}
			}
		},
	})

	secretInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {

			oldSecret := oldObj.(*corev1.Secret)
			newSecret := newObj.(*corev1.Secret)

			if oldSecret.ResourceVersion != newSecret.ResourceVersion && oldSecret.Name != "controller-leader-election-helper" {

				log.Info("Secret " + newSecret.Name + " has been changed")
				// delete affected pods to force recreation
				for _, podName := range watchedPodsForSecret {
					err = clientset.CoreV1().Pods(cw.Spec.Namespace).Delete(podName, &metav1.DeleteOptions{})
					if err != nil {
						panic(err.Error())
					}

					// after recycled the pod, it's time to record the operation to the Events
					cw.Status.Message = "Pod " + podName + " has been deleted and will be recreated."
					//TODO: write to Events collection of the CR
					err := r.Status().Update(ctx, &cw)
					if err != nil {
						panic(err.Error())
					}
					//queue up the task -- no longer needed, see comments above.
					//queue.Add(podName)

					// refresh the lists and get out
					watchedPodsForConfigMap, watchedPodsForSecret = getPodNameLists(podList, &cw)
					return
				}
			}
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

// a function that return two name lists, one for configmaps and another for secrets
func getPodNameLists(podList *corev1.PodList, cw *monitorsv1alpha1.ConfigWatch) ([]string, []string) {
	var podsUseConfigMap []string
	var podsUseSecret []string
	var podFoundForConfigMap bool
	var podFoundForSecret bool
	//TODO: write logic here
	for _, pod := range podList.Items {
		for _, container := range pod.Spec.Containers {
			// if a pod is found, reset the found flag and move to the next pod
			if podFoundForConfigMap {
				podFoundForConfigMap = false
				break
			}
			for _, env := range container.Env {
				if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil && env.ValueFrom.ConfigMapKeyRef.Name == cw.Spec.ConfigMapToWatch {
					//log.Info("Find pod that uses the configmap: " + pod.Name)

					podsUseConfigMap = append(podsUseConfigMap, pod.Name)
					podFoundForConfigMap = true // signal a pod was found and break out the inner for loop
					break
				}
			}
		}
	}

	for _, pod := range podList.Items {
		for _, container := range pod.Spec.Containers {
			// if a pod is found, reset the found flag and move to the next pod
			if podFoundForSecret {
				podFoundForSecret = false
				break
			}
			for _, env := range container.Env {
				if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == cw.Spec.SecretToWatch {
					//log.Info("Find pod that uses the secret: " + pod.Name)

					podsUseSecret = append(podsUseSecret, pod.Name)
					podFoundForSecret = true // signal a pod was found and break out the inner for loop
					break
				}
			}
		}
	}
	return podsUseConfigMap, podsUseSecret
}
