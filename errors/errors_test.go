package errors

import "testing"

func TestBadRequest_Error(t *testing.T) {
	tests := []struct {
		name string
		e    *BadRequest
		want string
	}{
		{
			name: "returns message",
			e:    &BadRequest{Message: "invalid input"},
			want: "invalid input",
		},
		{
			name: "empty message returns empty string",
			e:    &BadRequest{Message: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("BadRequest.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotFound_Error(t *testing.T) {
	tests := []struct {
		name string
		e    *NotFound
		want string
	}{
		{
			name: "returns message",
			e:    &NotFound{Message: "resource not found"},
			want: "resource not found",
		},
		{
			name: "empty message returns empty string",
			e:    &NotFound{Message: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("NotFound.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceUnavailable_Error(t *testing.T) {
	tests := []struct {
		name string
		e    *ServiceUnavailable
		want string
	}{
		{
			name: "returns message",
			e:    &ServiceUnavailable{Message: "service unavailable"},
			want: "service unavailable",
		},
		{
			name: "empty message returns empty string",
			e:    &ServiceUnavailable{Message: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("ServiceUnavailable.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestForbidden_Error(t *testing.T) {
	tests := []struct {
		name string
		e    *Forbidden
		want string
	}{
		{
			name: "returns message",
			e:    &Forbidden{Message: "access forbidden"},
			want: "access forbidden",
		},
		{
			name: "empty message returns empty string",
			e:    &Forbidden{Message: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("Forbidden.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnauthorized_Error(t *testing.T) {
	tests := []struct {
		name string
		e    *Unauthorized
		want string
	}{
		{
			name: "returns message",
			e:    &Unauthorized{Message: "unauthorized"},
			want: "unauthorized",
		},
		{
			name: "empty message returns empty string",
			e:    &Unauthorized{Message: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("Unauthorized.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}
