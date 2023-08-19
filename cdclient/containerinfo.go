package cdclient

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type ContainerStatusType int

const (
	C_STATUS_CREATED ContainerStatusType = iota
	C_STATUS_RUNNING
	C_STATS_TERMINATED
	C_STATUS_IDLE
	C_STATUS_UNKNOWN
)

type K8sMetadata struct {
	ContainerName string
	PodName       string
	PodNamespace  string
}

type ContainerData struct {
	Id             string
	Status         ContainerStatusType
	Name           string
	PidList        map[uint32]uint32
	LinuxNS        uint32
	firstPid       uint32 //use to establish namespace, all pids will be in same NS
	isLinuxNsSet   bool
	isK8sContainer bool
	k8sMetadata    *K8sMetadata
}

type ContainerMap struct {
	db map[string]*ContainerData
}

func NewContainerMap() *ContainerMap {
	return &ContainerMap{
		db: make(map[string]*ContainerData),
	}
}

func (c *ContainerMap) RemoveContainer(cid string) error {
	//First lets see if this is an existing container
	_, ok := c.db[cid]
	if !ok {
		return errors.New("trying to delete a container that is unknown")
	}

	delete(c.db, cid)
	return nil
}

func (c *ContainerMap) AddContainerWithTasks(cid string, pidList []uint32) error {
	return c.AddContainerWithTasksK8s(cid, pidList, nil)
}
func (c *ContainerMap) AddContainerWithTasksK8s(cid string, pidList []uint32, k8s *K8sMetadata) error {
	//First lets see if this is an existing container
	_, ok := c.db[cid]
	if ok {
		//This should not happen, cant add a container if it exists
		return errors.New("trying to add a container, but it already exists")
	}

	if len(pidList) == 0 {
		return errors.New("pidList cannot be nil or have zero elements")
	}

	isK8s := false
	if k8s != nil {
		isK8s = true
	}
	ctr := &ContainerData{
		Id:             cid,
		Status:         C_STATUS_RUNNING,
		Name:           "", //TODO: Deal with this later
		PidList:        make(map[uint32]uint32),
		LinuxNS:        0,
		isLinuxNsSet:   false,
		firstPid:       pidList[0],
		isK8sContainer: isK8s,
		k8sMetadata:    k8s,
	}

	//Lets add the PIDs
	for _, p := range pidList {
		ctr.PidList[p] = p
	}
	c.db[cid] = ctr

	return c.postProcessContainerData(ctr)
}
func (c *ContainerMap) AddTask(cid string, pid uint32) error {
	return c.AddTaskK8s(cid, pid, nil)
}
func (c *ContainerMap) AddTaskK8s(cid string, pid uint32, k8s *K8sMetadata) error {
	//First lets see if this is a new task for an unseen container
	ctr, ok := c.db[cid]
	if !ok {
		//Seeing First Task - thus New Container
		isK8s := false
		if k8s != nil {
			isK8s = true
		}
		ci := &ContainerData{
			Id:             cid,
			Status:         C_STATUS_RUNNING,
			Name:           "", //TODO: Deal with this later
			PidList:        make(map[uint32]uint32),
			LinuxNS:        0,
			firstPid:       pid,
			isK8sContainer: isK8s,
			k8sMetadata:    k8s,
		}
		//using a map so we can delete quickly
		ci.PidList[pid] = pid
		c.db[cid] = ci
	} else {
		//Container already exists, just add the PID
		ctr.PidList[pid] = pid
	}

	return c.postProcessContainerData(ctr)
}

func (c *ContainerMap) RemoveTask(cid string, pid uint32) error {
	//To remove a task the container must exist
	ctr, ok := c.db[cid]
	if !ok {
		return errors.New("cant remove a task from unknnown container")
	} else {
		//Container already exists, just add the PID
		_, ok := ctr.PidList[pid]
		//Now we expect to find the task
		if !ok {
			return errors.New("attempting to remove an unknown task from container")
		}
		delete(ctr.PidList, pid)
		//if the number of PIDs is zero, remove the container from the db, its
		//basically idle
		if len(ctr.PidList) == 0 {
			delete(c.db, cid)
		}
	}
	return nil
}

func (c *ContainerMap) PrintDB() {
	if c.db == nil {
		log.Println("DB NIL")
		return
	}
	if len(c.db) == 0 {
		log.Println("database is empty, no running containers...")
	}
	for _, v := range c.db {
		log.Println("CID = ", v.Id)
		log.Println("\tPids = ", v.PidList)
		if v.isLinuxNsSet {
			log.Println("\tLinux NS = ", v.LinuxNS)
		} else {
			log.Println("\tLinux NS = [UNKNOWN]")
		}
		if v.isK8sContainer {
			log.Println("\tK8S CONTAINER = ", v.k8sMetadata.ContainerName)
			log.Println("\tK8S POD = ", v.k8sMetadata.PodName)
			log.Println("\tK8S NAMESPACE = ", v.k8sMetadata.PodNamespace)
		}
	}
}

// This funciton makes sure the ContainerData datastructure has what
// we need, specifically, establishing the linux namespace for the container
func (c *ContainerMap) postProcessContainerData(cd *ContainerData) error {
	if cd.isLinuxNsSet {
		//Once the namespace is set for all tasks it will not change
		//we can just exit
		log.Println("Namespace already set")
		return nil
	}

	if !cd.isLinuxNsSet && len(cd.PidList) == 0 {
		//If the pid list is zero, the isLinuxNsSet should be false
		//all good so just return
		log.Println("Namespace not set, but PidList is empty")
		return nil
	}

	if !cd.isLinuxNsSet && cd.firstPid == 0 {
		//if the namespace is not set, we need to set it up, but this also
		//requires the firstPid value to be set to do this
		log.Println("Namespace not set, but neither is PidList")
		return errors.New("need to determine namespace, but there are no pids to do it from")
	}

	linuxNs, err := getLinuxNsFromPid(cd.firstPid)
	if err != nil {
		//This should be an error, but we just log the situation and leave the
		//state of the datastructure in tact
		log.Println("error getting linux namespace from pid: ", cd.firstPid, " e:", err)
		return nil
	}

	log.Printf("set the linux namespace for pid %d, is %d\n", cd.firstPid, linuxNs)

	cd.LinuxNS = linuxNs
	cd.isLinuxNsSet = true

	return nil
}

func getLinuxNsFromPid(pid uint32) (uint32, error) {
	//just looking for the pid namespace
	return getProcNS(pid, "pid")
}

// GetProcNS returns the namespace ID of a given namespace and process.
// To do so, it requires access to the /proc file system of the host, and CAP_SYS_PTRACE capability.
func getProcNS(pid uint32, nsName string) (uint32, error) {
	nsLink, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/%s", pid, nsName))
	if err != nil {
		return 0, fmt.Errorf("could not read ns file: %v", err)
	}
	ns, err := extractNSFromLink(nsLink)
	if err != nil {
		return 0, fmt.Errorf("could not extract ns id: %v", err)
	}
	return ns, nil
}

// note the format for the way linux manages this is pretty specific
// pid-> 'pid:[namespace_id]'
func extractNSFromLink(link string) (uint32, error) {
	nsLinkSplitted := strings.SplitN(link, ":[", 2)
	if len(nsLinkSplitted) != 2 {
		return 0, fmt.Errorf("link format is not supported")
	}
	nsString := strings.TrimSuffix(nsLinkSplitted[1], "]")
	ns, err := strconv.ParseUint(nsString, 10, 0)
	if err != nil {
		return 0, err
	}
	return uint32(ns), nil
}
