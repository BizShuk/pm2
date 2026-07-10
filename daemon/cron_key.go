package daemon

// cronKey is the canonical "namespace:name" key used by the
// process registry, the cron scheduler, and the metrics collector.
// Centralised so that if the key format ever needs to change
// (e.g. to handle a name containing ':') there is exactly one
// site to update.
func cronKey(ns, name string) string {
	return ns + ":" + name
}
