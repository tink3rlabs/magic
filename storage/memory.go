package storage

import (
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var memoryAdapterLock = &sync.Mutex{}

type MemoryAdapter struct {
	store map[string][]interface{}
}

var memoryAdapterInstance *MemoryAdapter

func GetMemoryAdapterInstance() *MemoryAdapter {
	if memoryAdapterInstance == nil {
		memoryAdapterLock.Lock()
		defer memoryAdapterLock.Unlock()
		if memoryAdapterInstance == nil {
			memoryAdapterInstance = &MemoryAdapter{store: map[string][]interface{}{}}
		}
	}
	return memoryAdapterInstance
}

func (m *MemoryAdapter) Execute(s string) error {
	return errors.New("memory adapter doesn't support executing arbitrary statements")
}

func (m *MemoryAdapter) Ping() error {
	return nil
}

func (m *MemoryAdapter) Create(item any) error {
	itemType := reflect.TypeOf(item).String()
	m.store[itemType] = append(m.store[itemType], item)
	return nil
}

func (m *MemoryAdapter) Get(dest any, itemKey string, itemValue string) error {
	t := strings.ReplaceAll(reflect.TypeOf(dest).String(), "*", "")
	for _, item := range m.store[t] {
		if reflect.ValueOf(item).FieldByName(itemKey).String() == itemValue {
			v := reflect.ValueOf(dest)
			// Check if the value is a pointer and if it's settable
			if v.Kind() == reflect.Ptr && v.Elem().CanSet() {
				v.Elem().Set(reflect.ValueOf(item))
			}
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryAdapter) Update(item any, itemKey string, itemValue string) error {
	t := reflect.TypeOf(item).String()
	for i, existingItem := range m.store[t] {
		if reflect.ValueOf(existingItem).FieldByName(itemKey).String() == itemValue {
			m.store[t][i] = item
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryAdapter) Delete(item any, itemKey string, itemValue string) error {
	t := strings.ReplaceAll(reflect.TypeOf(item).String(), "*", "")
	for k, v := range m.store[t] {
		if reflect.ValueOf(v).FieldByName(itemKey).String() == itemValue {
			m.store[t] = append(m.store[t][:k], m.store[t][k+1:]...)
		}
	}
	return nil
}

func (m *MemoryAdapter) List(items any, itemKey string, limit int, cursor string) (string, error) {
	nextId := ""

	id, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("failed to decode next cursor: %v", err)
	}

	r := reflect.ValueOf(items)

	// Get one extra item to be able to set that item's Id as the cursor for the next request
	t := strings.ReplaceAll(r.Elem().Type().String(), "[]", "")
	for _, v := range m.store[t] {
		if (reflect.ValueOf(v).FieldByName(itemKey).String() >= string(id)) && r.Elem().Len() < limit+1 {
			r.Elem().Set(reflect.Append(r.Elem(), reflect.ValueOf(v)))
		}
	}

	// If we have a full list, set the Id of the extra last item as the next cursor and remove it from the list of items to return
	if (r.Elem().Len()) == limit+1 {
		lastItem := r.Elem().Index(r.Elem().Len() - 1)
		nextId = base64.StdEncoding.EncodeToString([]byte(lastItem.FieldByName(itemKey).String()))
		// Check if the value is a pointer and if it's settable
		if r.Kind() == reflect.Ptr && r.Elem().CanSet() {
			r.Elem().Set(r.Elem().Slice(0, r.Elem().Len()-1))
		}
	}

	return nextId, nil
}
