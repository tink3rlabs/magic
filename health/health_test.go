package health

import (
	"reflect"
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

func TestNewHealthChecker(t *testing.T) {
	type args struct {
		storageAdapter storage.StorageAdapter
	}
	tests := []struct {
		name string
		args args
		want *HealthChecker
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewHealthChecker(tt.args.storageAdapter); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewHealthChecker() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHealthChecker_Check(t *testing.T) {
	type args struct {
		checkStorage bool
		dependencies []string
	}
	tests := []struct {
		name    string
		h       *HealthChecker
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.h.Check(tt.args.checkStorage, tt.args.dependencies); (err != nil) != tt.wantErr {
				t.Errorf("HealthChecker.Check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
