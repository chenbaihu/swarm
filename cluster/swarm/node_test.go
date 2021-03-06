package swarm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/samalba/dockerclient"
	"github.com/samalba/dockerclient/mockclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var (
	mockInfo = &dockerclient.Info{
		ID:              "id",
		Name:            "name",
		NCPU:            10,
		MemTotal:        20,
		Driver:          "driver-test",
		ExecutionDriver: "execution-driver-test",
		KernelVersion:   "1.2.3",
		OperatingSystem: "golang",
		Labels:          []string{"foo=bar"},
	}
)

func TestNodeConnectionFailure(t *testing.T) {
	node := NewNode("test", 0)
	assert.False(t, node.isConnected())

	// Always fail.
	client := mockclient.NewMockClient()
	client.On("Info").Return(&dockerclient.Info{}, errors.New("fail"))

	// Connect() should fail and isConnected() return false.
	assert.Error(t, node.connectClient(client))
	assert.False(t, node.isConnected())

	client.Mock.AssertExpectations(t)
}

func TestOutdatedNode(t *testing.T) {
	node := NewNode("test", 0)
	client := mockclient.NewMockClient()
	client.On("Info").Return(&dockerclient.Info{}, nil)

	assert.Error(t, node.connectClient(client))
	assert.False(t, node.isConnected())

	client.Mock.AssertExpectations(t)
}

func TestNodeCpusMemory(t *testing.T) {
	node := NewNode("test", 0)
	assert.False(t, node.isConnected())

	client := mockclient.NewMockClient()
	client.On("Info").Return(mockInfo, nil)
	client.On("ListContainers", true, false, "").Return([]dockerclient.Container{}, nil)
	client.On("ListImages").Return([]*dockerclient.Image{}, nil)
	client.On("StartMonitorEvents", mock.Anything, mock.Anything, mock.Anything).Return()

	assert.NoError(t, node.connectClient(client))
	assert.True(t, node.isConnected())
	assert.True(t, node.IsHealthy())

	assert.Equal(t, node.UsedCpus(), 0)
	assert.Equal(t, node.UsedMemory(), 0)

	client.Mock.AssertExpectations(t)
}

func TestNodeSpecs(t *testing.T) {
	node := NewNode("test", 0)
	assert.False(t, node.isConnected())

	client := mockclient.NewMockClient()
	client.On("Info").Return(mockInfo, nil)
	client.On("ListContainers", true, false, "").Return([]dockerclient.Container{}, nil)
	client.On("ListImages").Return([]*dockerclient.Image{}, nil)
	client.On("StartMonitorEvents", mock.Anything, mock.Anything, mock.Anything).Return()

	assert.NoError(t, node.connectClient(client))
	assert.True(t, node.isConnected())
	assert.True(t, node.IsHealthy())

	assert.Equal(t, node.Cpus, mockInfo.NCPU)
	assert.Equal(t, node.Memory, mockInfo.MemTotal)
	assert.Equal(t, node.Labels()["storagedriver"], mockInfo.Driver)
	assert.Equal(t, node.Labels()["executiondriver"], mockInfo.ExecutionDriver)
	assert.Equal(t, node.Labels()["kernelversion"], mockInfo.KernelVersion)
	assert.Equal(t, node.Labels()["operatingsystem"], mockInfo.OperatingSystem)
	assert.Equal(t, node.Labels()["foo"], "bar")

	client.Mock.AssertExpectations(t)
}

func TestNodeState(t *testing.T) {
	node := NewNode("test", 0)
	assert.False(t, node.isConnected())

	client := mockclient.NewMockClient()
	client.On("Info").Return(mockInfo, nil)
	client.On("StartMonitorEvents", mock.Anything, mock.Anything, mock.Anything).Return()

	// The client will return one container at first, then a second one will appear.
	client.On("ListContainers", true, false, "").Return([]dockerclient.Container{{Id: "one"}}, nil).Once()
	client.On("ListImages").Return([]*dockerclient.Image{}, nil).Once()
	client.On("InspectContainer", "one").Return(&dockerclient.ContainerInfo{Config: &dockerclient.ContainerConfig{CpuShares: 100}}, nil).Once()
	client.On("ListContainers", true, false, fmt.Sprintf("{%q:[%q]}", "id", "two")).Return([]dockerclient.Container{{Id: "two"}}, nil).Once()
	client.On("InspectContainer", "two").Return(&dockerclient.ContainerInfo{Config: &dockerclient.ContainerConfig{CpuShares: 100}}, nil).Once()

	assert.NoError(t, node.connectClient(client))
	assert.True(t, node.isConnected())

	// The node should only have a single container at this point.
	containers := node.Containers()
	assert.Len(t, containers, 1)
	if containers[0].Id != "one" {
		t.Fatalf("Missing container: one")
	}

	// Fake an event which will trigger a refresh. The second container will appear.
	node.handler(&dockerclient.Event{Id: "two", Status: "created"}, nil)
	containers = node.Containers()
	assert.Len(t, containers, 2)
	if containers[0].Id != "one" && containers[1].Id != "one" {
		t.Fatalf("Missing container: one")
	}
	if containers[0].Id != "two" && containers[1].Id != "two" {
		t.Fatalf("Missing container: two")
	}

	client.Mock.AssertExpectations(t)
}

