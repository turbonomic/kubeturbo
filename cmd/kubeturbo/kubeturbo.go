/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

package main

import (
	goflag "flag"
	"runtime"

	"k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/logs"

	"github.com/turbonomic/kubeturbo/cmd/kubeturbo/app"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"os"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	logs.InitLogs()
	defer logs.FlushLogs()

	// The default is to log to both of stderr and file
	// These arguments can be overloaded from the command-line args
	goflag.Set("logtostderr", "false")
	goflag.Set("alsologtostderr", "true")
	goflag.Set("log_dir", "/var/log")

	s := app.NewVMTServer()
	s.AddFlags(pflag.CommandLine)
	flag.InitFlags()

	glog.Infof("Run Kubeturbo service (GIT_COMMIT: %s)", os.Getenv("GIT_COMMIT"))

	s.Run(pflag.CommandLine.Args())
}
