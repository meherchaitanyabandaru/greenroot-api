// Package secrets loads the API's settings from AWS Secrets Manager.
//
// In production every secret (JWT, database and Redis URLs, Razorpay, SMS,
// storage keys, ...) lives in one Secrets Manager secret holding a JSON object
// of environment-variable names to values:
//
//	{"JWT_SECRET": "…", "DATABASE_URL": "postgres://…", "RAZORPAY_KEY_ID": "rzp_live_…"}
//
// Set AWS_SECRETS_ID to its name or ARN and the API reads it at start-up,
// before the configuration is loaded. Rotating a key = editing the secret and
// restarting the container; nothing is baked into images or files.
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// EnvVar is the variable naming the secret to load.
const EnvVar = "AWS_SECRETS_ID"

// Getter is the part of the Secrets Manager client used here (faked in tests).
type Getter interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

var envName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Load fetches AWS_SECRETS_ID (if set) and exports its keys as environment
// variables. A variable already set in the environment wins, so one value can
// be overridden for a single run without editing the secret. It returns the
// names it set (never the values).
func Load(ctx context.Context) ([]string, error) {
	id := os.Getenv(EnvVar)
	if id == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: aws config: %w", err)
	}
	return LoadFrom(ctx, secretsmanager.NewFromConfig(cfg), id)
}

// LoadFrom is Load with an explicit client.
func LoadFrom(ctx context.Context, client Getter, id string) ([]string, error) {
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &id})
	if err != nil {
		return nil, fmt.Errorf("secrets: read %s: %w", id, err)
	}
	if out.SecretString == nil {
		return nil, fmt.Errorf("secrets: %s has no string value (store a JSON object)", id)
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(*out.SecretString), &values); err != nil {
		return nil, fmt.Errorf("secrets: %s must be a JSON object of string values: %w", id, err)
	}
	var set []string
	for name, value := range values {
		if !envName.MatchString(name) {
			return nil, fmt.Errorf("secrets: %s: key %q is not an environment variable name", id, name)
		}
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return nil, err
		}
		set = append(set, name)
	}
	sort.Strings(set)
	return set, nil
}
