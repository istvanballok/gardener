// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

// Code generated by lister-gen. DO NOT EDIT.

package internalversion

import (
	core "github.com/gardener/gardener/pkg/apis/core"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ShootLister helps list Shoots.
// All objects returned here must be treated as read-only.
type ShootLister interface {
	// List lists all Shoots in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*core.Shoot, err error)
	// Shoots returns an object that can list and get Shoots.
	Shoots(namespace string) ShootNamespaceLister
	ShootListerExpansion
}

// shootLister implements the ShootLister interface.
type shootLister struct {
	indexer cache.Indexer
}

// NewShootLister returns a new ShootLister.
func NewShootLister(indexer cache.Indexer) ShootLister {
	return &shootLister{indexer: indexer}
}

// List lists all Shoots in the indexer.
func (s *shootLister) List(selector labels.Selector) (ret []*core.Shoot, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*core.Shoot))
	})
	return ret, err
}

// Shoots returns an object that can list and get Shoots.
func (s *shootLister) Shoots(namespace string) ShootNamespaceLister {
	return shootNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ShootNamespaceLister helps list and get Shoots.
// All objects returned here must be treated as read-only.
type ShootNamespaceLister interface {
	// List lists all Shoots in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*core.Shoot, err error)
	// Get retrieves the Shoot from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*core.Shoot, error)
	ShootNamespaceListerExpansion
}

// shootNamespaceLister implements the ShootNamespaceLister
// interface.
type shootNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Shoots in the indexer for a given namespace.
func (s shootNamespaceLister) List(selector labels.Selector) (ret []*core.Shoot, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*core.Shoot))
	})
	return ret, err
}

// Get retrieves the Shoot from the indexer for a given namespace and name.
func (s shootNamespaceLister) Get(name string) (*core.Shoot, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(core.Resource("shoot"), name)
	}
	return obj.(*core.Shoot), nil
}