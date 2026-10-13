package app

import (
	"testing"

	"github.com/caarlos0/env/v11"
)

func TestLoadInfoFromEnvNames(t *testing.T) {
	tests := []struct {
		key   string
		field func(Info) string
	}{
		{"APP_NAME", func(i Info) string { return i.Name }},
		{"APP_URL", func(i Info) string { return i.URL }},
		{"APP_TERMS_OF_SERVICE_URL", func(i Info) string { return i.TermsOfServiceURL }},
		{"APP_PRIVACY_POLICY_URL", func(i Info) string { return i.PrivacyPolicyURL }},
		{"APP_EMAIL", func(i Info) string { return i.Email }},
		{"APP_NOTIFICATION_EMAIL_SENDER", func(i Info) string { return i.NotificationEmailSender }},
		{"APP_TEAM_NAME", func(i Info) string { return i.TeamName }},
		{"APP_ENV", func(i Info) string { return i.Env }},
		{"APP_TZ_LOCATION", func(i Info) string { return i.TZLocation }},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			info := DefaultInfo()
			opts := env.Options{
				Prefix:      EnvPrefixDefault,
				Environment: map[string]string{tt.key: "from-env"},
			}
			if err := loadInfoFromEnv(&info, opts); err != nil {
				t.Fatalf("loadInfoFromEnv: %v", err)
			}
			if got := tt.field(info); got != "from-env" {
				t.Errorf("field for %s = %q, want %q", tt.key, got, "from-env")
			}
		})
	}
}

func TestLoadInfoFromEnvKeepsDefaults(t *testing.T) {
	info := DefaultInfo()
	opts := env.Options{Prefix: EnvPrefixDefault, Environment: map[string]string{}}
	if err := loadInfoFromEnv(&info, opts); err != nil {
		t.Fatalf("loadInfoFromEnv: %v", err)
	}
	if want := DefaultInfo(); info != want {
		t.Errorf("info = %+v, want defaults %+v", info, want)
	}
}
