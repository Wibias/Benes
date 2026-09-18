package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/anthropic"
	"github.com/Wibias/Benes/internal/oauth/commandcode"
	"github.com/Wibias/Benes/internal/oauth/xai"
)

func runCallbackOAuthLogin(ctx context.Context, provider, callback string, stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	authPath := filepath.Join(paths.Home, "auth.json")
	switch provider {
	case xai.ProviderID:
		pending := xai.PendingFile{Path: filepath.Join(paths.Home, xai.PendingFileName)}
		if callback == "" {
			login, err := xai.NewPendingLogin(ctx)
			if err != nil {
				return serveFailure(stderr, "start xai login", err)
			}
			authURL, err := login.AuthURL()
			if err != nil {
				return serveFailure(stderr, "build xai auth url", err)
			}
			if err := pending.Save(login); err != nil {
				return serveFailure(stderr, "save xai login", err)
			}
			fmt.Fprintln(stdout, authURL)
			fmt.Fprintln(stdout, "Complete xAI/Grok login, then run: benes login xai --callback <redirect-url-or-code>")
			return 0
		}
		completed, err := pending.Complete(ctx, callback)
		if err != nil {
			return serveFailure(stderr, "complete xai login", err)
		}
		return persistDeviceAccount(stdout, stderr, authPath, xai.Persist, completed.Account)
	case anthropic.ProviderID:
		pending := anthropic.PendingFile{Path: filepath.Join(paths.Home, anthropic.PendingFileName)}
		if callback == "" {
			login, err := anthropic.NewPendingLogin()
			if err != nil {
				return serveFailure(stderr, "start anthropic login", err)
			}
			authURL, err := login.AuthURL()
			if err != nil {
				return serveFailure(stderr, "build anthropic auth url", err)
			}
			if err := pending.Save(login); err != nil {
				return serveFailure(stderr, "save anthropic login", err)
			}
			fmt.Fprintln(stdout, authURL)
			fmt.Fprintln(stdout, "Complete Claude login, then run: benes login anthropic --callback <redirect-url-or-code>")
			return 0
		}
		completed, err := pending.Complete(ctx, callback)
		if err != nil {
			return serveFailure(stderr, "complete anthropic login", err)
		}
		return persistDeviceAccount(stdout, stderr, authPath, anthropic.Persist, completed.Account)
	case commandcode.ProviderID:
		pending := commandcode.PendingFile{Path: filepath.Join(paths.Home, commandcode.PendingFileName)}
		if callback == "" {
			login, err := commandcode.NewPendingLogin()
			if err != nil {
				return serveFailure(stderr, "start command-code login", err)
			}
			if err := pending.Save(login); err != nil {
				return serveFailure(stderr, "save command-code login", err)
			}
			fmt.Fprintln(stdout, login.AuthURL())
			fmt.Fprintln(stdout, "Complete Command Code login, then run: benes login command-code --callback <json-or-api-key>")
			return 0
		}
		completed, err := pending.Complete(ctx, callback)
		if err != nil {
			return serveFailure(stderr, "complete command-code login", err)
		}
		return persistDeviceAccount(stdout, stderr, authPath, commandcode.Persist, completed.Account)
	default:
		fmt.Fprintf(stderr, "benes: unsupported login provider %q\n", provider)
		return 2
	}
}
