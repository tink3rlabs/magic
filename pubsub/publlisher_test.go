package pubsub

import (
	"reflect"
	"testing"
)

func TestPublisherFactory_GetInstance(t *testing.T) {
	type args struct {
		publisherType PublisherType
		config        any
	}
	tests := []struct {
		name    string
		s       PublisherFactory
		args    args
		want    Publisher
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.GetInstance(tt.args.publisherType, tt.args.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("PublisherFactory.GetInstance() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PublisherFactory.GetInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}
