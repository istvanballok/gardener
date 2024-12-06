// Package race provides a CooperativeExecutor that can be used in unit tests to
// check all possible execution paths of concurrently executed functions using some
// client APIs, to assert that there is no ordering that would lead to an
// unexpected result.
//
// For more detailed documentation, see the cooperative_executor.md file.
package race

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type CooperativeExecutor interface {
	RunAllCombinations(setup func(), assertion func() error, fs ...func()) *Result
	LockAndYield(clientName string, callID int, description string)
	Release(clientName string, callID int)
}

type Result struct {
	NumberOfPathsChecked             int
	PathsChecked                     string
	NumberOfPathsWithFailedAssertion int
	errors                           []error
}

func NewCooperativeExecutor(loglevel int) CooperativeExecutor {
	return &cooperativeExecutor{loglevel: loglevel}
}

type (
	cooperativeExecutor struct {
		loglevel          int
		inbox             chan *msg
		state             []*msg
		inCriticalSection bool
		guide             []*choice
		currentPath       []*choice
		pathsToCheck      [][]*choice
		checkedPaths      [][]*choice
		assertionResults  []error
	}

	msg struct {
		clientName  string
		callID      int
		description string
		proceed     chan struct{}
	}

	choice struct {
		clientName  string
		callID      int
		description string
	}
)

func (e *cooperativeExecutor) LockAndYield(clientName string, callID int, description string) {
	m := &msg{
		clientName:  clientName,
		callID:      callID,
		description: description,
		proceed:     make(chan struct{})}
	e.inbox <- m
	<-m.proceed
}

func (e *cooperativeExecutor) Release(clientName string, callID int) {
	m := &msg{
		clientName: clientName,
		callID:     callID}
	e.inbox <- m
}

func (e *cooperativeExecutor) run(fs ...func()) {
	e.inbox = make(chan *msg)
	var wg, wg2 sync.WaitGroup
	for _, f := range fs {
		wg.Add(1)
		go func(f func()) {
			defer wg.Done()
			f()
		}(f)
	}

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case msg, ok := <-e.inbox:
				if !ok {
					return
				}
				e.maintainState(msg)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(100 * time.Millisecond)
			case <-timer.C:
				e.proceed()
			}
		}
	}()

	wg.Wait()
	close(e.inbox)
	wg2.Wait()

	fmt.Printf("\nExplored execution path:\n")
	for _, choice := range e.currentPath {
		fmt.Printf("- %s/%d %s\n", choice.clientName, choice.callID, choice.description)
	}

	if e.loglevel == 0 {
		fmt.Printf("\nPaths to check later: %d\n", len(e.pathsToCheck))
		for i, path := range e.pathsToCheck {
			fmt.Printf("  Path %d:\n", i+1)
			for _, choice := range path {
				fmt.Printf("  - %s/%d %s\n", choice.clientName, choice.callID, choice.description)
			}
		}
	}
	fmt.Println()
}

func (e *cooperativeExecutor) maintainState(msg *msg) {
	if msg.description == "" {
		var index int
		for i, s := range e.state {
			if s.clientName == msg.clientName && s.callID == msg.callID {
				index = i
				break
			}
		}
		e.state = append(e.state[:index], e.state[index+1:]...)
		e.inCriticalSection = false
	} else {
		e.state = append(e.state, msg)
	}
}

func (e *cooperativeExecutor) proceed() {
	if e.inCriticalSection {
		return
	}
	if len(e.state) == 0 {
		return
	}
	sort.Slice(e.state, func(i, j int) bool {
		return e.state[i].clientName < e.state[j].clientName ||
			(e.state[i].clientName == e.state[j].clientName &&
				e.state[i].description < e.state[j].description)
	})
	if e.loglevel == 0 {
		fmt.Printf("Yield point:\n")
		for _, s := range e.state {
			fmt.Printf("- %s/%d %s\n", s.clientName, s.callID, s.description)
		}
	}
	var next *msg
	if len(e.guide) == 0 {
		next = e.state[0]
		for _, s := range e.state[1:] {
			e.pathsToCheck = append(
				e.pathsToCheck,
				append(append(
					[]*choice{},
					e.currentPath...),
					&choice{
						clientName:  s.clientName,
						callID:      s.callID,
						description: s.description}))
		}
	} else {
		for _, s := range e.state {
			if e.guide[0].clientName == s.clientName && e.guide[0].description == s.description {
				next = s
				e.guide = e.guide[1:]
				break
			}
		}
		if next == nil {
			panic("Unexpected state: the guided choice does not match any in the current states")
		}
	}
	e.currentPath = append(
		e.currentPath,
		&choice{
			clientName:  next.clientName,
			callID:      next.callID,
			description: next.description})
	next.proceed <- struct{}{}
	if e.loglevel == 0 {
		fmt.Printf("  Decided to proceed with client: %s/%d %s\n", next.clientName, next.callID, next.description)
	}
	e.inCriticalSection = true
}

func (e *cooperativeExecutor) RunAllCombinations(setup func(), assertion func() error, fs ...func()) *Result {
	errors := []error{}
	for {
		if len(e.pathsToCheck) > 0 {
			e.guide = e.pathsToCheck[0]
			e.pathsToCheck = e.pathsToCheck[1:]
		}

		fmt.Printf("\n\nChecking path #%d. with '%d' guided choices:\n", len(e.checkedPaths), len(e.guide))
		for _, choice := range e.guide {
			fmt.Printf("- %s/%d %s\n", choice.clientName, choice.callID, choice.description)
		}
		fmt.Println()

		setup()
		e.run(fs...)
		err := assertion()
		e.assertionResults = append(e.assertionResults, err)
		if err != nil {
			errors = append(errors, err)
			fmt.Printf("Assertion failed: %v\n", err)
		}
		e.checkedPaths = append(e.checkedPaths, e.currentPath)
		e.currentPath = []*choice{}
		if len(e.pathsToCheck) == 0 {
			break
		}
	}
	fmt.Printf("\n\nOut of '%d' checked paths, '%d' had failed assertions.\n", len(e.checkedPaths), len(errors))
	return &Result{
		errors:                           errors,
		NumberOfPathsChecked:             len(e.checkedPaths),
		PathsChecked:                     e.pathsChecked(),
		NumberOfPathsWithFailedAssertion: len(errors),
	}
}

func (e *cooperativeExecutor) pathsChecked() string {
	result := ""
	for i, path := range e.checkedPaths {
		result += fmt.Sprintf("Path %d:\n", i+1)
		for _, choice := range path {
			result += fmt.Sprintf("- %s/%d %s\n", choice.clientName, choice.callID, choice.description)
		}
		result += fmt.Sprintf("  assertion error: %v\n", e.assertionResults[i])
	}
	return result
}
