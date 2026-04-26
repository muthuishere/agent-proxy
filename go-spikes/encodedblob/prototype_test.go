package encodedblob

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestBase64ConnectionString(t *testing.T) {
	secret := "postgres://user:pass@host:5432/prod"
	input := base64.StdEncoding.EncodeToString([]byte(secret))

	assertRoundTrip(t, []string{secret}, input, OptionB, 2)
	assertRoundTrip(t, []string{secret}, input, OptionC, 2)
}

func TestBase64DotenvBlock(t *testing.T) {
	secrets := []string{
		"postgres://user:pass@host:5432/prod",
		"sk-ant-api03-abcdefghijklmnop",
	}
	input := base64.StdEncoding.EncodeToString([]byte(strings.Join([]string{
		"DATABASE_URL=" + secrets[0],
		"ANTHROPIC_API_KEY=" + secrets[1],
	}, "\n")))

	assertRoundTrip(t, secrets, input, OptionB, 2)
	assertRoundTrip(t, secrets, input, OptionC, 2)
}

func TestHexEncodedSecret(t *testing.T) {
	secret := "ghp_exampleSecret123456"
	input := hex.EncodeToString([]byte(secret))

	assertRoundTrip(t, []string{secret}, input, OptionB, 2)
	assertRoundTrip(t, []string{secret}, input, OptionC, 2)
}

func TestURLEncodedSecret(t *testing.T) {
	secret := "aws_secret_access_key=abcdEFGHijklMNOP1234"
	input := url.QueryEscape(secret)

	assertRoundTrip(t, []string{secret}, input, OptionB, 2)
	assertRoundTrip(t, []string{secret}, input, OptionC, 2)
}

func TestNestedEncodedDepthFour(t *testing.T) {
	secret := "postgres://deep:user@host:5432/prod"
	level1 := url.QueryEscape(secret)
	level2 := base64.StdEncoding.EncodeToString([]byte(level1))
	level3 := hex.EncodeToString([]byte(level2))
	level4 := base64.StdEncoding.EncodeToString([]byte(level3))

	assertRoundTrip(t, []string{secret}, level4, OptionB, 4)
	assertRoundTrip(t, []string{secret}, level4, OptionC, 4)
}

func TestJSONStructurePreserved(t *testing.T) {
	secret := "postgres://user:pass@host:5432/prod"
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	input := `{"data":"` + encoded + `","note":"ok"}`

	for _, strategy := range []Strategy{OptionB, OptionC} {
		proto := NewPrototype([]string{secret}, 3)
		masked := proto.Mask(input, strategy)
		if !json.Valid([]byte(masked)) {
			t.Fatalf("expected valid json for %s", strategy)
		}
		restored := proto.Restore(masked, strategy)
		if restored != input {
			t.Fatalf("expected restore to match original for %s", strategy)
		}
	}
}

func TestOptionCMasksWholeBlobWhileOptionBPreservesContext(t *testing.T) {
	secret := "postgres://user:pass@host:5432/prod"
	block := "DATABASE_URL=" + secret + "\nMODE=prod"
	input := base64.StdEncoding.EncodeToString([]byte(block))

	optionB := NewPrototype([]string{secret}, 2).Mask(input, OptionB)
	optionC := NewPrototype([]string{secret}, 2).Mask(input, OptionC)

	if strings.Contains(optionB, "[MASKED:") {
		t.Fatalf("expected option b to re-encode token, not leave raw token in outer payload")
	}
	if !strings.Contains(optionC, "[MASKED:") {
		t.Fatalf("expected option c to replace whole blob with token")
	}
}

func assertRoundTrip(t *testing.T, secrets []string, input string, strategy Strategy, depth int) {
	t.Helper()
	proto := NewPrototype(secrets, depth)
	masked := proto.Mask(input, strategy)
	if masked == input {
		t.Fatalf("expected masked payload to change for %s", strategy)
	}
	for _, secret := range secrets {
		if strings.Contains(masked, secret) {
			t.Fatalf("expected masked payload to omit raw secret for %s", strategy)
		}
	}
	restored := proto.Restore(masked, strategy)
	if restored != input {
		t.Fatalf("expected restore to recover original for %s", strategy)
	}
}
