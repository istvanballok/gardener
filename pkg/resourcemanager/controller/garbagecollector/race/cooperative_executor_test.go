// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package race_test

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/race"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

func (c *simpleClient) set(value int) {
	c.value = value
	c.version++
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

func NewCooperativeSimpleClient(clientName string, simpleClient SimpleClient, executor race.CooperativeExecutor) SimpleClient {
	return &cooperativeSimpleClient{
		clientName:   clientName,
		simpleClient: simpleClient,
		executor:     executor,
	}
}

type cooperativeSimpleClient struct {
	clientName   string
	simpleClient SimpleClient
	executor     race.CooperativeExecutor
	mu           sync.Mutex
	callID       int
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
	cc.executor.LockAndYield(cc.clientName, callID, "Get()")
	value := cc.simpleClient.Get()
	cc.executor.Release(cc.clientName, callID, fmt.Sprintf("value=%d", value))
	return value
}

func (cc *cooperativeSimpleClient) Set(value int) {
	nextCallID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, nextCallID, fmt.Sprintf("Set(%d)", value))
	cc.simpleClient.Set(value)
	cc.executor.Release(cc.clientName, nextCallID, fmt.Sprintf("[value=%d]", cc.simpleClient.Get()))
}

func (cc *cooperativeSimpleClient) GetWithVersion() (value, version int) {
	nextCallID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, nextCallID, "GetWithVersion()")
	value, version = cc.simpleClient.GetWithVersion()
	cc.executor.Release(cc.clientName, nextCallID, fmt.Sprintf("value=%d, version=%d", value, version))
	return
}

func (cc *cooperativeSimpleClient) SetWithVersion(value, version int) error {
	nextCallID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, nextCallID, fmt.Sprintf("SetWithVersion(%d, %d)", value, version))
	err := cc.simpleClient.SetWithVersion(value, version)
	cc.executor.Release(cc.clientName, nextCallID, fmt.Sprintf("[value=%d], error=%v", cc.simpleClient.Get(), err))
	return err
}

