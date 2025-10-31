package utils

import (
	"testing"
)

func TestNewId(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
		validate func(t *testing.T, got *string)
	}{
		{
			name:    "generates valid ID",
			wantErr: false,
			validate: func(t *testing.T, got *string) {
				if got == nil {
					t.Error("NewId() returned nil")
					return
				}
				if *got == "" {
					t.Error("NewId() returned empty string")
				}
				if len(*got) == 0 {
					t.Error("NewId() returned ID with zero length")
				}
			},
		},
		{
			name:    "generates unique IDs",
			wantErr: false,
			validate: func(t *testing.T, got *string) {
				id2, err := NewId()
				if err != nil {
					t.Errorf("NewId() error = %v", err)
					return
				}
				if got != nil && id2 != nil && *got == *id2 {
					t.Error("NewId() generated duplicate IDs")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewId()
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewId() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}
