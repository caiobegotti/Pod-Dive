package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/caiobegotti/pod-dive/pkg/plugin"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	KubernetesConfigFlags *genericclioptions.ConfigFlags
)

func RootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pod-dive [pod name]",
		Short: "Dives into a node after the desired pod info.",
		Long: `Dives into a node after the desired pod and returns data associated
with the pod no matter where it is running, such as its origin workload,
namespace, the node where it is running and its node pod siblings, as
well basic health status of it all.

The purpose is to have meaningful pod info at a glance without needing to
run multiple kubectl commands to see what else is running next to your
pod in a given node inside a huge cluster, because sometimes all
you've got from an alert is the pod name.`,
		Example: `
Cluster-wide dive after a pod
$ kubectl pod-dive thanos-store-0

Restricts the dive to a namespace (faster in big clusters)
$ kubectl pod-dive elasticsearch-curator-1576112400-97htk -n logging`,
		SilenceErrors: true,
		SilenceUsage:  false,
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("A pod name is required!")
			}

			podName := args[0]
			argsChannel := make(chan string, 1)
			argsChannel <- podName

			if err := plugin.RunPlugin(KubernetesConfigFlags, argsChannel); err != nil {
				return errors.Cause(err)
			}

			return nil
		},
	}

	cobra.OnInitialize(initConfig)

	KubernetesConfigFlags = genericclioptions.NewConfigFlags(false)
	KubernetesConfigFlags.AddFlags(cmd.Flags())

	// hide common flags supported by any kubectl command to declutter -h/--help
	// most people would only (if ever) miss kubeconfig, context or cluster
	cmd.Flags().MarkHidden("as-group")
	cmd.Flags().MarkHidden("as")
	cmd.Flags().MarkHidden("cache-dir")
	cmd.Flags().MarkHidden("certificate-authority")
	cmd.Flags().MarkHidden("client-certificate")
	cmd.Flags().MarkHidden("client-key")
	cmd.Flags().MarkHidden("cluster")
	cmd.Flags().MarkHidden("context")
	cmd.Flags().MarkHidden("insecure-skip-tls-verify")
	cmd.Flags().MarkHidden("kubeconfig")
	cmd.Flags().MarkHidden("password")
	cmd.Flags().MarkHidden("request-timeout")
	cmd.Flags().MarkHidden("server")
	cmd.Flags().MarkHidden("token")
	cmd.Flags().MarkHidden("user")
	cmd.Flags().MarkHidden("username")

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	return cmd
}

func InitAndExecute() {
	if err := RootCmd().Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func initConfig() {
	viper.AutomaticEnv()
}
