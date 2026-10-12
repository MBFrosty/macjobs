package redact

import (
	"strings"
	"testing"
)

func TestCronText(t *testing.T) {
	for _, line := range []string{
		`MODE=x TOKEN="private words" /bin/echo example`,
		`MODE="private"suffix /bin/echo example`,
		`MODE=$(echo private words) /bin/echo example`,
		`MODE="private escaped\"words" /bin/echo example`,
		`MODE='private incomplete`,
		`/usr/bin/curl --token "private escaped\"words"`,
		`/usr/bin/curl -u 'alice:private'password`,
		`MODE='private words' /bin/echo example`,
		`MODE=private\ words /bin/echo example`,
		`/usr/bin/curl https://alice:private-value@example.com/ --token private-token`,
		`/usr/bin/curl --password=private-pass --api-key "private key"`,
		`/usr/bin/curl -H 'Authorization: Bearer private-bearer'`,
		`/usr/bin/curl -u alice:private-password`,
		`/usr/bin/curl -u "alice:private password"`,
	} {
		s := (Redactor{}).CronText(line)
		if strings.Contains(s, "private") || strings.Contains(s, "MODE=x") {
			t.Fatal("credential exposed", s)
		}
		if !strings.Contains(s, Hidden) {
			t.Fatal(s)
		}
	}
	r := Redactor{Values: CronValues([]string{"normal-long-value", "short"})}
	if r.CronText("echo normal-long-value short") != "echo [REDACTED] short" {
		t.Fatal(r)
	}
}
