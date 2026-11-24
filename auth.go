package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type authOptions struct {
	revoke bool
}

func authCmd(fs *flag.FlagSet) runFunc {
	var opts authOptions
	fs.BoolVar(&opts.revoke, "revoke", false, "revoke and delete the saved token")
	return func(ctx context.Context, cfg *config, args []string) error {
		return auth(ctx, cfg, opts, args)
	}
}

func (cfg *config) oauthConfig() (*oauth2.Config, error) {
	buf, err := os.ReadFile(cfg.Credentials)
	if err != nil {
		return nil, fmt.Errorf("reading credentials: %w", err)
	}
	return google.ConfigFromJSON(buf, gmail.GmailModifyScope)
}

func readToken(path string) (*oauth2.Token, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(buf, &tok); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &tok, nil
}

func writeToken(path string, tok *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0600)
}

type savingTokenSource struct {
	src  oauth2.TokenSource
	path string
	last string
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != s.last {
		if err := writeToken(s.path, tok); err != nil {
			return nil, err
		}
		s.last = tok.AccessToken
	}
	return tok, nil
}

func (cfg *config) newService(ctx context.Context) (*gmail.Service, error) {
	logOut = cfg.errOut
	if cfg.service != nil {
		return cfg.service, nil
	}
	oc, err := cfg.oauthConfig()
	if err != nil {
		return nil, err
	}

	if verbose {
		fmt.Fprintf(cfg.errOut, "oauth: credentials:%s token:%s\n", cfg.Credentials, cfg.Token)
	}

	tok, err := readToken(cfg.Token)
	if errors.Is(err, os.ErrNotExist) {
		tok, err = authorize(ctx, oc, cfg.errOut)
		if err != nil {
			return nil, err
		}
		if err := writeToken(cfg.Token, tok); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	ts := oauth2.ReuseTokenSource(tok,
		&savingTokenSource{src: oc.TokenSource(ctx, tok), path: cfg.Token, last: tok.AccessToken})
	if _, err := ts.Token(); err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
			return nil, fmt.Errorf("token expired or revoked; run %s auth", cfg.program)
		}
		return nil, err
	}

	client := oauth2.NewClient(ctx, ts)
	if verbose {
		client.Transport = &logTransport{base: client.Transport, w: cfg.errOut}
	}
	return gmail.NewService(ctx, option.WithHTTPClient(client))
}

type logTransport struct {
	base http.RoundTripper
	w    io.Writer
}

func (lt *logTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	q := req.URL.Query()
	q.Del("alt")
	q.Del("prettyPrint")
	target := req.URL.Path
	if len(q) > 0 {
		if s, err := url.QueryUnescape(q.Encode()); err == nil {
			target += "?" + s
		}
	}

	start := time.Now()
	resp, err := lt.base.RoundTrip(req)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Fprintf(lt.w, "%s %s: %s (%s)\n", req.Method, target, err, elapsed)
	} else {
		fmt.Fprintf(lt.w, "%s %s: %s (%s)\n", req.Method, target, resp.Status, elapsed)
	}
	return resp, err
}

func authorize(ctx context.Context, oc *oauth2.Config, w io.Writer) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	oc.RedirectURL = fmt.Sprintf("http://%s/", ln.Addr())
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()

	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}

		var res result
		switch {
		case q.Get("error") != "":
			res.err = fmt.Errorf("authorization failed: %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("authorization failed: no code returned")
		default:
			res.code = q.Get("code")
		}
		if res.err != nil {
			http.Error(w, res.err.Error(), http.StatusBadRequest)
		} else {
			fmt.Fprintln(w, "gmailt is authorized; you can close this window.")
		}
		select {
		case ch <- res:
		default:
		}
	})}
	go srv.Serve(ln)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	authURL := oc.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce,
		oauth2.S256ChallengeOption(verifier))
	fmt.Fprintf(w, "open this URL in a browser to authorize gmailt:\n\n%s\n\n", authURL)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return oc.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	case <-ctx.Done():
		return nil, errors.New("timed out waiting for authorization")
	}
}

func revokeToken(ctx context.Context, tok *oauth2.Token) error {
	t := tok.RefreshToken
	if t == "" {
		t = tok.AccessToken
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/revoke",
		strings.NewReader(url.Values{"token": {t}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("revoke: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func auth(ctx context.Context, cfg *config, opts authOptions, args []string) error {
	if len(args) != 0 {
		return usageError("[-revoke]")
	}

	if opts.revoke {
		tok, err := readToken(cfg.Token)
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no token: %s", cfg.Token)
		} else if err != nil {
			return err
		}
		if err := revokeToken(ctx, tok); err != nil {
			warn(cfg, err)
		}
		if err := os.Remove(cfg.Token); err != nil {
			return err
		}
		fmt.Fprintf(cfg.out, "deleted %s\n", cfg.Token)
		return nil
	}

	oc, err := cfg.oauthConfig()
	if err != nil {
		return err
	}
	tok, err := authorize(ctx, oc, cfg.errOut)
	if err != nil {
		return err
	}
	if err := writeToken(cfg.Token, tok); err != nil {
		return err
	}

	svc, err := cfg.newService(ctx)
	if err != nil {
		return err
	}
	profile, err := svc.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.out, "authorized as %s (%d messages); token saved to %s\n", profile.EmailAddress,
		profile.MessagesTotal, cfg.Token)
	return nil
}
