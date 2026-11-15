package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// recordpools.go builds the Record reader's per-tenant pools (EDG-01 slice 3c, part 3; ruling A170 and the default for OD-P-82): a directory with one file per tenant, named
// by the tenant ID, each holding that tenant's record_read DSN. The database, not this code, decides which tenant's chain a connection can read (DEBT-04): this code only
// refuses to hand out any pool but the tenant's own. A DSN holds a password, so no message here carries a DSN or any part of one (AGENT-13).

// EnvRecordReadDSNDir names the directory of DSN files. Unset means no Record reader; there is no default directory.
const EnvRecordReadDSNDir = "NOEMA_RECORD_READ_DSN_DIR"

// tenantFileRe is the name of a DSN file: a tenant ID of common.v1, as written (anchored, case-sensitive).
var tenantFileRe = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)

// Trust boundary: the files are an operator step. This code checks the shape of the directory (regular files named by a tenant ID, no link) and that no login appears twice.
// It does not check that a login is the tenant's own record_read login, or that it is not a login of wider reach (a superuser, record_verify): the database's own binding is
// the defence for that (DEBT-04), and the operator who mounts the files is responsible for what each holds.

// recordPools is a reconstruct.TenantDBs over one pool per tenant.
type recordPools struct{ pools map[string]*pgxpool.Pool }

// For returns the tenant's own pool, or an error. It never falls back to another tenant's pool or to a default.
func (p *recordPools) For(tenantID string) (reconstruct.TxBeginner, error) {
	pool, ok := p.pools[tenantID]
	if !ok || !tenantFileRe.MatchString(tenantID) {
		return nil, errors.New("cmd/core: no Record reader is configured for this tenant")
	}
	return pool, nil
}

// buildRecordReader reads the DSN files and returns the reader and a function that closes every pool (safe to call more than once). With the setting unset it returns no
// reader, a function that does nothing and no error. With it set, any file that cannot be applied safely is an error naming the setting or the tenant, with no reader and
// no pool left open. Pools connect lazily: nothing is dialled here.
func buildRecordReader(ctx context.Context, env Env) (reconstruct.TenantDBs, func(), error) {
	noop := func() {}
	if env.Getenv(EnvRecordReadDSNDir) == "" {
		return nil, noop, nil
	}
	pools, closer, err := loginPools(ctx, env, EnvRecordReadDSNDir)
	if err != nil {
		return nil, nil, err
	}
	return &recordPools{pools: pools}, closer, nil
}

// loginPools is the directory loader both the Record reader and the orchestrator's Recorder use (noema#535, A202): one lazily connecting pool per file, each file named by a
// tenant ID and holding that tenant's login; regular files only, no link; one login in one file. setting is the name of the setting that holds the directory, for the
// errors. The error names the setting or the tenant and never a DSN or any part of one (AGENT-13). The closer is safe to call more than once.
func loginPools(ctx context.Context, env Env, setting string) (map[string]*pgxpool.Pool, func(), error) {
	dir := env.Getenv(setting)
	if env.ReadDir == nil || env.ReadFile == nil {
		return nil, nil, fmt.Errorf("cmd/core: %s cannot be read: the process has no file access wired", setting)
	}
	entries, err := env.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: %s: the directory cannot be read", setting)
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("cmd/core: %s: the directory holds no tenant file", setting)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		// Type() does not follow a link: a symlink, a directory or any other non-regular entry is refused, so one tenant's file cannot stand in for another's.
		if !e.Type().IsRegular() || !tenantFileRe.MatchString(e.Name()) {
			return nil, nil, fmt.Errorf("cmd/core: %s: an entry is not a regular file named by a tenant ID", setting)
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	pools := make(map[string]*pgxpool.Pool, len(names))
	logins := make(map[string]string, len(names)) // login identity -> the tenant whose file holds it
	closeAll := func() {
		for _, p := range pools {
			p.Close()
		}
	}
	for _, tenant := range names {
		raw, err := env.ReadFile(filepath.Join(dir, tenant))
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the file of tenant %s cannot be read", setting, tenant)
		}
		dsn := strings.TrimSpace(string(raw))
		if dsn == "" {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the file of tenant %s is empty", setting, tenant)
		}
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil { // the parse error is dropped on purpose: it can quote the DSN
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the DSN of tenant %s does not parse", setting, tenant)
		}
		// One login serves one tenant: the database binds a login to a tenant (DEBT-04), so the same login in two files would let two tenants share a binding. The identity is
		// the user, host, port and database; the password and options do not make another login. The error names the tenants, never the login.
		id := fmt.Sprintf("%s\x00%s\x00%d\x00%s", cfg.ConnConfig.User, cfg.ConnConfig.Host, cfg.ConnConfig.Port, cfg.ConnConfig.Database)
		if other, dup := logins[id]; dup {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the files of tenants %s and %s hold the same login", setting, other, tenant)
		}
		logins[id] = tenant
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the pool of tenant %s cannot be made", setting, tenant)
		}
		pools[tenant] = pool
	}
	var once sync.Once
	return pools, func() { once.Do(closeAll) }, nil
}
