package pubsub

import (
	"reflect"
	"testing"
)

func TestGetSNSPublisher(t *testing.T) {
	type args struct {
		config map[string]string
	}
	tests := []struct {
		name string
		args args
		want *SNSPublisher
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetSNSPublisher(tt.args.config); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetSNSPublisher() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSNSPublisher_Publish(t *testing.T) {
	type args struct {
		topic   string
		message string
		params  map[string]any
	}
	tests := []struct {
		name    string
		s       *SNSPublisher
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Publish(tt.args.topic, tt.args.message, tt.args.params); (err != nil) != tt.wantErr {
				t.Errorf("SNSPublisher.Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
