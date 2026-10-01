package di

import sharedgodi "github.com/assurrussa/godi"

// ModuleBootstrap registers helpers to assemble bootstrap dependencies for modern gonotify.
func ModuleBootstrap() sharedgodi.Dependencies {
	return sharedgodi.CollectDependencies(
		sharedgodi.NewDependency(ProvideNotifyHubClient, sharedgodi.WithKey(KeyNotifyHubClient)),
		sharedgodi.NewDependency(ProvideOutboxJob, sharedgodi.WithKey(KeyNotificationsJob)),
	)
}
