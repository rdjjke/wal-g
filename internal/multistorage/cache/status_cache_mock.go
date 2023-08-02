// Code generated by MockGen. DO NOT EDIT.
// Source: status_cache.go

// Package cache is a generated GoMock package.
package cache

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockStatusCache is a mock of StatusCache interface.
type MockStatusCache struct {
	ctrl     *gomock.Controller
	recorder *MockStatusCacheMockRecorder
}

// MockStatusCacheMockRecorder is the mock recorder for MockStatusCache.
type MockStatusCacheMockRecorder struct {
	mock *MockStatusCache
}

// NewMockStatusCache creates a new mock instance.
func NewMockStatusCache(ctrl *gomock.Controller) *MockStatusCache {
	mock := &MockStatusCache{ctrl: ctrl}
	mock.recorder = &MockStatusCacheMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockStatusCache) EXPECT() *MockStatusCacheMockRecorder {
	return m.recorder
}

// AllAliveStorages mocks base method.
func (m *MockStatusCache) AllAliveStorages() ([]NamedFolder, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AllAliveStorages")
	ret0, _ := ret[0].([]NamedFolder)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AllAliveStorages indicates an expected call of AllAliveStorages.
func (mr *MockStatusCacheMockRecorder) AllAliveStorages() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AllAliveStorages", reflect.TypeOf((*MockStatusCache)(nil).AllAliveStorages))
}

// FirstAliveStorage mocks base method.
func (m *MockStatusCache) FirstAliveStorage() (*NamedFolder, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FirstAliveStorage")
	ret0, _ := ret[0].(*NamedFolder)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// FirstAliveStorage indicates an expected call of FirstAliveStorage.
func (mr *MockStatusCacheMockRecorder) FirstAliveStorage() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FirstAliveStorage", reflect.TypeOf((*MockStatusCache)(nil).FirstAliveStorage))
}

// SpecificStorage mocks base method.
func (m *MockStatusCache) SpecificStorage(name string) (*NamedFolder, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SpecificStorage", name)
	ret0, _ := ret[0].(*NamedFolder)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SpecificStorage indicates an expected call of SpecificStorage.
func (mr *MockStatusCacheMockRecorder) SpecificStorage(name interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SpecificStorage", reflect.TypeOf((*MockStatusCache)(nil).SpecificStorage), name)
}
