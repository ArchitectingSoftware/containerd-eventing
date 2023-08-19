/*
Copyright 2016 The Kubernetes Authors.

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

// Note: the example only works with the code within the same release/branch.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/architectingsoftware/cdevents/cdclient"
)

var (
	containerdPathFlag string
	namespaceFlag      string
	watchChangesFlag   bool
)

func processCmdLineFlags() {
	flag.StringVar(&containerdPathFlag, "p", "/run/containerd/containerd.sock", "Path to containerd sock")
	flag.StringVar(&namespaceFlag, "n", "k8s.io", "Set containerd namespace")
	flag.BoolVar(&watchChangesFlag, "w", true, "Watch for changes with -w=true|false")
	listNS := flag.Bool("list-namespaces", false, "List containerd namespaces")
	wantsHelp := flag.Bool("h", false, "Get help with command line options")

	flag.Parse()
	if *wantsHelp {
		flag.Usage()
		os.Exit(0)
	}

	if *listNS {
		nsList, err := cdclient.GetNamespaces()
		if err != nil {
			log.Println("error getting containerd namespaces:", err)
			os.Exit(-1)
		}
		if len(nsList) == 0 {
			log.Println("There are no available containerd namespaces")
		} else {
			log.Println("Available Namespaces...")
			for _, ns := range nsList {
				log.Println("\tNamespace: ", ns)
			}
		}
		os.Exit(0)
	}
}

func envVarOrDefault(envVar string, defaultVal string) string {
	envVal := os.Getenv(envVar)
	if envVal != "" {
		return envVal
	}
	return defaultVal
}

func setupParms() {
	//first process any command line flags
	processCmdLineFlags()

	//now process any environment variables, override defaults or command
	//line flags, enviornment variables take top priority
	containerdPathFlag = envVarOrDefault("CONTAINERD_SOCK_PATH", containerdPathFlag)
	namespaceFlag = envVarOrDefault("CONTAINERD_NAMESPACE", namespaceFlag)
}

func main() {
	setupParms()

	c, err := cdclient.NewClientWithConfig(containerdPathFlag, namespaceFlag)
	if err != nil {
		log.Println("error starting containerd client:", err)
	}

	if !watchChangesFlag {
		//We just want to list existing containers, which is done by the
		//constructor so we just exit here
		os.Exit(0)
	}

	ew := c.ErrorWatcher()
	c.Start()

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("Blocking, press ctrl+c to continue...")

	for {
		select {
		case <-done:
			log.Println("received termination signal")
			c.Stop()
			os.Exit(0)
		case e := <-ew:
			log.Println("Error from error watcher:", e)
		}
	}

}
