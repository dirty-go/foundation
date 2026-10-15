package s3

import "testing"

func TestNewServiceRejectsHalfSetCredentials(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"only access key id", Config{Region: "r", BucketName: "b", AccessKeyID: "akid"}},
		{"only secret access key", Config{Region: "r", BucketName: "b", SecretAccessKey: "secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewService(&tt.cfg); err == nil {
				t.Fatal("NewService: err = nil, want error")
			}
		})
	}
}
