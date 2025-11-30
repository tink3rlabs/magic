package storage

import (
	"reflect"
	"testing"
)

func TestStorageAdapterFactory_GetInstance(t *testing.T) {
	type args struct {
		adapterType StorageAdapterType
		config      any
	}
	tests := []struct {
		name    string
		s       StorageAdapterFactory
		args    args
		want    StorageAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.GetInstance(tt.args.adapterType, tt.args.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("StorageAdapterFactory.GetInstance() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StorageAdapterFactory.GetInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}
