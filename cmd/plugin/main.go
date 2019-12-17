package main

import (
	"github.com/caiobegotti/pod-dive/cmd/plugin/cli"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // "gcp" was supposedly necessary for GKE, but it's not
)

func main() {
	cli.InitAndExecute()
}
