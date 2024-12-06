# Cooperative Executor

This document describes the cooperative executor go module, which is designed to
check for race conditions in functions that use a client API.

## Introduction to Race Conditions

Let's consider the following simple client API:

```go
type Client interface {
  Get() (value int)
  Set(value int)
}
```

The client API has two methods: `Get` and `Set`. The `Get` method returns a
global value stored in a shared server that the client is connected to. The
`Set` method takes a value and stores it in that shared server.

Let's consider the following function that uses the client API to increment the
value stored in the server:

```go
func Increment(client Client) {
  value := client.Get()
  client.Set(value + 1)
}
```

If two goroutines or even two processes on the same or different VMs execute the
`Increment` function concurrently, depending on the order of the execution of
the client API calls `Get` and `Set/1`, the final value stored in the server may
not be the same as if the calls were executed sequentially.

If the two actors, `A` and `B`, execute the `Increment` function sequentially,
the final value will be `2`.

```text
A: client.Get() -> 0
A: client.Set(0 + 1) -> 1
B: client.Get() -> 1
B: client.Set(1 + 1) -> 2
```

However, if the two actors execute the `Increment` function concurrently, the
final value may be unequal to `2`. For example, if the API calls are executed in
the following order, the final value will be one.

```text
A: client.Get() -> 0
B: client.Get() -> 0
A: client.Set(0 + 1) -> 1
B: client.Set(0 + 1) -> 1
```

This is a **race condition**: the final value depends on the order of the
execution of the client API calls.

## Locking

If the two actors are goroutines running in the same process, we can use **locks**
to ensure that the critical section of the code is executed by only one actor at
a time.

```go
var lock sync.Mutex

func Increment(client Client) {
  lock.Lock()
  defer lock.Unlock()
  value := client.Get()
  client.Set(value + 1)
}
```

With the lock, even if the two actors execute the `Increment` function
concurrently, only one actor will be able to execute the critical section of the
code at a time, hence the order of the execution of the client API calls will be
the same as if the actors were executed sequentially, and the final value will
be always `2`.

## Optimistic Locking

In distributed systems, locks are not always the best solution to synchronize
access to shared resources. Locks can lead to deadlocks and can also have an
impact on the performance of the system.

The alternative to locks is **optimistic locking**. In optimistic locking, we
assume that there will be no conflicts, and we only check for conflicts when we
updated the shared resource. If there was a conflict, we retry the operation.

A client API that supports optimistic locking could look like this:

```go
type Client interface {
  GetWithVersion() (value int, version int)
  SetWithVersion(value int, version int) error
}
```

The `Get` method returns the global value stored in the shared server and also
the version of the value. The version is a monotonically increasing number that
is incremented by the server atomically every time the value is updated. The
`Set` method takes a value to be stored in the shared server and also the
version of the value that the client has. If the version of the value stored in
the server is different from the version passed by the client to the `Set`
method, the `Set` method will return an error because the actor has a stale
version of the value and executing the `Set` method would lead to losing updates
made by other actors in the meantime. If the `Set` method returns an error, the
actor should retry the entire operation by calling the `Get` and `Set` methods
again.

The `Increment` function that uses the client API that supports optimistic
locking could look like this:

```go
func Increment(client Client) {
  for {
    value, version := client.GetWithVersion()
    if err := client.SetWithVersion(value + 1, version); err == nil {
      break
    }
  }
}
```

If the value stored in the server has been updated by another actor, the `Set`
method will return an error, and the `Increment` function will retry the entire
operation: it will call the `Get` method again to get the updated value and
version, and then it will try to increment and store the value again.

If two actors execute the `Increment` function sequentially, the final value
will be `2`.

```text
A: client.Get() -> 0, 0
A: client.Set(0 + 1, 0) -> nil
B: client.Get() -> 1, 1
B: client.Set(1 + 1, 1) -> nil
// 2
```

Even if two actors execute the `Increment` function *concurrently*, the final
value will be always `2`. If there was a concurrent modification, and e.g. actor
`A` updated the value before actor `B`, the `Set` method of actor `B` will
return an error and actor `B` will retry the operation.

```text
A: client.Get() -> 0, 0
B: client.Get() -> 0, 0
A: client.Set(0 + 1, 0) -> nil
B: client.Set(0 + 1, 0) -> error
B: client.Get() -> 1, 1
B: client.Set(1 + 1, 1) -> nil
// 2
```

## Testability

Checking actor implementations in unit tests for race conditions is challenging:
the probability of observing an execution order of the client API calls that
leads to an unexpected result is low. So, although the race condition might
reliable occur in production at scale, it might not be easily reproducible in a
unit test.

