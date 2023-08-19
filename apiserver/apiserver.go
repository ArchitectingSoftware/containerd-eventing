package apiserver

import (
	"fmt"
	"log"
	"net/http"

	"github.com/architectingsoftware/cdevents/cdclient"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 10080
)

type ApiConfig struct {
	Sockpath  string
	Namespace string
	HostName  string
	Port      int
}

type ApiServer struct {
	apiConfig ApiConfig
	client    *cdclient.ContainerDCli
	apiEngine *gin.Engine
}

func NewApiServerWithConfig(apiConfig *ApiConfig) (*ApiServer, error) {

	apiConfigCopy := *apiConfig

	c, err := cdclient.NewClientWithConfig(apiConfig.Sockpath, apiConfig.Namespace)
	if err != nil {
		log.Println("error starting containerd client:", err)
		return nil, err
	}

	r := gin.Default()
	r.Use(cors.Default())

	apiSvr := &ApiServer{
		apiConfig: apiConfigCopy,
		client:    c,
		apiEngine: r,
	}

	return apiSvr, nil
}

func (s *ApiServer) Run() error {
	s.apiEngine.GET("/containerd/start", s.startContainerD)
	s.apiEngine.GET("/containerd/stop", s.stopContainerD)

	serverPath := fmt.Sprintf("%s:%d", s.apiConfig.HostName, s.apiConfig.Port)
	return s.apiEngine.Run(serverPath)
}

func (s *ApiServer) startContainerD(c *gin.Context) {
	err := s.client.Start()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "started ok"})
}

func (s *ApiServer) stopContainerD(c *gin.Context) {
	err := s.client.Stop()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
