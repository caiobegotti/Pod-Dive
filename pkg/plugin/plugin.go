package plugin

import (
	"strings"

	"github.com/caiobegotti/pod-dive/pkg/logger"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

/*
time comparison: N kubectls versus pod-dive
real	0m1.647s
user	0m0.134s
sys	0m0.043s


feature: all info needed instantly shown in a single term scroll on macos default window
*/

// INPUT: pod name

// DO:
//		1.0 traverse all namespaces OK
//		1.0 find pod OK
//		1.0 get its namespace: metadata.namespace OK
//		1.0 get its node: spec.nodeName OK
//		2.0 pass namespace as option
//		2.0 get its workload: metadata.ownerReferences.kind,.name OK

// OUTPUT:
// 		1.0 master node or not
//		1.0 healthy node or not
//		1.0 pods in node
//		2.0 pod cpu/mem usage
//		2.0 node cpu/mem usage

// TEST:
//		1.0 krew flags
//		1.0 namespace
//		1.0 no pod name
//		1.0 non existant pod name
//		1.0 node not ready
//		1.0 pod from a job/cron
//		1.0 pod from sts
//		1.0 pod from deploy
//		1.0 manual pod
//		1.0 bad chars in pod name (space, with quotes, escaping, @#$%=)
//		1.0 with and without init containers
//		1.0 containers with and without restarts
//		1.0 pod with and without terminated

func RunPlugin(configFlags *genericclioptions.ConfigFlags, outputCh chan string) error {
	config, err := configFlags.ToRESTConfig()
	if err != nil {
		return errors.Wrap(err, "Failed to read kubeconfig")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.Wrap(err, "Failed to create API clientset")
	}

	log := logger.NewLogger()

	podName := <-outputCh
	podField := "metadata.name=" + podName
	log.Instructions("Diving after %s:", podName)

	// BEGIN tree separator
	log.Instructions("")

	podFind, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: podField})
	if err != nil {
		return errors.Wrap(err, "Failed to list cluster pods")
	} else if len(podFind.Items) == 0 {
		log.Instructions("Expected items in pods data but got nothing")
	}

	podObject, err := clientset.CoreV1().Pods(podFind.Items[0].Namespace).Get(podFind.Items[0].Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "Failed to get pod info")
	}

	nodeObject, err := clientset.CoreV1().Nodes().Get(podObject.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "Failed to get nodes info")
	}

	nodeField := "spec.nodeName=" + nodeObject.Name
	nodePods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: nodeField})
	if err != nil {
		return errors.Wrap(err, "Failed to get sibling pods info")
	}

	var nodeCondition string
	nodeConditions := nodeObject.Status.Conditions
	for _, condition := range nodeConditions {
		if condition.Type == "Ready" {
			if condition.Status == "False" {
				nodeCondition = "not ready"
			} else if condition.Status == "Unknown" {
				nodeCondition = "unknown state"
			} else {
				nodeCondition = "ready"
			}
		}
	}

	nodeLabels := nodeObject.ObjectMeta.GetLabels()
	if nodeLabels["kubernetes.io/role"] == "master" {
		log.Instructions("[node]      %s (%s, %s)", podObject.Spec.NodeName, nodeLabels["kubernetes.io/role"], nodeCondition)
	} else {
		log.Instructions("[node]      %s (%s)", podObject.Spec.NodeName, nodeCondition)
	}

	// if ReplicaSet, go over it all again
	for _, existingOwnerRef := range podObject.GetOwnerReferences() {
		log.Instructions("[namespace]    ├─┬─ %s", podObject.Namespace)
		log.Instructions("[type]         │ └─┬─ %s", strings.ToLower(existingOwnerRef.Kind))
		log.Instructions("[workload]     │   └─┬─ %s", existingOwnerRef.Name)
		log.Instructions("[pod]          │     └─┬─ %s", podName)

		for num, val := range podObject.Status.ContainerStatuses {
			if num == 0 {
				// print header if start of the tree
				if num == len(podObject.Status.ContainerStatuses)-1 {
					// terminate ascii tree if this is the last item
					if val.RestartCount == 1 {
						// with singular
						log.Instructions("[containers]   │       └─── %s (%d restart)", val.Name, val.RestartCount)
					} else {
						// with plural
						log.Instructions("[containers]   │       └─── %s (%d restarts)", val.Name, val.RestartCount)
					}
				} else {
					// connect the ascii tree to next link
					if val.RestartCount == 1 {
						log.Instructions("[containers]   │       ├─── %s (%d restart)", val.Name, val.RestartCount)
					} else {
						log.Instructions("[containers]   │       ├─── %s (%d restarts)", val.Name, val.RestartCount)
					}
				}
			} else {
				// clean tree space for N itens
				if num == len(podObject.Status.ContainerStatuses)-1 {
					if len(podObject.Spec.InitContainers) == 0 {
						if val.RestartCount == 1 {
							log.Instructions("               │       └─── %s (%d restart)", val.Name, val.RestartCount)
						} else {
							log.Instructions("               │       └─── %s (%d restarts)", val.Name, val.RestartCount)
						}
					} else {
						if val.RestartCount == 1 {
							log.Instructions("               │       ├─── %s (%d restart)", val.Name, val.RestartCount)
						} else {
							log.Instructions("               │       ├─── %s (%d restarts)", val.Name, val.RestartCount)
						}
					}
				} else {
					if val.RestartCount == 1 {
						log.Instructions("               │       ├─── %s (%d restart)", val.Name, val.RestartCount)
					} else {
						log.Instructions("               │       ├─── %s (%d restarts)", val.Name, val.RestartCount)
					}
				}
			}
		}

		// no need to manually link init containers as there will
		// always be at leats one container inside the pod above
		// so they can all be appended here in the ascii tree safely
		for num, val := range podObject.Status.InitContainerStatuses {
			if num == len(podObject.Status.InitContainerStatuses)-1 {
				if val.RestartCount == 1 {
					log.Instructions("               │       └─── %s (init, %d restart)", val.Name, val.RestartCount)
				} else {
					log.Instructions("               │       └─── %s (init, %d restarts)", val.Name, val.RestartCount)
				}
			} else {
				if val.RestartCount == 1 {
					log.Instructions("               │       ├─── %s (init, %d restart)", val.Name, val.RestartCount)
				} else {
					log.Instructions("               │       ├─── %s (init, %d restarts)", val.Name, val.RestartCount)
				}
			}
		}
	}

	allNodePods := nodePods.Items
	siblingsPods := []string{}

	for _, val := range allNodePods {
		if val.GetName() != podName {
			siblingsPods = append(siblingsPods, val.GetName())
		}
	}

	for num, val := range siblingsPods {
		if num == 0 {
			if num == len(siblingsPods)-1 {
				log.Instructions("[siblings]     └─── %s", val)
			} else {
				log.Instructions("[siblings]     ├─── %s", val)
			}
		} else {
			if num == len(siblingsPods)-1 {
				log.Instructions("               └─── %s", val)
			} else {
				log.Instructions("               ├─── %s", val)
			}
		}
	}

	// END tree separator
	log.Instructions("")

	log.Instructions("Last terminations:")
	for _, containerStatuses := range podObject.Status.ContainerStatuses {
		if containerStatuses.LastTerminationState.Terminated != nil {
			if containerStatuses.LastTerminationState.Terminated.Reason != "Completed" {
				log.Instructions("    %s %s (code %d)", containerStatuses.Name, strings.ToLower(containerStatuses.LastTerminationState.Terminated.Reason), containerStatuses.LastTerminationState.Terminated.ExitCode)
			}
		}
	}

	/*
		pod last terminated
		pod health
		workload replicas
		workload name
		node labels + annotations
		containers status
	*/

	// 200ms more to show this
	conditions := nodeObject.Status.Conditions
	for _, condition := range conditions {
		if condition.Type == "Ready" {
			if condition.Status == "False" {
				log.Instructions("Node: not ready")
			} else if condition.Status == "Unknown" {
				log.Instructions("Node: unknown condition")
			} else {
				log.Instructions("Node: ready")
			}
		}
	}

	return nil
}
