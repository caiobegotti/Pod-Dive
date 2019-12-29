package plugin

import (
	"strings"

	"github.com/caiobegotti/pod-dive/pkg/logger"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// TODO
// test node not ready

type NodeInfo struct {
	Object    *v1.Node
	Pods      *v1.PodList
	Labels    map[string]string
	Condition string
}

type PodDivePlugin struct {
	config    *rest.Config
	Clientset *kubernetes.Clientset
	PodObject *v1.Pod
	Node      *NodeInfo
}

func NewPodDivePlugin(configFlags *genericclioptions.ConfigFlags) (*PodDivePlugin, error) {
	config, err := configFlags.ToRESTConfig()
	if err != nil {
		return nil, errors.New("Failed to read kubeconfig, exiting.")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.New("Failed to create API clientset")
	}

	return &PodDivePlugin{
		config:    config,
		Clientset: clientset,
	}, nil
}

func (pd *PodDivePlugin) findPodByPodName(name string, namespace string) error {
	podFieldSelector := "metadata.name=" + name

	// we will seek the whole cluster if namespace is not passed as a flag (it will be a "" string)
	podFind, err := pd.Clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{FieldSelector: podFieldSelector})
	if err != nil || len(podFind.Items) == 0 {
		return errors.New("Failed to get pod: check your parameters, set a context or verify API server.")
	}

	pd.PodObject = &podFind.Items[0]
	if pd.PodObject.Spec.NodeName == "" || pd.PodObject == nil {
		return errors.New("Pod is not assigned to a node yet, it's still pending scheduling probably.")
	}

	return nil
}

func (pd *PodDivePlugin) findNodeByNodeName() error {
	// basically to create the ascii tree of siblings below
	nodeObject, err := pd.Clientset.CoreV1().Nodes().Get(pd.PodObject.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return errors.New("Failed to get nodes info, verify the connection to their pool.")
	}

	pd.Node = &NodeInfo{
		Object: nodeObject,
	}

	return nil
}

func (pd *PodDivePlugin) getNodeInfo() error {
	nodeFieldSelector := "spec.nodeName=" + pd.Node.Object.Name
	pods, err := pd.Clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: nodeFieldSelector})
	if err != nil {
		return errors.New("Failed to get sibling pods info, API server could not be reached.")
	}

	pd.Node.Pods = pods

	// this will be used to show whether the pod is running inside a master node or not
	pd.Node.Labels = pd.Node.Object.ObjectMeta.GetLabels()

	// we only care about the critical ones here
	for _, condition := range pd.Node.Object.Status.Conditions {
		if condition.Type != "Ready" {
			continue
		}

		switch condition.Status {
		case "False":
			pd.Node.Condition = "not ready"
		case "Unknown":
			pd.Node.Condition = "unknown state"
		default:
			pd.Node.Condition = "ready"
		}
	}

	return nil
}

