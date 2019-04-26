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
	"os"
	"runtime"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"github.com/turbonomic/kubeturbo/cmd/kubeturbo/app"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/util/flag"
	"k8s.io/klog"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Change default goflag: log to both stderr and /var/log/
	// These arguments can be overwritten from the command-line args
	if err := goflag.Set("alsologtostderr", "true"); err != nil {
		glog.Warningf("Failed to set default value for alsologtostderr: %v", err)
	}
	if err := goflag.Set("log_dir", "/var/log"); err != nil {
		glog.Warningf("Failed to set default value for log_dir: %v", err)
	}

	// Initialize klog specific flags into a new FlagSet
	klogFlags := goflag.NewFlagSet("klog", goflag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Add klog specific flags into goflag
	klogFlags.VisitAll(func(klogFlag *goflag.Flag) {
		if goflag.CommandLine.Lookup(klogFlag.Name) == nil {
			// This is a klog specific flag
			goflag.CommandLine.Var(klogFlag.Value, klogFlag.Name, klogFlag.Usage)
		}
	})

	// Convert goflag to pflag
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	// Add kubeturbo specific flags to pflag
	s := app.NewVMTServer()
	s.AddFlags(pflag.CommandLine)

	// Add log flush frequency to pflag
	logFlushFreq := pflag.Duration("log-flush-frequency", 5*time.Second,
		"Maximum number of seconds between log flushes")

	// We have all the defined flags, now parse it
	pflag.CommandLine.SetNormalizeFunc(flag.WordSepNormalizeFunc)
	pflag.Parse()

	// Launch separate goroutines to flush glog and klog
	go wait.Forever(klog.Flush, *logFlushFreq)
	go wait.Forever(glog.Flush, *logFlushFreq)
	defer klog.Flush()
	defer glog.Flush()

	// Print out all parsed flags
	pflag.VisitAll(func(flag *pflag.Flag) {
		glog.V(2).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})

	// Sync the glog and klog flags
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				glog.Warningf("Failed to set value for flag %s: %v", f1.Name, err)
			}
		}
	})

	glog.Infof("Run Kubeturbo service (GIT_COMMIT: %s)", os.Getenv("GIT_COMMIT"))

	s.Run(pflag.CommandLine.Args())
}
