package types

import (
	"reflect"
	"testing"
)

func TestGetOpenAPIDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		want    []byte
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetOpenAPIDefinitions()
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetOpenAPIDefinitions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetOpenAPIDefinitions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeOpenAPIDefinitions(t *testing.T) {
	type args struct {
		inputDefinition []byte
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeOpenAPIDefinitions(tt.args.inputDefinition)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MergeOpenAPIDefinitions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MergeOpenAPIDefinitions() = %v, want %v", got, tt.want)
			}
		})
	}
}