func RunPlugin(configFlags *genericclioptions.ConfigFlags, outputChan chan string) error {
	pd, err := NewPodDivePlugin(configFlags)
	if err != nil {
		return err
	}

	podName := <-outputChan

	log := logger.NewLogger()

	if err := pd.findPodByPodName(podName, *configFlags.Namespace); err != nil {
		return err
	}

	if err := pd.findNodeByNodeName(); err != nil {
		return err
	}

	if err := pd.getNodeInfo(); err != nil {
		return err
	}

	// i like how ascii tree easily convey meaning, the hierarchy of objects inside
	// the cluster and it looks cool :-) i just am not so sure about how to present
	// secondary info such as restart counts or status along these, as well the headers
	// of each level... at least currently it's quite doable to strip them out with
	// sed as they are always grouped by either [] or () so the actual tree is intact
	if pd.Node.Labels["kubernetes.io/role"] == "master" {
		log.Info("[node]      %s [%s, %s]",
			pd.PodObject.Spec.NodeName,
			pd.Node.Labels["kubernetes.io/role"],
			pd.Node.Condition)
	} else {
		log.Info("[node]      %s [%s]",
			pd.PodObject.Spec.NodeName,
			pd.Node.Condition)
	}
	// FIXME: if ReplicaSet, go over it all again
	log.Info("[namespace]  ├─┬ %s", pd.PodObject.Namespace)

	if pd.PodObject.GetOwnerReferences() == nil {
		log.Info("[type]       │ └─┬ pod")
		log.Info("[workload]   │   └─┬ [no replica set]")
	} else {
		for _, existingOwnerRef := range pd.PodObject.GetOwnerReferences() {
			ownerKind := strings.ToLower(existingOwnerRef.Kind)

			if ownerKind == "replicaset" {
				log.Info("[type]       │ └─┬ %s [deployment]", ownerKind)

				rsObject, err := pd.Clientset.AppsV1().ReplicaSets(
					pd.PodObject.GetNamespace()).Get(
					existingOwnerRef.Name,
					metav1.GetOptions{})
				if err != nil {
					return errors.New(
						"Failed to retrieve replica set data, AppsV1 API was not available.")
				}

				if rsObject.Status.Replicas == 1 {
					log.Info("[workload]   │   └─┬ %s [%d replica]",
						existingOwnerRef.Name,
						rsObject.Status.Replicas)
				} else {
					log.Info("[workload]   │   └─┬ %s [%d replicas]",
						existingOwnerRef.Name,
						rsObject.Status.Replicas)
				}
			} else if ownerKind == "statefulset" {
				log.Info("[type]       │ └─┬ %s", ownerKind)

				ssObject, err := pd.Clientset.AppsV1().StatefulSets(
					pd.PodObject.GetNamespace()).Get(
					existingOwnerRef.Name,
					metav1.GetOptions{})
				if err != nil {
					return errors.New(
						"Failed to retrieve stateful set data, AppsV1 API was not available.")
				}
				if ssObject.Status.Replicas == 1 {
					log.Info("[workload]   │   └─┬ %s [%d replica]",
						existingOwnerRef.Name,
						ssObject.Status.Replicas)
				} else {
					log.Info("[workload]   │   └─┬ %s [%d replicas]",
						existingOwnerRef.Name,
						ssObject.Status.Replicas)
				}
			} else if ownerKind == "daemonset" {
				log.Info("[type]       │ └─┬ %s", ownerKind)

				dsObject, err := pd.Clientset.AppsV1().DaemonSets(
					pd.PodObject.GetNamespace()).Get(
					existingOwnerRef.Name,
					metav1.GetOptions{})
				if err != nil {
					return errors.New(
						"Failed to retrieve daemon set data, AppsV1 API was not available.")
				}
				if dsObject.Status.DesiredNumberScheduled == 1 {
					log.Info("[workload]   │   └─┬ %s [%d replica]",
						existingOwnerRef.Name,
						dsObject.Status.DesiredNumberScheduled)
				} else {
					log.Info("[workload]   │   └─┬ %s [%d replicas]",
						existingOwnerRef.Name,
						dsObject.Status.DesiredNumberScheduled)
				}
			} else {
				log.Info("[type]       │ └─┬ %s", ownerKind)
				log.Info("[workload]   │   └─┬ %s [? replicas]", existingOwnerRef.Name)
			}
		}
	}

	// we have to convert v1.PodPhase to string first, before we lowercase it
	log.Info("[pod]        │     └─┬ %s [%s]",
		pd.PodObject.GetName(),
		strings.ToLower(string(pd.PodObject.Status.Phase)))

	for num, val := range pd.PodObject.Status.ContainerStatuses {
		if num == 0 {
			// print header if start of the tree
			if num == len(pd.PodObject.Status.ContainerStatuses)-1 {
				// terminate ascii tree if this is the last item
				if val.RestartCount == 1 {
					// with singular
					log.Info("[containers] │       └── %s [%d restart]", val.Name, val.RestartCount)
				} else {
					// with plural
					log.Info("[containers] │       └── %s [%d restarts]", val.Name, val.RestartCount)
				}
			} else {
				// connect the ascii tree with next link
				if val.RestartCount == 1 {
					log.Info("[containers] │       ├── %s [%d restart]", val.Name, val.RestartCount)
				} else {
					log.Info("[containers] │       ├── %s [%d restarts]", val.Name, val.RestartCount)
				}
			}
		} else {
			// clean tree space for N itens
			if num == len(pd.PodObject.Status.ContainerStatuses)-1 {
				if len(pd.PodObject.Spec.InitContainers) == 0 {
					if val.RestartCount == 1 {
						log.Info("             │       └── %s [%d restart]", val.Name, val.RestartCount)
					} else {
						log.Info("             │       └── %s [%d restarts]", val.Name, val.RestartCount)
					}
				} else {
					if val.RestartCount == 1 {
						log.Info("             │       ├── %s [%d restart]", val.Name, val.RestartCount)
					} else {
						log.Info("             │       ├── %s [%d restarts]", val.Name, val.RestartCount)
					}
				}
			} else {
				if val.RestartCount == 1 {
					log.Info("             │       ├── %s [%d restart]", val.Name, val.RestartCount)
				} else {
					log.Info("             │       ├── %s [%d restarts]", val.Name, val.RestartCount)
				}
			}
		}
	}

	// no need to manually link init containers as there will
	// always be at leats one container inside the pod above
	// so they can all be appended here in the ascii tree safely
	for num, val := range pd.PodObject.Status.InitContainerStatuses {
		if num == len(pd.PodObject.Status.InitContainerStatuses)-1 {
			if val.RestartCount == 1 {
				log.Info("             │       └── %s [init, %d restart]", val.Name, val.RestartCount)
			} else {
				log.Info("             │       └── %s [init, %d restarts]", val.Name, val.RestartCount)
			}
		} else {
			if val.RestartCount == 1 {
				log.Info("             │       ├── %s [init, %d restart]", val.Name, val.RestartCount)
			} else {
				log.Info("             │       ├── %s [init, %d restarts]", val.Name, val.RestartCount)
			}
		}
	}

	siblingsPods := []string{}
	for _, val := range pd.Node.Pods.Items {
		// remove its own name from the node pods list
		if val.GetName() != pd.PodObject.GetName() {
			siblingsPods = append(siblingsPods, val.GetName())
		}
	}

	// the purpose of having a tree of all siblings pods of the desired node
	// is that there are scenarios where your pod should not be running
	// next to other critical or broken workloads inside the same node, so
	// knowing what else is next to your pod is quite helpful when you
	// are planning affinities and selectors

	// visual separator since siblings may live in different namespaces
	log.Info("            ... ")

	for num, val := range siblingsPods {
		if num == 0 {
			if num == len(siblingsPods)-1 {
				log.Info("[siblings]   └── %s", val)
			} else {
				log.Info("[siblings]   ├── %s", val)
			}
		} else {
			if num == len(siblingsPods)-1 {
				log.Info("             └── %s", val)
			} else {
				log.Info("             ├── %s", val)
			}
		}
	}

	// tree ending separator
	log.Info("")

	// basic reasons for pods not being in a running state
	for _, containerStatuses := range pd.PodObject.Status.ContainerStatuses {
		if containerStatuses.LastTerminationState.Waiting != nil ||
			containerStatuses.State.Waiting != nil {
			log.Info("WAITING:")

			if containerStatuses.LastTerminationState.Waiting != nil {
				log.Info("    %s %s (%s)",
					containerStatuses.Name,
					strings.ToLower(containerStatuses.LastTerminationState.Waiting.Reason),
					containerStatuses.LastTerminationState.Waiting.Message)
			}

			if containerStatuses.State.Waiting != nil {
				log.Info("    %s %s (%s)",
					containerStatuses.Name,
					strings.ToLower(containerStatuses.State.Waiting.Reason),
					containerStatuses.State.Waiting.Message)
			}
		}

		if containerStatuses.LastTerminationState.Terminated != nil {
			if containerStatuses.LastTerminationState.Terminated.Reason != "Completed" {
				log.Info("TERMINATION:")

				log.Info("    %s %s (code %d)",
					containerStatuses.Name,
					strings.ToLower(containerStatuses.LastTerminationState.Terminated.Reason),
					containerStatuses.LastTerminationState.Terminated.ExitCode)
			}
		}
	}

	return nil
}
