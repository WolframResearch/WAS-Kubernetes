package supportbundle

import (
	"strings"
	"testing"
)

func TestRedactText_AWSKey(t *testing.T) {
	in := "access key: AKIAIOSFODNN7EXAMPLE and then some text"
	out := RedactText(in)
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("AWS key not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED_AWS_KEY]") {
		t.Fatalf("expected [REDACTED_AWS_KEY] in: %s", out)
	}
}

func TestRedactText_BearerJWT(t *testing.T) {
	token := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	in := "Authorization: Bearer " + token
	out := RedactText(in)
	if strings.Contains(out, token) {
		t.Fatalf("JWT not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED_JWT]") {
		t.Fatalf("expected [REDACTED_JWT] in: %s", out)
	}
}

func TestRedactText_GenericJWT(t *testing.T) {
	// A JWT appearing without "Bearer" prefix
	token := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0"
	out := RedactText("token=" + token)
	if strings.Contains(out, "eyJ") {
		t.Fatalf("generic JWT not redacted: %s", out)
	}
}

func TestRedactText_AzureConnectionString(t *testing.T) {
	in := `DefaultEndpointsProtocol=https;AccountName=mystorageaccount;AccountKey=abc123==`
	out := RedactText(in)
	if strings.Contains(out, "mystorageaccount") {
		t.Fatalf("Azure connection string not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in: %s", out)
	}
}

func TestRedactText_SensitiveKV(t *testing.T) {
	cases := []struct {
		in      string
		badWord string
	}{
		{"password=supersecret123", "supersecret123"},
		{"secret=my-secret-value", "my-secret-value"},
		{"db_password: letmein99", "letmein99"},
	}
	for _, tc := range cases {
		out := RedactText(tc.in)
		if strings.Contains(out, tc.badWord) {
			t.Fatalf("sensitive value not redacted in %q: got %q", tc.in, out)
		}
	}
}

func TestRedactText_PreservesNonSensitive(t *testing.T) {
	in := "cluster=was-prod region=us-east-1 status=healthy"
	out := RedactText(in)
	if out != in {
		t.Fatalf("non-sensitive text was altered:\n  want: %q\n  got:  %q", in, out)
	}
}

func TestRedactEnvVars_Allowlist(t *testing.T) {
	env := []string{
		"PATH=/usr/bin:/bin",
		"AWS_PROFILE=my-profile",
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY=secret",
		"AWS_SESSION_TOKEN=tok",
		"TOKEN_XYZ=super",
		"HOME=/home/user",
	}
	out := RedactEnvVars(env)

	for _, e := range out {
		k, _, _ := strings.Cut(e, "=")
		if k == "AWS_ACCESS_KEY_ID" || k == "AWS_SECRET_ACCESS_KEY" || k == "AWS_SESSION_TOKEN" {
			t.Fatalf("sensitive key %q leaked into output", k)
		}
	}

	found := false
	for _, e := range out {
		if strings.HasPrefix(e, "PATH=") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected PATH to be present in output")
	}
}

func TestRedactHelmValues_RedactsPassword(t *testing.T) {
	input := `{"database":{"password":"supersecret","host":"db.example.com"}}`
	out, redacted, err := RedactHelmValues([]byte(input))
	if err != nil {
		t.Fatalf("RedactHelmValues: %v", err)
	}
	if strings.Contains(string(out), "supersecret") {
		t.Fatalf("password not redacted in: %s", out)
	}
	if len(redacted) == 0 {
		t.Fatal("expected at least one redacted key path")
	}
}

func TestRedactHelmValues_NestedArray(t *testing.T) {
	input := `{"services":[{"secretKey":"mysecret"},{"name":"public"}]}`
	out, _, err := RedactHelmValues([]byte(input))
	if err != nil {
		t.Fatalf("RedactHelmValues: %v", err)
	}
	if strings.Contains(string(out), "mysecret") {
		t.Fatalf("nested secret not redacted in: %s", out)
	}
}

func TestRedactHelmValues_NotJSON(t *testing.T) {
	input := []byte("password=plaintext\nAKIAIOSFODNN7EXAMPLE")
	out, _, err := RedactHelmValues(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(out), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("AWS key in non-JSON input not redacted")
	}
}

func TestMaskAccountID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"123456789012", "1234****9012"},
		{"000000000000", "0000****0000"},
		{"short", "short"},           // non-12 digit, pass-through
		{"1234567890123", "1234567890123"}, // 13 digits, pass-through
	}
	for _, tc := range cases {
		if got := MaskAccountID(tc.in); got != tc.want {
			t.Fatalf("MaskAccountID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactWorkspaceJSON_MasksAccountID(t *testing.T) {
	input := `{"clusterName":"was-prod","awsAccountID":"123456789012","status":"active"}`
	out, err := RedactWorkspaceJSON([]byte(input))
	if err != nil {
		t.Fatalf("RedactWorkspaceJSON: %v", err)
	}
	if strings.Contains(string(out), "123456789012") {
		t.Fatalf("account ID not masked in: %s", out)
	}
	if !strings.Contains(string(out), "1234****9012") {
		t.Fatalf("expected masked account ID in: %s", out)
	}
}

func TestRedactWorkspaceJSON_InvalidJSON(t *testing.T) {
	// Falls back to text redaction
	input := []byte("not json, AKIAIOSFODNN7EXAMPLE")
	out, err := RedactWorkspaceJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(out), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("AWS key not redacted in fallback path")
	}
}
