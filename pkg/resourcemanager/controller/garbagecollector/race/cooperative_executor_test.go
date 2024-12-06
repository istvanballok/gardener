package race_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/race"
)

type SimpleClient interface {
	Get() (value int)
	Set(value int)
	GetWithVersion() (value, version int)
	SetWithVersion(value, version int) error
}

func NewSimpleClient() SimpleClient {
	return &simpleClient{}
}

type simpleClient struct {
	mu      sync.Mutex
	version int
	value   int
}

func (c *simpleClient) Get() (value int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func (c *simpleClient) Set(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.set(value)
}

func (c *simpleClient) GetWithVersion() (value, version int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value, c.version
}

func (c *simpleClient) SetWithVersion(value, version int) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version != version {
		return fmt.Errorf("version mismatch: expected %d, got %d", c.version, version)
	}
	c.set(value)
	return nil
}

func (c *simpleClient) set(value int) {
	c.value = value
	c.version++
}

func NewCooperativeSimpleClient(clientName string, simpleClient SimpleClient, executor race.CooperativeExecutor) SimpleClient {
	return &cooperativeSimpleClient{
		clientName:   clientName,
		simpleClient: simpleClient,
		executor:     executor,
	}
}

type cooperativeSimpleClient struct {
	clientName   string
	callID       int
	mu           sync.Mutex
	simpleClient SimpleClient
	executor     race.CooperativeExecutor
}

func (cc *cooperativeSimpleClient) nextCallID() int {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	callID := cc.callID
	cc.callID++
	return callID
}

func (cc *cooperativeSimpleClient) Get() int {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, "Get")
	defer cc.executor.Release(cc.clientName, callID)
	return cc.simpleClient.Get()
}

func (cc *cooperativeSimpleClient) Set(value int) {
	nextCallID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, nextCallID, fmt.Sprintf("Set(%d)", value))
	defer cc.executor.Release(cc.clientName, nextCallID)
	cc.simpleClient.Set(value)
}

func (cc *cooperativeSimpleClient) GetWithVersion() (value, version int) {
	nextCallID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, nextCallID, "GetWithVersion")
	defer cc.executor.Release(cc.clientName, nextCallID)
	return cc.simpleClient.GetWithVersion()
}

func (cc *cooperativeSimpleClient) SetWithVersion(value, version int) error {
	nextCallID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, nextCallID, fmt.Sprintf("SetWithVersion(%d, %d)", value, version))
	defer cc.executor.Release(cc.clientName, nextCallID)
	return cc.simpleClient.SetWithVersion(value, version)
}

func TestIncrementSimple(t *testing.T) {
	executor := race.NewCooperativeExecutor(0)
	var simpleClient, clientA, clientB SimpleClient

	setup := func() {
		simpleClient = NewSimpleClient()
		clientA = NewCooperativeSimpleClient("A", simpleClient, executor)
		clientB = NewCooperativeSimpleClient("B", simpleClient, executor)
	}

	assert := func() error {
		if simpleClient.Get() != 2 {
			return fmt.Errorf("Assertion failed: expected 2, got %d", simpleClient.Get())
		}
		return nil
	}

	result := executor.RunAllCombinations(
		setup,
		assert,
		func() {
			value := clientA.Get()
			clientA.Set(value + 1)
		},
		func() {
			value := clientB.Get()
			clientB.Set(value + 1)
		},
	)
	if result.NumberOfPathsWithFailedAssertion == 0 {
		t.Error("The race condition was not detected")
	}
	if result.NumberOfPathsWithFailedAssertion != 4 {
		t.Errorf("Expected to find 4 paths that lead to a race condition, found: %d", result.NumberOfPathsWithFailedAssertion)
	}
	if result.NumberOfPathsChecked != 6 {
		t.Errorf("Not all paths were checked: expected to check 6 paths in total, got %d", result.NumberOfPathsChecked)
	}
}

func TestIncrementWithOptimisticLockingSimple(t *testing.T) {
	executor := race.NewCooperativeExecutor(0)
	var simpleClient, clientA, clientB SimpleClient

	setup := func() {
		simpleClient = NewSimpleClient()
		clientA = NewCooperativeSimpleClient("A", simpleClient, executor)
		clientB = NewCooperativeSimpleClient("B", simpleClient, executor)
	}

	assert := func() error {
		if simpleClient.Get() != 2 {
			return fmt.Errorf("Assertion failed: expected 2, got %d", simpleClient.Get())
		}
		return nil
	}

	result := executor.RunAllCombinations(
		setup,
		assert,
		func() {
			for {
				value, version := clientA.GetWithVersion()
				if clientA.SetWithVersion(value+1, version) == nil {
					break
				}
			}
		},
		func() {
			for {
				value, version := clientB.GetWithVersion()
				if clientB.SetWithVersion(value+1, version) == nil {
					break
				}
			}
		},
	)
	if result.NumberOfPathsWithFailedAssertion != 0 {
		t.Error("There should be no race condition")
	}
	if result.NumberOfPathsChecked != 6 {
		t.Errorf("Not all paths were checked: expected to check 6 paths in total, got %d", result.NumberOfPathsChecked)
	}
}
