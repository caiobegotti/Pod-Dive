package plugin

import (
	"strings"

	"github.com/caiobegotti/pod-dive/pkg/logger"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

// INPUT: pod name

// DO:
//		1.0 traverse all namespaces OK
//		1.0 find pod OK
//		1.0 get its namespace: metadata.namespace OK
//		1.0 get its node: spec.nodeName OK
//		2.0 pass namespace as option
//		2.0 get its workload: metadata.ownerReferences.kind,.name OK

// OUTPUT:
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
//		1.0 kube-proxy-gke-staging-default-pool-acca72c6-klsn container
//		1.0 2 pods with the same name, different namespace

func RunPlugin(configFlags *genericclioptions.ConfigFlags, outputChan chan string) error {
	config, err := configFlags.ToRESTConfig()
	if err != nil {
		return errors.Wrap(err, "Failed to read kubeconfig")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.Wrap(err, "Failed to create API clientset")
	}

	log := logger.NewLogger()

	podName := <-outputChan
	podFieldSelector := "metadata.name=" + podName
	log.Instructions("Diving after pod %s:", podName)

	// BEGIN tree separator
	log.Instructions("")

	// seek the whole cluster, in all namespaces, for the pod name
	podFind, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: podFieldSelector})
	if err != nil || len(podFind.Items) == 0 {
		return errors.Wrap(err, "Failed to list cluster pods, set a config context or verify the API server.")
	}

	// we can save one API call here, making it much faster and smaller, hopefully
	// podObject, err := clientset.CoreV1().Pods(podFind.Items[0].Namespace).Get(podFind.Items[0].Name, metav1.GetOptions{})
	// if err != nil {
	// 		return errors.Wrap(err, "Failed to get pod info")
	// 		}
	podObject := podFind.Items[0]

	// basically to create the ascii tree of siblings below
	nodeObject, err := clientset.CoreV1().Nodes().Get(podObject.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "Failed to get nodes info")
	}

	nodeFieldSelector := "spec.nodeName=" + nodeObject.Name
	nodePods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: nodeFieldSelector})
	if err != nil {
		return errors.Wrap(err, "Failed to get sibling pods info")
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
		log.Instructions("[node]      %s [%s, %s]", podObject.Spec.NodeName, nodeLabels["kubernetes.io/role"], nodeCondition)
	} else {
		log.Instructions("[node]      %s [%s]", podObject.Spec.NodeName, nodeCondition)
	}
	// FIXME: if ReplicaSet, go over it all again
	// FIXME: put everything outside getownerreferences()
	// FIXME: log.Info("%s", strings.ToLower(podObject.Status.Phase))
	for _, existingOwnerRef := range podObject.GetOwnerReferences() {
		log.Instructions("[namespace]    ├─┬─ %s", podObject.Namespace)
		log.Instructions("[type]         │ └─┬─ %s", strings.ToLower(existingOwnerRef.Kind))
		log.Instructions("[workload]     │   └─┬─ %s [N replicas]", existingOwnerRef.Name)
		log.Instructions("[pod]          │     └─┬─ %s [%s]", podObject.GetName(), podObject.Status.Phase)

		for num, val := range podObject.Status.ContainerStatuses {
			if num == 0 {
				// print header if start of the tree
				if num == len(podObject.Status.ContainerStatuses)-1 {
					// terminate ascii tree if this is the last item
					if val.RestartCount == 1 {
						// with singular
						log.Instructions("[containers]   │       └─── %s [%d restart]", val.Name, val.RestartCount)
					} else {
						// with plural
						log.Instructions("[containers]   │       └─── %s [%d restarts]", val.Name, val.RestartCount)
					}
				} else {
					// connect the ascii tree with next link
					if val.RestartCount == 1 {
						log.Instructions("[containers]   │       ├─── %s [%d restart]", val.Name, val.RestartCount)
					} else {
						log.Instructions("[containers]   │       ├─── %s [%d restarts]", val.Name, val.RestartCount)
					}
				}
			} else {
				// clean tree space for N itens
				if num == len(podObject.Status.ContainerStatuses)-1 {
					if len(podObject.Spec.InitContainers) == 0 {
						if val.RestartCount == 1 {
							log.Instructions("               │       └─── %s [%d restart]", val.Name, val.RestartCount)
						} else {
							log.Instructions("               │       └─── %s [%d restarts]", val.Name, val.RestartCount)
						}
					} else {
						if val.RestartCount == 1 {
							log.Instructions("               │       ├─── %s [%d restart]", val.Name, val.RestartCount)
						} else {
							log.Instructions("               │       ├─── %s [%d restarts]", val.Name, val.RestartCount)
						}
					}
				} else {
					if val.RestartCount == 1 {
						log.Instructions("               │       ├─── %s [%d restart]", val.Name, val.RestartCount)
					} else {
						log.Instructions("               │       ├─── %s [%d restarts]", val.Name, val.RestartCount)
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
					log.Instructions("               │       └─── %s [init, %d restart]", val.Name, val.RestartCount)
				} else {
					log.Instructions("               │       └─── %s [init, %d restarts]", val.Name, val.RestartCount)
				}
			} else {
				if val.RestartCount == 1 {
					log.Instructions("               │       ├─── %s [init, %d restart]", val.Name, val.RestartCount)
				} else {
					log.Instructions("               │       ├─── %s [init, %d restarts]", val.Name, val.RestartCount)
				}
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

	// basic reasons for pods not being in a running state
	for _, containerStatuses := range podObject.Status.ContainerStatuses {
		if containerStatuses.LastTerminationState.Waiting != nil {
			log.Instructions("Stuck:")
			log.Instructions("    %s %s [code %s]",
				containerStatuses.Name,
				strings.ToLower(containerStatuses.LastTerminationState.Waiting.Reason),
				containerStatuses.LastTerminationState.Waiting.Message)

		}

		if containerStatuses.LastTerminationState.Terminated != nil {
			if containerStatuses.LastTerminationState.Terminated.Reason != "Completed" {
				log.Instructions("Terminations:")

				log.Instructions("    %s %s [code %d]",
					containerStatuses.Name,
					strings.ToLower(containerStatuses.LastTerminationState.Terminated.Reason),
					containerStatuses.LastTerminationState.Terminated.ExitCode)
			}
		}
	}

	return nil
}
