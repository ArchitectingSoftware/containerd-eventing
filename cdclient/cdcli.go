package cdclient

import (
	"context"
	"fmt"
	"log"

	"github.com/containerd/containerd"
	cdevnt "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/api/services/tasks/v1"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl/v2"
)

const (
	defaultContainerdPath = "/run/containerd/containerd.sock"
	defaultNamespace      = "k8s.io"
	containerTypeKey      = "io.cri-containerd.kind"
	k8sPodNameKey         = "io.kubernetes.pod.name"
	k8sNamespaceKey       = "io.kubernetes.pod.namespace"
	k8sContainerNameKey   = "io.kubernetes.container.name"
	isContainerKey        = "container"
	isSandboxKey          = "sandbox"
)

type CdContainerType int

const (
	CD_TYPE_CONTAINER CdContainerType = iota
	CD_TYPE_K8SCONTAINER
	CD_TYPE_SANDBOX
	CD_TYPE_K8SSANDBOX
	CD_TYPE_UNKNOWN
)

type ContainerDCli struct {
	sockpath  string
	namespace string

	client      *containerd.Client
	ctx         context.Context
	ctrsvc      containers.Store
	evntsvc     containerd.EventService
	tasksvc     tasks.TasksClient
	stopWatcher chan error

	db *ContainerMap
}

func NewClient() (*ContainerDCli, error) {
	return NewClientWithConfig(defaultContainerdPath, defaultNamespace)
}

func NewClientWithConfig(socket string, namespace string) (*ContainerDCli, error) {

	cdcli, err := containerd.New(socket)
	if err != nil {
		log.Printf("error connecting to containerd daemon: %v", err)
		return nil, err
	}

	ctx := namespaces.WithNamespace(context.Background(), namespace)

	version, err := cdcli.Version(ctx)
	if err != nil {
		log.Printf("error retrieving containerd version: %v", err)
		return nil, err
	}

	log.Printf("containerd version (daemon: %s [Revision: %s])\n", version.Version, version.Revision)

	//Prehook some services for our usage later
	ctrsvc := cdcli.ContainerService()
	evntsvc := cdcli.EventService()
	tasksvc := cdcli.TaskService()

	ccli := &ContainerDCli{
		sockpath:    socket,
		namespace:   namespace,
		client:      cdcli,
		ctx:         ctx,
		ctrsvc:      ctrsvc,
		evntsvc:     evntsvc,
		tasksvc:     tasksvc,
		stopWatcher: make(chan error),
		db:          NewContainerMap(),
	}

	//Lets preload the existing containers
	ccli.getAllContainers()
	ccli.db.PrintDB()

	return ccli, nil

}

func GetNamespaces() ([]string, error) {
	return GetNamespacesWithSock(defaultContainerdPath)
}
func GetNamespacesWithSock(socket string) ([]string, error) {
	cdcli, err := containerd.New(socket)
	if err != nil {
		log.Printf("error connecting to containerd daemon: %v", err)
		return nil, err
	}

	ctx := context.TODO()
	nsStore := cdcli.NamespaceService()
	return nsStore.List(ctx)
}

func (c *ContainerDCli) ErrorWatcher() chan error {
	return c.stopWatcher
}

// Stop stop watching events
func (c *ContainerDCli) Stop() error {
	c.stopWatcher <- nil
	//wait for any cleanup
	return <-c.stopWatcher
}

func (c *ContainerDCli) Start() error {
	go c.monitorContainerEvents()
	return nil
}

func (c *ContainerDCli) monitorContainerEvents() {
	filters := []string{
		`topic=="/tasks/start"`,
		`topic=="/tasks/delete"`,
	}
	ch, errs := c.evntsvc.Subscribe(c.ctx, filters...)

	for {
		select {
		case event := <-ch:

			evnt, err := typeurl.UnmarshalAny(event.Event)
			if err != nil {
				fmt.Println("got eror unmarshalling event ", err)
				continue
			}

			//fmt.Println("TOPIC", event.Topic)

			switch containerEvent := evnt.(type) {
			case *cdevnt.TaskStart:
				ct, k8smd, err := c.containerType(containerEvent.GetContainerID())
				if err != nil {
					//this should not happen, unknown container type or
					//container id, just continue
					continue
				}

				switch ct {
				case CD_TYPE_CONTAINER:
					log.Println("NEW CONTAINER STARTED, PID=", containerEvent.Pid)
					c.db.AddTaskK8s(containerEvent.ContainerID, containerEvent.Pid, k8smd)
					c.db.PrintDB()
				//just ignore these unless you want to hook them
				case CD_TYPE_SANDBOX:
				case CD_TYPE_UNKNOWN:
					continue
				default:
					continue
				}
			case *cdevnt.TaskDelete:
				//Note below we dont care about the k8s metadata, if it
				//exits or doesnt
				ct, _, err := c.containerType(containerEvent.GetContainerID())
				if err != nil {
					//this should not happen, unknown container type or
					//container id, just continue
					continue
				}

				switch ct {
				case CD_TYPE_CONTAINER:
					log.Println("DELETEING CONTAINER, PID=", containerEvent.Pid)
					c.db.RemoveTask(containerEvent.ContainerID, containerEvent.Pid)
					c.db.PrintDB()
				case CD_TYPE_SANDBOX:
				case CD_TYPE_UNKNOWN:
					continue
				default:
					continue
				}
			default:
				//log and continue
				//You can captures these if you open up or remove the filters
				log.Println("EVENT", event.Event.GetTypeUrl())
			}

		case err := <-errs:
			fmt.Println("ERROR EVENT ", err)
			return

		case <-c.stopWatcher:

			fmt.Println("received stop event clean up and exiting")
			c.client.Close()
			c.stopWatcher <- nil
			return
		}

	}
}

