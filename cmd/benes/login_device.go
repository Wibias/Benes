package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/githubcopilot"
	"github.com/Wibias/Benes/internal/oauth/kimi"
	"github.com/Wibias/Benes/internal/oauth/nous"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func runDeviceOAuthLogin(ctx context.Context, provider string, stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	authPath := filepath.Join(paths.Home, "auth.json")
	switch provider {
	case kimi.ProviderID:
		pending := kimi.PendingFile{Path: filepath.Join(paths.Home, kimi.PendingFileName)}
		login, err := kimi.NewPendingLogin(ctx, paths.Home)
		if err != nil {
			return serveFailure(stderr, "start kimi login", err)
		}
		if err := pending.Save(login); err != nil {
			return serveFailure(stderr, "save kimi login", err)
		}
		fmt.Fprintln(stdout, login.AuthURL())
		fmt.Fprintf(stdout, "Enter code: %s\n", login.UserCode)
		completed, err := pending.Complete(ctx, paths.Home)
		if err != nil {
			return serveFailure(stderr, "complete kimi login", err)
		}
		return persistDeviceAccount(stdout, stderr, authPath, kimi.Persist, completed.Account)
	case nous.ProviderID:
		pending := nous.PendingFile{Path: filepath.Join(paths.Home, nous.PendingFileName)}
		login, err := nous.NewPendingLogin(ctx)
		if err != nil {
			return serveFailure(stderr, "start nous login", err)
		}
		if err := pending.Save(login); err != nil {
			return serveFailure(stderr, "save nous login", err)
		}
		fmt.Fprintln(stdout, login.AuthURL())
		fmt.Fprintf(stdout, "Enter code: %s\n", login.UserCode)
		completed, err := pending.Complete(ctx)
		if err != nil {
			return serveFailure(stderr, "complete nous login", err)
		}
		return persistDeviceAccount(stdout, stderr, authPath, nous.Persist, completed.Account)
	case githubcopilot.ProviderID:
		pending := githubcopilot.PendingFile{Path: filepath.Join(paths.Home, githubcopilot.PendingFileName)}
		login, err := githubcopilot.NewPendingLogin(ctx)
		if err != nil {
			return serveFailure(stderr, "start github-copilot login", err)
		}
		if err := pending.Save(login); err != nil {
			return serveFailure(stderr, "save github-copilot login", err)
		}
		fmt.Fprintln(stdout, login.AuthURL())
		fmt.Fprintf(stdout, "Enter code: %s\n", login.UserCode)
		completed, err := pending.Complete(ctx)
		if err != nil {
			return serveFailure(stderr, "complete github-copilot login", err)
		}
		return persistDeviceAccount(stdout, stderr, authPath, githubcopilot.Persist, completed.Account)
	default:
		fmt.Fprintf(stderr, "benes: unsupported login provider %q\n", provider)
		return 2
	}
}

func persistDeviceAccount(stdout, stderr io.Writer, authPath string, persist func(string, antigravity.StoredAccount) error, account antigravity.StoredAccount) int {
	if err := persist(authPath, account); err != nil {
		return serveFailure(stderr, "persist account", err)
	}
	if account.Email != "" {
		fmt.Fprintf(stdout, "logged in %s (%s)\n", account.ID, account.Email)
		return 0
	}
	fmt.Fprintf(stdout, "logged in %s\n", account.ID)
	return 0
}
