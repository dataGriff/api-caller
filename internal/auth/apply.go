package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go/logging"
)

// Cache stores tokens between invocations (backed by the apic session).
type Cache interface {
	Get(key string) (string, bool)
	Set(key, value string) error
}

// Env is what Apply needs from the caller.
type Env struct {
	Cache     Cache
	Client    *http.Client // used for token endpoints
	AllowExec bool
	Stderr    io.Writer // device-code prompts
	Now       func() time.Time
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Apply adds credentials to req according to a rendered spec. body is the
// request body as sent (needed for AWS payload hashing).
func Apply(ctx context.Context, s *Spec, req *http.Request, body []byte, env *Env) error {
	switch s.Type {
	case "none":
		return nil
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+s.Args[0])
	case "basic":
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s.Args[0]+":"+s.Args[1])))
	case "aws":
		return applyAWS(ctx, s, req, body, env)
	case "oauth2":
		tok, err := oauth2Token(ctx, s, env)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	case "exec":
		return applyExec(ctx, s, req, env)
	default:
		return fmt.Errorf("unknown auth type %q", s.Type)
	}
	return nil
}

func applyAWS(ctx context.Context, s *Spec, req *http.Request, body []byte, env *Env) error {
	opts := []func(*config.LoadOptions) error{config.WithLogger(logging.Nop{})}
	if p := s.Options["profile"]; p != "" {
		opts = append(opts, config.WithSharedConfigProfile(p))
	}
	if r := s.Options["region"]; r != "" {
		opts = append(opts, config.WithRegion(r))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("aws: load config: %w", err)
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("aws: no credentials found (set AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, AWS_PROFILE, or log in with `aws sso login`): %w", err)
	}
	region := cfg.Region
	if region == "" {
		return fmt.Errorf("aws: no region: add region=.. to @auth aws, set AWS_REGION, or configure it in your profile")
	}
	service := s.Options["service"]
	if service == "" {
		service = "execute-api"
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	req.Header.Set("X-Amz-Content-Sha256", hash)
	signer := v4.NewSigner()
	return signer.SignHTTP(ctx, creds, req, hash, service, region, env.now().UTC())
}

// SignAWS is exported for tests: it signs req with explicit credentials.
func SignAWS(ctx context.Context, req *http.Request, body []byte, creds aws.Credentials, service, region string, at time.Time) error {
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	req.Header.Set("X-Amz-Content-Sha256", hash)
	return v4.NewSigner().SignHTTP(ctx, creds, req, hash, service, region, at.UTC())
}

func applyExec(ctx context.Context, s *Spec, req *http.Request, env *Env) error {
	if !env.AllowExec {
		return fmt.Errorf("@auth exec is disabled: set `auth:\n  allowExec: true` in apic.yaml to let request files run %q", strings.Join(s.Args, " "))
	}
	header := s.Options["header"]
	if header == "" {
		header = "Authorization"
	}
	prefix, hasPrefix := s.Options["prefix"]
	if !hasPrefix {
		prefix = "Bearer"
	}
	key := "$exec:" + hashKey(strings.Join(s.Args, "\x00"))
	var ttl time.Duration
	if t := s.Options["ttl"]; t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return fmt.Errorf("@auth exec: bad ttl %q", t)
		}
		ttl = d
	}
	var token string
	if ttl > 0 && env.Cache != nil {
		if raw, ok := env.Cache.Get(key); ok {
			if c, err := decodeToken(raw); err == nil && c.ExpiresAt.After(env.now()) {
				token = c.AccessToken
			}
		}
	}
	if token == "" {
		cmd := exec.CommandContext(ctx, s.Args[0], s.Args[1:]...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("@auth exec %s: %v: %s", s.Args[0], err, strings.TrimSpace(stderr.String()))
		}
		token = strings.TrimSpace(string(out))
		if token == "" {
			return fmt.Errorf("@auth exec %s: command printed nothing", s.Args[0])
		}
		if ttl > 0 && env.Cache != nil {
			_ = env.Cache.Set(key, encodeToken(cachedToken{AccessToken: token, ExpiresAt: env.now().Add(ttl)}))
		}
	}
	if prefix != "" {
		token = prefix + " " + token
	}
	req.Header.Set(header, token)
	return nil
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