func TestNodeContainerLookup(t *testing.T) {
	node := NewNode("test-node", 0)
	assert.False(t, node.isConnected())

	client := mockclient.NewMockClient()
	client.On("Info").Return(mockInfo, nil)
	client.On("StartMonitorEvents", mock.Anything, mock.Anything, mock.Anything).Return()

	client.On("ListContainers", true, false, "").Return([]dockerclient.Container{{Id: "container-id", Names: []string{"/container-name1", "/container-name2"}}}, nil).Once()
	client.On("ListImages").Return([]*dockerclient.Image{}, nil).Once()
	client.On("InspectContainer", "container-id").Return(&dockerclient.ContainerInfo{Config: &dockerclient.ContainerConfig{CpuShares: 100}}, nil).Once()

	assert.NoError(t, node.connectClient(client))
	assert.True(t, node.isConnected())

	// Invalid lookup
	assert.Nil(t, node.Container("invalid-id"))
	assert.Nil(t, node.Container(""))
	// Container ID lookup.
	assert.NotNil(t, node.Container("container-id"))
	// Container ID prefix lookup.
	assert.NotNil(t, node.Container("container-"))
	// Container name lookup.
	assert.NotNil(t, node.Container("container-name1"))
	assert.NotNil(t, node.Container("container-name2"))
	// Container node/name matching.
	assert.NotNil(t, node.Container("id/container-name1"))
	assert.NotNil(t, node.Container("id/container-name2"))

	client.Mock.AssertExpectations(t)
}

func TestCreateContainer(t *testing.T) {
	var (
		config = &dockerclient.ContainerConfig{
			Image:     "busybox",
			CpuShares: 512,
			Cmd:       []string{"date"},
			Tty:       false,
		}
		node   = NewNode("test", 0)
		client = mockclient.NewMockClient()
	)

	client.On("Info").Return(mockInfo, nil)
	client.On("StartMonitorEvents", mock.Anything, mock.Anything, mock.Anything).Return()
	client.On("ListContainers", true, false, "").Return([]dockerclient.Container{}, nil).Once()
	client.On("ListImages").Return([]*dockerclient.Image{}, nil).Once()
	assert.NoError(t, node.connectClient(client))
	assert.True(t, node.isConnected())

	mockConfig := *config
	mockConfig.CpuShares = config.CpuShares * mockInfo.NCPU

	// Everything is ok
	name := "test1"
	id := "id1"
	client.On("CreateContainer", &mockConfig, name).Return(id, nil).Once()
	client.On("ListContainers", true, false, fmt.Sprintf(`{"id":[%q]}`, id)).Return([]dockerclient.Container{{Id: id}}, nil).Once()
	client.On("ListImages").Return([]*dockerclient.Image{}, nil).Once()
	client.On("InspectContainer", id).Return(&dockerclient.ContainerInfo{Config: config}, nil).Once()
	container, err := node.create(config, name, false)
	assert.Nil(t, err)
	assert.Equal(t, container.Id, id)
	assert.Len(t, node.Containers(), 1)

	// Image not found, pullImage == false
	name = "test2"
	mockConfig.CpuShares = config.CpuShares * mockInfo.NCPU
	client.On("CreateContainer", &mockConfig, name).Return("", dockerclient.ErrNotFound).Once()
	container, err = node.create(config, name, false)
	assert.Equal(t, err, dockerclient.ErrNotFound)
	assert.Nil(t, container)

	// Image not found, pullImage == true, and the image can be pulled successfully
	name = "test3"
	id = "id3"
	mockConfig.CpuShares = config.CpuShares * mockInfo.NCPU
	client.On("PullImage", config.Image+":latest", mock.Anything).Return(nil).Once()
	client.On("CreateContainer", &mockConfig, name).Return("", dockerclient.ErrNotFound).Once()
	client.On("CreateContainer", &mockConfig, name).Return(id, nil).Once()
	client.On("ListContainers", true, false, fmt.Sprintf(`{"id":[%q]}`, id)).Return([]dockerclient.Container{{Id: id}}, nil).Once()
	client.On("ListImages").Return([]*dockerclient.Image{}, nil).Once()
	client.On("InspectContainer", id).Return(&dockerclient.ContainerInfo{Config: config}, nil).Once()
	container, err = node.create(config, name, true)
	assert.Nil(t, err)
	assert.Equal(t, container.Id, id)
	assert.Len(t, node.Containers(), 2)
}

func TestTotalMemory(t *testing.T) {
	node := NewNode("test", 0.05)
	node.Memory = 1024
	assert.Equal(t, node.TotalMemory(), 1024+1024*5/100)

	node = NewNode("test", 0)
	node.Memory = 1024
	assert.Equal(t, node.TotalMemory(), 1024)
}

func TestTotalCpus(t *testing.T) {
	node := NewNode("test", 0.05)
	node.Cpus = 2
	assert.Equal(t, node.TotalCpus(), 2+2*5/100)

	node = NewNode("test", 0)
	node.Cpus = 2
	assert.Equal(t, node.TotalCpus(), 2)
}
