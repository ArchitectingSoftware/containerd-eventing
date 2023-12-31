package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/architectingsoftware/cdevents/apiserver"
	"github.com/architectingsoftware/cdevents/cdclient"
)

var (
	containerdPathFlag string
	namespaceFlag      string
	watchChangesFlag   bool
	serverHostFlag     string
	serverPortFlag     int
	serverRunMode      bool
)

func processCmdLineFlags() {
	flag.StringVar(&containerdPathFlag, "p", "/run/containerd/containerd.sock", "Path to containerd sock")
	flag.StringVar(&namespaceFlag, "n", "k8s.io", "Set containerd namespace")
	flag.BoolVar(&watchChangesFlag, "w", true, "Watch for changes with -w=true|false")
	listNS := flag.Bool("list-namespaces", false, "List containerd namespaces")

	flag.StringVar(&serverHostFlag, "server-host", "0.0.0.0", "API server host name")
	flag.IntVar(&serverPortFlag, "server-port", 10080, "API server port")
	flag.BoolVar(&serverRunMode, "server", false, "Start as API Server (true) vs CLI(false) -server=true|false")

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

func envVarOrDefaultInt(envVar string, defaultVal int) int {
	envVal := os.Getenv(envVar)
	if envVal != "" {
		p64, err := strconv.ParseInt(envVal, 0, 32)
		if err != nil {
			log.Println("Error convertingn server port environment var to int:", err)
			return defaultVal
		}
		return int(p64)
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
	serverHostFlag = envVarOrDefault("CONTAINERD_SVR_HOST", serverHostFlag)
	serverPortFlag = envVarOrDefaultInt("CONTAINERD_SVR_PORT", serverPortFlag)
}

func startSvrMode(c *cdclient.ContainerDCli) {
	apiCfg := &apiserver.ApiConfig{
		Sockpath:  containerdPathFlag,
		Namespace: namespaceFlag,
		HostName:  serverHostFlag,
		Port:      serverPortFlag,
	}

	apiServer, err := apiserver.NewApiServerWithConfig(apiCfg)
	if err != nil {
		log.Println("error creating apiserver, err:", err)
		return
	}
	err = apiServer.Run()
	if err != nil {
		log.Println("error starting apiserver, err:", err)
		return
	}
}

func startCliMode(c *cdclient.ContainerDCli) {
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

func main() {
	setupParms()

	c, err := cdclient.NewClientWithConfig(containerdPathFlag, namespaceFlag)
	if err != nil {
		log.Println("error starting containerd client:", err)
	}

	if serverRunMode {
		startSvrMode(c)
	} else {
		startCliMode(c)
	}

}
