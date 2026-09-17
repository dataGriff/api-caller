package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// awsSettings is what the aws auth type needs to sign a request.
type awsSettings struct {
	Creds  AWSCredentials
	Region string
	Source string // where the credentials came from, for error messages and describe
}

// resolveAWS finds credentials and a region the way the AWS CLI does, without
// the SDK:
//
//  1. AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN (unless profile= is set)
//  2. static keys in ~/.aws/credentials or ~/.aws/config for the profile
//  3. `aws configure export-credentials --profile <p>` when the AWS CLI v2 is installed,
//     which resolves SSO, assumed roles, credential_process and the rest
//
// Region: region= option, AWS_REGION, AWS_DEFAULT_REGION, then the profile's region.
func resolveAWS(ctx context.Context, s *Spec) (*awsSettings, error) {
	profile := s.Options["profile"]
	explicitProfile := profile != ""
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		profile = "default"
	}
	out := &awsSettings{Region: s.Options["region"]}
	if out.Region == "" {
		out.Region = firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"))
	}

	credFile := readINI(firstNonEmpty(os.Getenv("AWS_SHARED_CREDENTIALS_FILE"), filepath.Join(homeDir(), ".aws", "credentials")))
	confFile := readINI(firstNonEmpty(os.Getenv("AWS_CONFIG_FILE"), filepath.Join(homeDir(), ".aws", "config")))
	confSection := confFile["profile "+profile]
	if profile == "default" {
		confSection = mergeINI(confFile["default"], confSection)
	}
	if out.Region == "" {
		out.Region = confSection["region"]
	}

	id, secret := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
	if !explicitProfile && id != "" && secret != "" {
		out.Creds = AWSCredentials{AccessKeyID: id, SecretAccessKey: secret, SessionToken: os.Getenv("AWS_SESSION_TOKEN")}
		out.Source = "environment"
		return out, out.checkRegion()
	}
	for _, sec := range []map[string]string{credFile[profile], confSection} {
		if sec["aws_access_key_id"] != "" && sec["aws_secret_access_key"] != "" {
			out.Creds = AWSCredentials{AccessKeyID: sec["aws_access_key_id"], SecretAccessKey: sec["aws_secret_access_key"], SessionToken: sec["aws_session_token"]}
			out.Source = "profile " + profile
			return out, out.checkRegion()
		}
	}
	if cli, err := exec.LookPath("aws"); err == nil {
		creds, err := exportCredentials(ctx, cli, profile)
		if err != nil {
			return nil, err
		}
		out.Creds, out.Source = creds, "aws cli, profile "+profile
		return out, out.checkRegion()
	}
	return nil, fmt.Errorf("aws: no credentials for profile %q: set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, add the profile to ~/.aws/credentials, or install the AWS CLI so SSO and assumed-role profiles can be used", profile)
}

func (s *awsSettings) checkRegion() error {
	if s.Region == "" {
		return errors.New("aws: no region: add region=.. to @auth aws, set AWS_REGION, or put region in the profile")
	}
	return nil
}

// exportCredentials asks the AWS CLI for the profile's current credentials.
func exportCredentials(ctx context.Context, cli, profile string) (AWSCredentials, error) {
	cmd := exec.CommandContext(ctx, cli, "configure", "export-credentials", "--profile", profile, "--format", "process")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return AWSCredentials{}, fmt.Errorf("aws: `aws configure export-credentials --profile %s` failed: %s", profile, msg)
	}
	var v struct {
		AccessKeyID     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || v.AccessKeyID == "" {
		return AWSCredentials{}, fmt.Errorf("aws: unexpected output from `aws configure export-credentials` (needs AWS CLI v2)")
	}
	return AWSCredentials{AccessKeyID: v.AccessKeyID, SecretAccessKey: v.SecretAccessKey, SessionToken: v.SessionToken}, nil
}

// readINI parses the subset of INI used by AWS config files: [sections] and
// key = value lines. Missing files yield an empty map.
func readINI(path string) map[string]map[string]string {
	out := map[string]map[string]string{}
	f, err := os.Open(path) //nolint:gosec // the user's own ~/.aws credentials file
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && section != "" {
			if strings.HasPrefix(sc.Text(), " ") || strings.HasPrefix(sc.Text(), "\t") {
				continue // nested keys (e.g. s3 = ...) are not needed
			}
			out[section][strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func mergeINI(base, over map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
