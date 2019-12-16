package plugin

import (
	"strings"

	"github.com/caiobegotti/pod-dive/pkg/logger"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

// TESTS
//
// krew flags
// namespace
// no pod name
// non existant pod name
// node not ready
// pod from a job/cron
// pod from sts
// pod from deploy
// manual pod
// bad chars in pod name (space, with quotes, escaping, @#$%=)
// with and without init containers
// containers with and without restarts
// pod with and without terminated
// kube-proxy-gke-staging-default-pool-acca72c6-klsn container
// 2 pods with the same name, different namespace

func RunPlugin(configFlags *genericclioptions.ConfigFlags, outputChan chan string) error {
	config, err := configFlags.ToRESTConfig()
	if err != nil {
		return errors.Wrap(err, "Failed to read kubeconfig, exiting.")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.Wrap(err, "Failed to create API clientset")
	}

	log := logger.NewLogger()

	podName := <-outputChan
	podFieldSelector := "metadata.name=" + podName
	log.Info("Diving after pod %s:", podName)

	// BEGIN tree separator
	log.Info("")

	// seek the whole cluster, in all namespaces, for the pod name
	podFind, err := clientset.CoreV1().Pods("").List(
		metav1.ListOptions{FieldSelector: podFieldSelector})
	if err != nil || len(podFind.Items) == 0 {
		return errors.Wrap(err,
			"Failed to list cluster pods, set a config context or verify the API server.")
	}

	// we can save one API call here, making it much faster and smaller, hopefully
	// podObject, err := clientset.CoreV1().Pods(podFind.Items[0].Namespace).Get(
	// 	podFind.Items[0].Name, metav1.GetOptions{})
	// if err != nil {
	// 	return errors.Wrap(err, "Failed to get pod info")
	// }
	podObject := podFind.Items[0]

	// basically to create the ascii tree of siblings below
	nodeObject, err := clientset.CoreV1().Nodes().Get(
		podObject.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err,
			"Failed to get nodes info, verify the connection to their pool.")
	}

	nodeFieldSelector := "spec.nodeName=" + nodeObject.Name
	nodePods, err := clientset.CoreV1().Pods("").List(
		metav1.ListOptions{FieldSelector: nodeFieldSelector})
	if err != nil {
		return errors.Wrap(err,
			"Failed to get sibling pods info, API server could not be reached.")
	}

	// this will be used to show whether the pod is running inside a master node or not
	nodeLabels := nodeObject.ObjectMeta.GetLabels()

	var nodeCondition string
	nodeConditions := nodeObject.Status.Conditions

	// we only care about the critical ones here
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

	// i like how ascii tree easily convey meaning, the hierarchy of objects inside
	// the cluster and it looks cool :-) i just am not so sure about how to present
	// secondary info such as restart counts or status along these, as well the headers
	// of each level... at least currently it's quite doable to strip them out with
	// sed as they are always grouped by either [] or () so the actual tree is intact
	if nodeLabels["kubernetes.io/role"] == "master" {
		log.Info("[node]      %s [%s, %s]",
			podObject.Spec.NodeName,
			nodeLabels["kubernetes.io/role"],
			nodeCondition)
	} else {
		log.Info("[node]      %s [%s]",
			podObject.Spec.NodeName,
			nodeCondition)
	}
	// FIXME: if ReplicaSet, go over it all again
	log.Info("[namespace]    ├─┬─ %s", podObject.Namespace)

	if podObject.GetOwnerReferences() == nil {
		log.Info("[type]         │ └─┬─ pod")
		log.Info("[workload]     │   └─┬─ [no replica set]]")
	} else {
		for _, existingOwnerRef := range podObject.GetOwnerReferences() {
			if strings.ToLower(existingOwnerRef.Kind) == "replicaset" {
				rsObject, err := clientset.AppsV1().ReplicaSets(
					podObject.GetNamespace()).Get(
					existingOwnerRef.Name,
					metav1.GetOptions{})
				if err != nil {
					return errors.Wrap(err,
						"Failed to retrieve replica sets data, AppsV1 API was not available.")
				}

				log.Info("[type]         │ └─┬─ %s", strings.ToLower(existingOwnerRef.Kind))
				if rsObject.Status.Replicas == 1 {
					log.Info("[workload]     │   └─┬─ %s [%d replica]",
						existingOwnerRef.Name,
						rsObject.Status.Replicas)
				} else {
					log.Info("[workload]     │   └─┬─ %s [%d replicas]",
						existingOwnerRef.Name,
						rsObject.Status.Replicas)
				}
			} else {
				log.Info("[type]         │ └─┬─ %s", strings.ToLower(existingOwnerRef.Kind))
				log.Info("[workload]     │   └─┬─ %s [? replicas]", existingOwnerRef.Name)
			}
		}
	}

	// we have to convert v1.PodPhase to string first, before we lowercase it
	log.Info("[pod]          │     └─┬─ %s [%s]",
		podObject.GetName(),
		strings.ToLower(string(podObject.Status.Phase)))

	for num, val := range podObject.Status.ContainerStatuses {
		if num == 0 {
			// print header if start of the tree
			if num == len(podObject.Status.ContainerStatuses)-1 {
				// terminate ascii tree if this is the last item
				if val.RestartCount == 1 {
					// with singular
					log.Info("[containers]   │       └─── %s [%d restart]", val.Name, val.RestartCount)
				} else {
					// with plural
					log.Info("[containers]   │       └─── %s [%d restarts]", val.Name, val.RestartCount)
				}
			} else {
				// connect the ascii tree with next link
				if val.RestartCount == 1 {
					log.Info("[containers]   │       ├─── %s [%d restart]", val.Name, val.RestartCount)
				} else {
					log.Info("[containers]   │       ├─── %s [%d restarts]", val.Name, val.RestartCount)
				}
			}
		} else {
			// clean tree space for N itens
			if num == len(podObject.Status.ContainerStatuses)-1 {
				if len(podObject.Spec.InitContainers) == 0 {
					if val.RestartCount == 1 {
						log.Info("               │       └─── %s [%d restart]", val.Name, val.RestartCount)
					} else {
						log.Info("               │       └─── %s [%d restarts]", val.Name, val.RestartCount)
					}
				} else {
					if val.RestartCount == 1 {
						log.Info("               │       ├─── %s [%d restart]", val.Name, val.RestartCount)
					} else {
						log.Info("               │       ├─── %s [%d restarts]", val.Name, val.RestartCount)
					}
				}
			} else {
				if val.RestartCount == 1 {
					log.Info("               │       ├─── %s [%d restart]", val.Name, val.RestartCount)
				} else {
					log.Info("               │       ├─── %s [%d restarts]", val.Name, val.RestartCount)
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
				log.Info("               │       └─── %s [init, %d restart]", val.Name, val.RestartCount)
			} else {
				log.Info("               │       └─── %s [init, %d restarts]", val.Name, val.RestartCount)
			}
		} else {
			if val.RestartCount == 1 {
				log.Info("               │       ├─── %s [init, %d restart]", val.Name, val.RestartCount)
			} else {
				log.Info("               │       ├─── %s [init, %d restarts]", val.Name, val.RestartCount)
			}
		}
	}

	siblingsPods := []string{}
	for _, val := range nodePods.Items {
		// remove its own name from the node pods list
		if val.GetName() != podObject.GetName() {
			siblingsPods = append(siblingsPods, val.GetName())
		}
	}

	// the purpose of having a tree of all siblings pods of the desired node
	// is that there are scenarios where your pod should not be running
	// next to other critical or broken workloads inside the same node, so
	// knowing what else is next to your pod is quite helpful when you
	// are planning affinities and selectors
	for num, val := range siblingsPods {
		if num == 0 {
			if num == len(siblingsPods)-1 {
				log.Info("[siblings]     └─── %s", val)
			} else {
				log.Info("[siblings]     ├─── %s", val)
			}
		} else {
			if num == len(siblingsPods)-1 {
				log.Info("               └─── %s", val)
			} else {
				log.Info("               ├─── %s", val)
			}
		}
	}

	// END tree separator
	log.Info("")

	// basic reasons for pods not being in a running state
	for _, containerStatuses := range podObject.Status.ContainerStatuses {
		if containerStatuses.LastTerminationState.Waiting != nil {
			log.Info("Stuck:")
			log.Info("    %s %s [code %s]",
				containerStatuses.Name,
				strings.ToLower(containerStatuses.LastTerminationState.Waiting.Reason),
				containerStatuses.LastTerminationState.Waiting.Message)

		}

		if containerStatuses.LastTerminationState.Terminated != nil {
			if containerStatuses.LastTerminationState.Terminated.Reason != "Completed" {
				log.Info("Terminations:")

				log.Info("    %s %s [code %d]",
					containerStatuses.Name,
					strings.ToLower(containerStatuses.LastTerminationState.Terminated.Reason),
					containerStatuses.LastTerminationState.Terminated.ExitCode)
			}
		}
	}

	return nil
}
