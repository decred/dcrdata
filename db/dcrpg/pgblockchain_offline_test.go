package dcrpg

import (
	"errors"
	"testing"
)

func TestIsRetryError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"true", retryError{}, true},
		{"false", errors.New("adsf"), false},
		{"nil false", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryError(tt.err); got != tt.want {
				t.Errorf("IsRetryError() = %v, want %v", got, tt.want)
			}
		})
	}
}
