package main

import (
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

func Test_main(t *testing.T) {
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			main()
		})
	}
}

func Test_list(t *testing.T) {
	type args struct {
		s storage.StorageAdapter
	}
	tests := []struct {
		name string
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list(tt.args.s)
		})
	}
}
