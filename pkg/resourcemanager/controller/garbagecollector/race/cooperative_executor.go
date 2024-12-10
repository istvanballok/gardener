// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0
//
// Package race provides a CooperativeExecutor that can be used in unit tests to
// check all possible execution paths of concurrently executed functions using some
// client APIs, to assert that there is no ordering that would lead to an
// unexpected result.
//
// For a more detailed documentation, please see the cooperative_executor\.md file.
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
	Release(clientName string, callID int, result string)
}

type Result struct {
	NumberOfPathsChecked             int
	NumberOfPathsWithFailedAssertion int
	PathsChecked                     string
	errors                           []error
}

type LogLevel int

var DEBUG, INFO LogLevel = 0, 1

func NewCooperativeExecutor(logLevel LogLevel) CooperativeExecutor {
	return &cooperativeExecutor{logLevel: logLevel}
}

type (
	cooperativeExecutor struct {
		logLevel          LogLevel
		inbox             chan *msg
		state             state
		inCriticalSection bool
		guide             path
		currentPath       path
		pathsToCheck      []path
		checkedPaths      []path
		assertionResults  []error
	}

	msg struct {
		kind        Kind
		clientName  string
		callID      int
		description string
		result      string
		proceed     chan struct{}
	}

	state []*msg

	Kind string

	choice struct {
		clientName  string
		callID      int
		description string
		result      string
	}

	path []*choice
)

const (
	LockAndYield Kind = "LockAndYield"
	Release      Kind = "Release"
)

func (e *cooperativeExecutor) LockAndYield(clientName string, callID int, description string) {
	m := &msg{
		kind:        LockAndYield,
		clientName:  clientName,
		callID:      callID,
		description: description,
		proceed:     make(chan struct{})}
	e.inbox <- m
	<-m.proceed
}

func (e *cooperativeExecutor) Release(clientName string, callID int, result string) {
	m := &msg{
		kind:       Release,
		clientName: clientName,
		callID:     callID,
		result:     result}
	e.inbox <- m
}

func (e *cooperativeExecutor) run(fs ...func()) {
	e.inbox = make(chan *msg)
	var wg sync.WaitGroup
	for _, f := range fs {
		wg.Add(1)
		go func(f func()) {
			defer wg.Done()
			f()
		}(f)
	}

	var wg2 sync.WaitGroup
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		timer := time.NewTimer(10 * time.Millisecond)
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
				timer.Reset(10 * time.Millisecond)
			case <-timer.C:
				e.proceed()
			}
		}
	}()

	wg.Wait()
	close(e.inbox)
	wg2.Wait()
}

func (e *cooperativeExecutor) maintainState(msg *msg) {
	switch msg.kind {
	case LockAndYield:
		e.state = append(e.state, msg)
	case Release:
		e.removeStateEntry(msg)
		e.currentPath[len(e.currentPath)-1].result = msg.result
		e.inCriticalSection = false
	}
}

func (e *cooperativeExecutor) removeStateEntry(msg *msg) {
	var index int
	for i, s := range e.state {
		if s.clientName == msg.clientName && s.callID == msg.callID {
			index = i
			break
		}
	}
	e.state = append(e.state[:index], e.state[index+1:]...)
}

func (e *cooperativeExecutor) proceed() {
	if e.inCriticalSection || len(e.state) == 0 {
		return
	}
	sort.Slice(e.state, func(i, j int) bool {
		return e.state[i].clientName < e.state[j].clientName ||
			(e.state[i].clientName == e.state[j].clientName &&
				e.state[i].description < e.state[j].description)
	})
	if e.logLevel == DEBUG {
		// fmt.Printf is used for concise multi line text output instead of a regular logger
		fmt.Printf("Yield point:\n%s", e.state)
	}
	var next *msg
	if len(e.guide) == 0 {
		// No guided choice, proceed with the first choice and explore the other choices later
		next = e.state[0]
		for _, m := range e.state[1:] {
			e.pathsToCheck = append(
				e.pathsToCheck,
				append(
					append(
						[]*choice{},
						e.currentPath...),
					&choice{
						clientName:  m.clientName,
						callID:      m.callID,
						description: m.description,
						result:      m.result}))
		}
	} else {
		// Proceed with the guided choice
		for _, s := range e.state {
			if e.guide[0].clientName == s.clientName &&
				e.guide[0].description == s.description {
				next = s
				e.guide = e.guide[1:]
				break
			}
		}
		if next == nil {
			fmt.Printf("\n  State:\n%s\n", e.state)
			fmt.Printf("  Guide:\n%s\n", e.guide)
			panic("Unexpected state: the guided choice does not match any in the current state")
		}
	}
	e.currentPath = append(
		e.currentPath,
		&choice{
			clientName:  next.clientName,
			callID:      next.callID,
			description: next.description,
			result:      next.result})
	if e.logLevel == DEBUG {
		fmt.Printf("  Decided to proceed with client: %s/%d %s\n\n", next.clientName, next.callID, next.description)
	}
	e.inCriticalSection = true
	next.proceed <- struct{}{}
}

func (e *cooperativeExecutor) RunAllCombinations(setup func(), assertion func() error, fs ...func()) *Result {
	errors := []error{}
	for {
		if len(e.pathsToCheck) > 0 {
			e.guide = e.pathsToCheck[0]
			e.pathsToCheck = e.pathsToCheck[1:]
		}
		time.Sleep(10 * time.Millisecond) // prevent interleaved log output with the test environment that might log concurrently
		fmt.Printf("\nChecking path #%d. with '%d' guided choices:\n%s\n", len(e.checkedPaths), len(e.guide), e.guide)

		setup()
		e.run(fs...)
		err := assertion()
		fmt.Printf("\nExplored execution path:\n%s\n", e.currentPath)
		fmt.Printf("  assertion error: %v\n", err)

		if e.logLevel == DEBUG {
			fmt.Printf("\nPaths to check later: %d\n\n", len(e.pathsToCheck))
			for i, path := range e.pathsToCheck {
				fmt.Printf("  Path %d:\n%s\n", i+1, path)
			}
		}
		e.assertionResults = append(e.assertionResults, err)
		if err != nil {
			errors = append(errors, err)
		}
		e.checkedPaths = append(e.checkedPaths, e.currentPath)
		e.currentPath = path{}
		if len(e.pathsToCheck) == 0 {
			break
		}
	}
	fmt.Printf("\n\nOut of '%d' checked paths, '%d' had failed assertions.\n", len(e.checkedPaths), len(errors))
	return &Result{
		NumberOfPathsChecked:             len(e.checkedPaths),
		NumberOfPathsWithFailedAssertion: len(errors),
		PathsChecked:                     e.pathsChecked(),
		errors:                           errors,
	}
}

func (e *cooperativeExecutor) pathsChecked() string {
	result := ""
	for i, path := range e.checkedPaths {
		result += fmt.Sprintf("Path %d:\n%s", i+1, path)
		result += fmt.Sprintf("  assertion error: %v\n", e.assertionResults[i])
	}
	return result
}

func (p path) String() string {
	result := ""
	for _, choice := range p {
		result += fmt.Sprintf("- %s/%d %s -> %s\n", choice.clientName, choice.callID, choice.description, choice.result)
	}
	return result
}

func (s state) String() string {
	result := ""
	for _, m := range s {
		result += fmt.Sprintf("- %s/%d %s\n", m.clientName, m.callID, m.description)
	}
	return result
}
