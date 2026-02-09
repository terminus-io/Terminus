package main

import (
	"os"

	"github.com/terminus-io/Terminus/pkg/scheduler"
	"k8s.io/component-base/cli"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(scheduler.SchedulerName, scheduler.New),
	)
	code := cli.Run(command)
	os.Exit(code)
}
