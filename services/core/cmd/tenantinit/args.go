package main

// tenantSpec is one --tenant value: the ID and the name of a tenant row.
type tenantSpec struct{ id, name string }

// settings are the validated arguments of a run.
type settings struct {
	dsnFile, keyDir string
	keyMaxActive    int
	tenants         []tenantSpec
}

// parseArgs validates the arguments. false is a usage refusal; nothing about the arguments is reported beyond that.
func parseArgs(args []string) (settings, bool) {
	_ = args
	return settings{}, false
}
