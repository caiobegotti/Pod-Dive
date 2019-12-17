package main

import (
	"github.com/caiobegotti/pod-dive/cmd/plugin/cli"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // required for GKE
)

func main() {
	cli.InitAndExecute()
}