func (c *ContainerDCli) getAllContainers() error {
	clist, _ := c.ctrsvc.List(c.ctx)
	for idx, ctr := range clist {
		lbls := ctr.Labels

		//Only deal with container vs sandbox containers
		if lbls[containerTypeKey] == isContainerKey {
			//check if its kubernetes or not

			var k8s *K8sMetadata = nil
			k8sPodName, okp := lbls[k8sPodNameKey]
			k8sNamespace, okn := lbls[k8sNamespaceKey]
			k8sContainerName, okc := lbls[k8sContainerNameKey]

			if !okp || !okn || !okc {
				log.Println("Not kubernetes")
				k8s = nil
			} else {
				k8s = &K8sMetadata{
					PodName:       k8sPodName,
					PodNamespace:  k8sNamespace,
					ContainerName: k8sContainerName,
				}
			}

			//We have a container lets see if it has running tasks, it might
			//be idle - aka all tasks ended
			tlr := &tasks.ListPidsRequest{
				ContainerID: ctr.ID,
			}

			tlrsp, _ := c.tasksvc.ListPids(c.ctx, tlr)
			if tlrsp == nil {
				//Just assume this is a container with no running processes
				//if there is a better way its a TODO
				continue
			}

			piList := tlrsp.Processes
			if len(piList) == 0 {
				//dont know why this would happen, but just in case
				continue
			}
			piUintList := make([]uint32, len(piList))

			for idx, p := range piList {
				piUintList[idx] = p.Pid
			}

			c.db.AddContainerWithTasksK8s(ctr.ID, piUintList, k8s)
			log.Printf("C[%d] PIL:%v\n", idx, piList)
		}

	}
	return nil
}

func (c *ContainerDCli) getPidsFromCid(cid string) ([]uint32, error) {
	tlr := &tasks.ListPidsRequest{
		ContainerID: cid,
	}

	tlrsp, err := c.tasksvc.ListPids(c.ctx, tlr)
	if err != nil {
		log.Println("Could not get pid for container", cid, ":", err)
		return nil, err
	}
	plist := tlrsp.Processes

	pidList := make([]uint32, len(plist))
	for idx, p := range plist {
		pidList[idx] = p.GetPid()
	}

	return pidList, nil
}

func (c *ContainerDCli) containerType(cid string) (CdContainerType, *K8sMetadata, error) {
	ctr, err := c.ctrsvc.Get(c.ctx, cid)
	if err != nil {
		log.Println("got eror lookin up container", err)
		return CD_TYPE_UNKNOWN, nil, err
	}

	//Now lets determine the container type
	cType := ctr.Labels[containerTypeKey]

	//Now lets see if its kubernetes as well
	k8sPodName, okp := ctr.Labels[k8sPodNameKey]
	k8sNamespace, okn := ctr.Labels[k8sNamespaceKey]
	k8sContainerName, okc := ctr.Labels[k8sContainerNameKey]
	isK8s := false
	if okp && okn && okc {
		isK8s = true
	}

	var k8s *K8sMetadata = nil

	if isK8s {
		k8s = &K8sMetadata{
			PodName:       k8sPodName,
			PodNamespace:  k8sNamespace,
			ContainerName: k8sContainerName,
		}
	}

	switch cType {
	case isContainerKey:
		if isK8s {
			return CD_TYPE_K8SCONTAINER, k8s, nil
		}
		return CD_TYPE_CONTAINER, nil, nil
	case isSandboxKey:
		if isK8s {
			return CD_TYPE_K8SSANDBOX, k8s, nil
		}
		return CD_TYPE_SANDBOX, nil, nil
	default:
		return CD_TYPE_UNKNOWN, nil, nil
	}
}
