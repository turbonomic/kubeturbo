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
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"github.com/turbonomic/kubeturbo/cmd/kubeturbo/app"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
)

// Initialize logs with the following steps:
// - Merge glog and klog flags into goflag FlagSet
// - Add the above merged goflag set into pflag CommandLine FlagSet
// - Add kubeturbo flags into pflag CommandLine FlagSet
// - Parse pflag FlagSet:
//     - goflag FlagSet will be parsed first
//     - pflag FlagSet will be parsed next
// - Sync those glog flags that also appear in klog flags
//
// Return log flush frequency
func initLogs(s *app.VMTServer) *time.Duration {
	// Change default behavior: log to both stderr and /var/log/
	// These arguments can be overwritten from the command-line args
	_ = goflag.Set("alsologtostderr", "true")
	_ = goflag.Set("log_dir", "/var/log")

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

	// Add log flush frequency
	logFlushFreq := goflag.Duration("log-flush-frequency", 5*time.Second,
		"Maximum number of seconds between log flushes")

	// Add goflag to pflag
	// During pflag.Parse(), all goflag will be parsed using goflag.Parse()
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	// Add kubeturbo specific flags to pflag
	s.AddFlags(pflag.CommandLine)

	// We have all the defined flags, now parse it
	pflag.CommandLine.SetNormalizeFunc(wordSepNormalizeFunc)
	pflag.Parse()

	// Sync the glog and klog flags
	pflag.CommandLine.VisitAll(func(glogFlag *pflag.Flag) {
		klogFlag := klogFlags.Lookup(glogFlag.Name)
		if klogFlag != nil {
			value := glogFlag.Value.String()
			_ = klogFlag.Value.Set(value)
		}
	})

	// Print out all parsed flags
	pflag.VisitAll(func(flag *pflag.Flag) {
		glog.V(2).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})

	return logFlushFreq
}

// WordSepNormalizeFunc changes all flags that contain "_" separators
func wordSepNormalizeFunc(f *pflag.FlagSet, name string) pflag.NormalizedName {
	if strings.Contains(name, "_") {
		return pflag.NormalizedName(strings.Replace(name, "_", "-", -1))
	}
	return pflag.NormalizedName(name)
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	s := app.NewVMTServer()

	logFlushFreq := initLogs(s)

	// Launch separate goroutines to flush glog and klog
	go wait.Forever(klog.Flush, *logFlushFreq)
	go wait.Forever(glog.Flush, *logFlushFreq)
	defer klog.Flush()
	defer glog.Flush()

	glog.Infof("Run Kubeturbo service (GIT_COMMIT: %s)", os.Getenv("GIT_COMMIT"))

	s.Run()
}
