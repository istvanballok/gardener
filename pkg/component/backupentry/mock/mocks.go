// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/gardener/gardener/pkg/component/backupentry (interfaces: Interface)

// Package backupentry is a generated GoMock package.
package backupentry

import (
	context "context"
	reflect "reflect"

	v1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gomock "go.uber.org/mock/gomock"
)

// MockInterface is a mock of Interface interface.
type MockInterface struct {
	ctrl     *gomock.Controller
	recorder *MockInterfaceMockRecorder
}

// MockInterfaceMockRecorder is the mock recorder for MockInterface.
type MockInterfaceMockRecorder struct {
	mock *MockInterface
}

// NewMockInterface creates a new mock instance.
func NewMockInterface(ctrl *gomock.Controller) *MockInterface {
	mock := &MockInterface{ctrl: ctrl}
	mock.recorder = &MockInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockInterface) EXPECT() *MockInterfaceMockRecorder {
	return m.recorder
}

// Deploy mocks base method.
func (m *MockInterface) Deploy(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Deploy", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Deploy indicates an expected call of Deploy.
func (mr *MockInterfaceMockRecorder) Deploy(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Deploy", reflect.TypeOf((*MockInterface)(nil).Deploy), arg0)
}

// Destroy mocks base method.
func (m *MockInterface) Destroy(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Destroy", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Destroy indicates an expected call of Destroy.
func (mr *MockInterfaceMockRecorder) Destroy(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Destroy", reflect.TypeOf((*MockInterface)(nil).Destroy), arg0)
}

// Get mocks base method.
func (m *MockInterface) Get(arg0 context.Context) (*v1beta1.BackupEntry, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0)
	ret0, _ := ret[0].(*v1beta1.BackupEntry)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockInterfaceMockRecorder) Get(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockInterface)(nil).Get), arg0)
}

// GetActualBucketName mocks base method.
func (m *MockInterface) GetActualBucketName() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetActualBucketName")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetActualBucketName indicates an expected call of GetActualBucketName.
func (mr *MockInterfaceMockRecorder) GetActualBucketName() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetActualBucketName", reflect.TypeOf((*MockInterface)(nil).GetActualBucketName))
}

// Migrate mocks base method.
func (m *MockInterface) Migrate(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Migrate", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Migrate indicates an expected call of Migrate.
func (mr *MockInterfaceMockRecorder) Migrate(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Migrate", reflect.TypeOf((*MockInterface)(nil).Migrate), arg0)
}

// Restore mocks base method.
func (m *MockInterface) Restore(arg0 context.Context, arg1 *v1beta1.ShootState) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Restore", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Restore indicates an expected call of Restore.
func (mr *MockInterfaceMockRecorder) Restore(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Restore", reflect.TypeOf((*MockInterface)(nil).Restore), arg0, arg1)
}

// SetBucketName mocks base method.
func (m *MockInterface) SetBucketName(arg0 string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetBucketName", arg0)
}

// SetBucketName indicates an expected call of SetBucketName.
func (mr *MockInterfaceMockRecorder) SetBucketName(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetBucketName", reflect.TypeOf((*MockInterface)(nil).SetBucketName), arg0)
}

// SetForceDeletionAnnotation mocks base method.
func (m *MockInterface) SetForceDeletionAnnotation(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetForceDeletionAnnotation", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetForceDeletionAnnotation indicates an expected call of SetForceDeletionAnnotation.
func (mr *MockInterfaceMockRecorder) SetForceDeletionAnnotation(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetForceDeletionAnnotation", reflect.TypeOf((*MockInterface)(nil).SetForceDeletionAnnotation), arg0)
}

// Wait mocks base method.
func (m *MockInterface) Wait(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Wait", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Wait indicates an expected call of Wait.
func (mr *MockInterfaceMockRecorder) Wait(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Wait", reflect.TypeOf((*MockInterface)(nil).Wait), arg0)
}

// WaitCleanup mocks base method.
func (m *MockInterface) WaitCleanup(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WaitCleanup", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// WaitCleanup indicates an expected call of WaitCleanup.
func (mr *MockInterfaceMockRecorder) WaitCleanup(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WaitCleanup", reflect.TypeOf((*MockInterface)(nil).WaitCleanup), arg0)
}

// WaitMigrate mocks base method.
func (m *MockInterface) WaitMigrate(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WaitMigrate", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// WaitMigrate indicates an expected call of WaitMigrate.
func (mr *MockInterfaceMockRecorder) WaitMigrate(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WaitMigrate", reflect.TypeOf((*MockInterface)(nil).WaitMigrate), arg0)
}