The general case of checking for race conditions in a golang program is hard:
however, that is concerned with the execution order of low-level operations in
different goroutines.

### Cooperative Executor

The cooperative executor is a go module that aims to check a *specific case of
race conditions*: the scenario where functions use a client API and the race
condition only emerges from the order of the execution of the client API calls.

This is a much simpler problem than the general case of checking for race
conditions: typically it is sufficent to check for two actors and the number of
client API calls made is low, such that it is even feasible to check all
possible orderings of the client API calls.

Race conditions that depend on the order of the execution of the client API
calls are relevant in distributed systems, where multiple actors running even in
different processes access a shared remote service concurrently via a client
API.

The **cooperative executor** is designed to facilitate testing such actors for
race condtions in unit tests. It allows you to check if there is an ordering of
the client API calls when two actors execute concurently that leads to an
unexpected result.

We can use it to check the `Increment` function and verify that indeed, it is
susceptible to race conditions.

```go
func TestIncrement(t *testing.T) {
	executor := race.NewCooperativeExecutor()
	var client, clientA, clientB Client

	setup := func() {
		client = NewClient()
		clientA = NewCooperativeClient("A", client, executor)
		clientB = NewCooperativeClient("B", client, executor)
	}

	assert := func() error {
		if client.Get() != 2 {
			return fmt.Errorf("Assertion failed: expected 2, got %d", client.Get())
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
		t.Errorf("Not all paths were checked: expected 6, got %d", result.NumberOfPathsChecked)
	}
}
```

Furthermore, we can assert that the increment function that uses optimistic
locking is not susceptible to race condtions.

```go
func TestIncrementWithOptimisticLocking(t *testing.T) {
	executor := race.NewCooperativeExecutor()
	var client, clientA, clientB Client

	setup := func() {
		client = NewClient()
		clientA = NewCooperativeClient("A", client, executor)
		clientB = NewCooperativeClient("B", client, executor)
	}

	assert := func() error {
		if client.Get() != 2 {
			return fmt.Errorf("Assertion failed: expected 2, got %d", client.Get())
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
		t.Errorf("Not all paths were checked: expected 6, got %d", result.NumberOfPathsChecked)
	}
}
```

Following these simple and somewhat contrived examples, the cooperative executor
could be used to check even more complex actor implementations for race
conditions.

#### Implementation details

As the examples above show, the implementation of the actor was not changed to be
able to use the cooperative executor.

```go
			value := clientB.Get()
			clientB.Set(value + 1)
```

However, there were a few modifications to allow the cooperative executor to
check all possible orderings of the client API calls.

The `CooperativeClient` is a wrapper around the client API that calls the
`LockAndYield` and `Release` methods of the cooperative executor before and
after each client API call.

```go
type CooperativeExecutor interface {
  LockAndYield(clientName string, callID int, description string)
  Release(clientName string, callID int)
}
```

So, e.g. the `Get` method of the `CooperativeClient` could look like this:

```go
type CooperativeClient struct {
  mu     sync.Mutex
  name   string
  callID int
  client Client
  e      CooperativeExecutor
}

func (cc *cooperativeClient) nextID() int {
	cc.mu.Lock()
	result := cc.callID
	cc.callID++
	defer cc.mu.Unlock()
	return result
}

func (c *CooperativeClient) Get() int {
  callID := cc.nextID()
  c.e.LockAndYield(c.name, callID, "Get")
  defer c.e.Release(c.name, callID)
  return c.client.Get()
}
```

The wrapped client API calls block in the `LockAndYield` method. The cooperative
executor waits a bit until all clients have reached such a yielding point during
their execution. Then, it decides which client API call to execute first and
let's it proceed. It takes note of this decision and the next time it will let
another client proceed. This way, it can explore all possible orderings of the
client API calls, in a way similar to depth-first search, without backtracking,
so each path has to be executed from the beginning.

In the unit test, actors are expected to use dedicated client instances with
unique names. The callID is expected to be unique in the scope of a single
client. The description is expected to capture the intent of the call. With all
this information, the cooperative executor can check all possible orderings of
the client API calls and emit readable output of the explored execution paths.

```text
Execution path:
- A/0 Get
- A/1 Set(1)
- B/0 Get
- B/1 Set(2)
```

## Summary

The cooperative executor is a go module that allows you to check if there is an
ordering of some client API calls when two actors execute concurently that leads
to an unexpected result. It can be helpful to write unit tests for production
code to assert that no ordering of some client API calls will lead to unexpected
results.