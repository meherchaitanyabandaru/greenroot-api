package secrets

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type fakeGetter struct {
	value *string
	err   error
}

func (f fakeGetter) GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return &secretsmanager.GetSecretValueOutput{SecretString: f.value}, f.err
}

func str(s string) *string { return &s }

func TestLoadFromSetsUnsetVariablesOnly(t *testing.T) {
	t.Setenv("GR_TEST_KEEP", "from-env")
	os.Unsetenv("GR_TEST_NEW")
	t.Cleanup(func() { os.Unsetenv("GR_TEST_NEW") })

	set, err := LoadFrom(context.Background(), fakeGetter{value: str(`{"GR_TEST_NEW":"from-secret","GR_TEST_KEEP":"ignored"}`)}, "greenroot/prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 1 || set[0] != "GR_TEST_NEW" {
		t.Fatalf("set = %v, want only GR_TEST_NEW", set)
	}
	if os.Getenv("GR_TEST_NEW") != "from-secret" || os.Getenv("GR_TEST_KEEP") != "from-env" {
		t.Fatal("secret must fill unset variables and never override the environment")
	}
}

func TestLoadFromRejectsBadSecrets(t *testing.T) {
	for name, g := range map[string]fakeGetter{
		"aws error":    {err: errors.New("AccessDenied")},
		"binary":       {},
		"not json":     {value: str("JWT_SECRET=abc")},
		"bad key name": {value: str(`{"jwt secret":"x"}`)},
		"non-string":   {value: str(`{"HTTP_PORT":8080}`)},
	} {
		if _, err := LoadFrom(context.Background(), g, "greenroot/prod"); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "x\"") {
			t.Errorf("%s: error leaks a value: %v", name, err)
		}
	}
}

func TestLoadWithoutSecretIDIsNoop(t *testing.T) {
	t.Setenv(EnvVar, "")
	if set, err := Load(context.Background()); err != nil || set != nil {
		t.Fatalf("Load without %s: %v %v", EnvVar, set, err)
	}
}