var _ = Describe("Cooperative Executor", func() {

	It("should detect possible race conditions when two actors try to increment a shared value without any synchronization mechanism", func() {
		executor := race.NewCooperativeExecutor(race.DEBUG)
		var simpleClient, clientA, clientB SimpleClient

		setup := func() {
			simpleClient = NewSimpleClient()
			clientA = NewCooperativeSimpleClient("A", simpleClient, executor)
			clientB = NewCooperativeSimpleClient("B", simpleClient, executor)
		}

		assert := func() error {
			if simpleClient.Get() != 2 {
				return fmt.Errorf("expected value==2, got value==%d", simpleClient.Get())
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
		Expect(result.NumberOfPathsWithFailedAssertion).To(Equal(4))
		Expect(result.NumberOfPathsChecked).To(Equal(6))
		Expect(strings.TrimSpace(result.PathsChecked)).To(Equal(strings.TrimSpace(`

Path 1:
- A/0 Get() -> value=0
- A/1 Set(1) -> [value=1]
- B/0 Get() -> value=1
- B/1 Set(2) -> [value=2]
  assertion error: <nil>
Path 2:
- B/0 Get() -> value=0
- A/0 Get() -> value=0
- A/1 Set(1) -> [value=1]
- B/1 Set(1) -> [value=1]
  assertion error: expected value==2, got value==1
Path 3:
- A/0 Get() -> value=0
- B/0 Get() -> value=0
- A/1 Set(1) -> [value=1]
- B/1 Set(1) -> [value=1]
  assertion error: expected value==2, got value==1
Path 4:
- B/0 Get() -> value=0
- B/1 Set(1) -> [value=1]
- A/0 Get() -> value=1
- A/1 Set(2) -> [value=2]
  assertion error: <nil>
Path 5:
- B/0 Get() -> value=0
- A/0 Get() -> value=0
- B/1 Set(1) -> [value=1]
- A/1 Set(1) -> [value=1]
  assertion error: expected value==2, got value==1
Path 6:
- A/0 Get() -> value=0
- B/0 Get() -> value=0
- B/1 Set(1) -> [value=1]
- A/1 Set(1) -> [value=1]
  assertion error: expected value==2, got value==1

		`)), "Here is the actual value of PathsChecked in full length. Copy this and update the string literal in the unit test's source code: \n\n%s\n",
			result.PathsChecked)
	})

	It("should detect no race conditions when the actors properly use optimistic locking", func() {
		executor := race.NewCooperativeExecutor(race.DEBUG)
		var simpleClient, clientA, clientB SimpleClient

		setup := func() {
			simpleClient = NewSimpleClient()
			clientA = NewCooperativeSimpleClient("A", simpleClient, executor)
			clientB = NewCooperativeSimpleClient("B", simpleClient, executor)
		}

		assert := func() error {
			if simpleClient.Get() != 2 {
				return fmt.Errorf("expected value==2, got value==%d", simpleClient.Get())
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
		Expect(result.NumberOfPathsWithFailedAssertion).To(Equal(0))
		Expect(result.NumberOfPathsChecked).To(Equal(6))

		Expect(strings.TrimSpace(result.PathsChecked)).To(Equal(strings.TrimSpace(`

Path 1:
- A/0 GetWithVersion() -> value=0, version=0
- A/1 SetWithVersion(1, 0) -> [value=1], error=<nil>
- B/0 GetWithVersion() -> value=1, version=1
- B/1 SetWithVersion(2, 1) -> [value=2], error=<nil>
  assertion error: <nil>
Path 2:
- B/0 GetWithVersion() -> value=0, version=0
- A/0 GetWithVersion() -> value=0, version=0
- A/1 SetWithVersion(1, 0) -> [value=1], error=<nil>
- B/1 SetWithVersion(1, 0) -> [value=1], error=version mismatch: expected 1, got 0
- B/2 GetWithVersion() -> value=1, version=1
- B/3 SetWithVersion(2, 1) -> [value=2], error=<nil>
  assertion error: <nil>
Path 3:
- A/0 GetWithVersion() -> value=0, version=0
- B/0 GetWithVersion() -> value=0, version=0
- A/1 SetWithVersion(1, 0) -> [value=1], error=<nil>
- B/1 SetWithVersion(1, 0) -> [value=1], error=version mismatch: expected 1, got 0
- B/2 GetWithVersion() -> value=1, version=1
- B/3 SetWithVersion(2, 1) -> [value=2], error=<nil>
  assertion error: <nil>
Path 4:
- B/0 GetWithVersion() -> value=0, version=0
- B/1 SetWithVersion(1, 0) -> [value=1], error=<nil>
- A/0 GetWithVersion() -> value=1, version=1
- A/1 SetWithVersion(2, 1) -> [value=2], error=<nil>
  assertion error: <nil>
Path 5:
- B/0 GetWithVersion() -> value=0, version=0
- A/0 GetWithVersion() -> value=0, version=0
- B/1 SetWithVersion(1, 0) -> [value=1], error=<nil>
- A/1 SetWithVersion(1, 0) -> [value=1], error=version mismatch: expected 1, got 0
- A/2 GetWithVersion() -> value=1, version=1
- A/3 SetWithVersion(2, 1) -> [value=2], error=<nil>
  assertion error: <nil>
Path 6:
- A/0 GetWithVersion() -> value=0, version=0
- B/0 GetWithVersion() -> value=0, version=0
- B/1 SetWithVersion(1, 0) -> [value=1], error=<nil>
- A/1 SetWithVersion(1, 0) -> [value=1], error=version mismatch: expected 1, got 0
- A/2 GetWithVersion() -> value=1, version=1
- A/3 SetWithVersion(2, 1) -> [value=2], error=<nil>
  assertion error: <nil>

		`)), "Here is the actual value of PathsChecked in full length. Copy this and update the string literal in the unit test's source code: \n\n%s\n",
			result.PathsChecked)
	})
})
